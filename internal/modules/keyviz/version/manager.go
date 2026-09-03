package version

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
)

const (
	exeName   = "keyviz.exe" // Tauri 有效载荷主程序（MSI 内单文件，WebView2 走系统运行时）
	dirPrefix = "keyviz_"    // 版本隔离目录前缀（与 piclite_ / ccswitch_ 同构）
)

// plainVersionRe 纯版本号（如 2.1.1），用于目录名与 FileVersion 校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（keyviz_2.1.1）；
// imported- 分支收纳版本探测失败时间戳兜底的导入（无 FileVersion 资源时）
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager Keyviz 版本管理引擎：远程列表、MSI 下载完整性校验、
// msiexec 管理提取隔离安装、本地导入。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时）
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 10 * time.Minute},
	}
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]KeyvizRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// exe 缺失/为空视为损坏安装跳过。Keyviz 配置恒在 %APPDATA%\org.keyviz\store.json
// （tauri-plugin-store 用户目录，与 exe 位置无关），目录内除有效载荷外只有托管侧的 meta.json。
func (m *Manager) ListInstalled() ([]KeyvizVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []KeyvizVersionInfo
	for _, e := range entries {
		if !e.IsDir() || !dirNameRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		exe := filepath.Join(dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := KeyvizVersionInfo{
			Version: "v" + strings.TrimPrefix(e.Name(), dirPrefix),
			ExePath: exe,
			Dir:     dir,
			Size:    fi.Size(),
		}
		// 读取元信息（安装时间、导入来源）
		if meta, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
			var mm map[string]any
			if json.Unmarshal(meta, &mm) == nil {
				if at, ok := mm["installedAt"].(string); ok {
					info.InstalledAt = at
				}
				if isIm, ok := mm["isImport"].(bool); ok {
					info.IsImport = isIm
				}
				if src, ok := mm["source"].(string); ok {
					info.Source = src
				}
			}
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// Download 下载 Windows MSI 并管理提取安装到 versions/keyviz_X.Y.Z/。
// 上游不提供便携 zip，完整性四层兜底与 piclite 同构：
//  1. 官方 sha256 校验（GitHub API digest，第一主依据）；
//  2. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  3. msiexec 管理提取由 Windows Installer 对 cabinet 流做内建 CRC 校验
//     （替代 zip 路线的 archive/zip 逐 entry CRC32）；
//  4. 提取后布局自检（keyviz.exe 存在且非空），失败清理目录。
//
// onProgress 可选：实时上报各阶段进度（下载字节、校验、提取）。
func (m *Manager) Download(version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程资产
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *KeyvizRelease
	for i := range releases {
		if releases[i].Version == version {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		err := fmt.Errorf("远程列表不存在版本 %s", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpMSI, err := os.CreateTemp("", "hanxi-keyviz-*.msi")
	if err != nil {
		return err
	}
	tmpMSIPath := tmpMSI.Name()
	defer os.Remove(tmpMSIPath)
	tmpMSI.Close()

	// 2. 下载 MSI（直连 + 镜像逐个回退）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(version, rel.AssetName), tmpMSIPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 字节数校验
	actual, err := fileSize(tmpMSIPath)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 4. 官方 sha256 校验
	emit("verify", 0, 0, "")
	if err := verifySHA256(tmpMSIPath, rel.SHA256); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}

	// 5. msiexec 管理提取安装到隔离目录（Installer 内建校验在此阶段完成）
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
	if err := extractMSI(tmpMSIPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("提取失败: %v", err))
		return err
	}

	// 6. 落盘元信息
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"source":       rel.AssetName,
		"msiSize":      rel.Size,
		"msiSHA256":    fileSHA256(tmpMSIPath),
		"assetSHA256":  rel.SHA256,
		"verifiedHash": true,
	}
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), meta)

	emit("done", 100, 100, "")
	return nil
}

// Remove 卸载指定版本（删除隔离目录）
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ResolveExe 返回指定版本的 keyviz.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（keyviz_X.Y.Z 或 keyviz_imported-时间戳）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(ver) {
		if !importedDirRe.MatchString(ver) {
			return "", fmt.Errorf("非法版本号: %q", version)
		}
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// ImportLocal 导入本地已安装的 Keyviz（官方 MSI 安装版目录即可：Program Files\keyviz）。
// 与 piclite 同构：Keyviz 配置恒在 %APPDATA%\org.keyviz\store.json 用户目录，
// 与 exe 位置无关，故只迁移 exe 一个文件。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (KeyvizVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return KeyvizVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return KeyvizVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return KeyvizVersionInfo{}, err
	}

	if err := copyFileTo(srcExe, filepath.Join(targetDir, exeName)); err != nil {
		_ = os.RemoveAll(targetDir)
		return KeyvizVersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      exeName,
	})

	return KeyvizVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// extractMSI 经 msiexec 管理提取（administrative install）把 MSI 有效载荷拆进隔离目录。
//
// 上游只有安装器没有便携包，本函数是托管成立的根基（piclite 同机制已真机验证）：
//   - `msiexec /a <msi> /qn TARGETDIR=<dir>` 是 Windows Installer 的管理安装：
//     只按 Directory 表展开文件到 TARGETDIR，不写注册表、不建快捷方式、免管理员；
//   - 管理映像布局 <stage>\PFiles\keyviz\keyviz.exe（实测 v2.1.1）："PFiles" 是
//     Installer 对 ProgramFiles 目录的固定字面映射（不受系统语言影响），子目录名
//     跟随 WiX INSTALLFOLDER；不硬编码层级，改为递归收割 exe 所在目录，天然吸收布局漂移；
//   - 收割后丢弃 stage（连同管理映像自动复制进去的源 keyviz.msi 副本）；
//   - 提取完整性依赖 Windows Installer 对 cabinet 流的内建 CRC 校验（第三层）。
func extractMSI(msiPath, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}
	fail := func(err error) error {
		_ = os.RemoveAll(targetDir)
		return err
	}

	stage, err := os.MkdirTemp("", "hanxi-keyviz-msi-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	// /qn 全程静默；msiexec.exe 作为 Installer 客户端在装完前不返回，CombinedOutput 同步等待
	cmd := exec.Command("msiexec.exe", "/a", msiPath, "/qn", "TARGETDIR="+stage)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fail(fmt.Errorf("msiexec 管理提取失败: %w（输出: %s）", err, strings.TrimSpace(string(out))))
	}

	// 防御：客户端返回与安装服务落盘理论上有间隙，轮询等 payload 出现
	var payload string
	deadline := time.Now().Add(10 * time.Second)
	for {
		if payload = findPayloadDir(stage, exeName); payload != "" {
			break
		}
		if time.Now().After(deadline) {
			return fail(fmt.Errorf("管理提取无效：映像中未找到 %s", exeName))
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := copyTree(payload, targetDir); err != nil {
		return fail(err)
	}

	// 布局自检：exe 存在且非空
	fi, err := os.Stat(filepath.Join(targetDir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fail(fmt.Errorf("MSI 布局无效：缺少可用的 %s", exeName))
	}
	return nil
}

// findPayloadDir 在管理映像根下递归定位含目标 exe 的目录（大小写不敏感）。
// 只取第一个命中；正常映像内 exe 唯一。
func findPayloadDir(stageRoot, exe string) string {
	wanted := strings.ToLower(exe)
	var found string
	_ = filepath.WalkDir(stageRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" || d.IsDir() {
			return nil //nolint:nilerr // 遍历中的局部 IO 错误跳过即可
		}
		if strings.ToLower(d.Name()) == wanted {
			found = filepath.Dir(path)
		}
		return nil
	})
	return found
}

// copyTree 把 src 目录内容整体复制到 dst（保留相对子目录结构）。
// 文件统一 0755：MSI 有效载荷恒含主 exe，且目标只在托管隔离目录内。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFileTo(path, target)
	})
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
