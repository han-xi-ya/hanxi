package version

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
)

const (
	exeName    = "TranslucentTB.exe"
	dirPrefix  = "translucenttb_" // 版本隔离目录前缀（与 ccswitch_3.20.0 / frp_v0.61.1 同构）
	configName = "settings.json"  // 上游便携版配置：恒在 exe 同目录（DetermineConfigPath 实证）
)

// companionNames 便携 zip 根目录的必需伴生文件（官方 2022.1~2026.2 实测布局恒定）：
// exe 缺任何一件都无法完成"透明任务栏"核心功能（注入器/UI 线程库/日志/WinUI 页面）。
var companionNames = []string{
	"ExplorerHooks.dll",
	"ExplorerTAP.dll",
	"ProgramLog.dll",
	"Xaml.dll",
	"resources.pri",
}

// yearVersionRe 纯版本号（如 2026.2），用于目录名与 FileVersion 校验
var yearVersionRe = regexp.MustCompile(`^\d{4}\.\d+$`)

// fileVersionRe PE 版本资源归一化：上游 FileVersion 形如 2026.2.0.d4636e4
// （年份.序号.0.构建sha），取前两段即为与远程 tag 同域的规范版本。
var fileVersionRe = regexp.MustCompile(`^(\d{4}\.\d+)(?:\.\d+)?(?:\.[0-9a-fA-F.]+)?$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（translucenttb_2026.2）；imported- 分支收纳
// 版本探测失败的导入（非规范 FileVersion 资源时）
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager TranslucentTB 版本管理引擎：远程列表、下载完整性校验、保布局解压隔离、本地导入。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时）
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 10 * time.Minute},
	}
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]TBRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// exe 缺失/为空或任一必需伴生文件缺失均视为损坏安装跳过（官方便携 zip 恒含全套）。
func (m *Manager) ListInstalled() ([]TBVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []TBVersionInfo
	for _, e := range entries {
		if !e.IsDir() || !dirNameRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		if !layoutValid(dir) {
			continue
		}
		exe := filepath.Join(dir, exeName)
		fi, _ := os.Stat(exe) // layoutValid 已保证存在

		info := TBVersionInfo{
			Version: strings.TrimPrefix(e.Name(), dirPrefix),
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

// layoutValid 安装布局完整性：exe 非空 + 全部必需伴生文件就位（导入目录同样适用）。
func layoutValid(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return false
	}
	for _, c := range companionNames {
		if _, err := os.Stat(filepath.Join(dir, c)); err != nil {
			return false
		}
	}
	return true
}

// Download 下载便携 zip 并解压安装到 versions/translucenttb_YYYY.N/。
// 上游提供官方 sha256（GitHub API digest），完整性四层兜底：
//  1. 官方 sha256 校验（第一主依据，无 digest 的版本已在远程列表过滤层剔除）；
//  2. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  3. archive/zip 读取每个 entry 时强制 CRC32 校验（extractAll 读满不提前返回）；
//  4. 提取后布局自检（exe 非空 + 四 dll + resources.pri 全套），失败清理目录。
//
// onProgress 可选：实时上报各阶段进度（下载字节、校验、解压）。
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
	var rel *TBRelease
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

	tmpZip, err := os.CreateTemp("", "hanxi-translucenttb-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 下载 zip（直连 + 镜像逐个回退）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(version, rel.AssetName), tmpZipPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 字节数校验
	actual, err := fileSize(tmpZipPath)
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
	if err := verifySHA256(tmpZipPath, rel.SHA256); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}

	// 5. 解压保布局安装到隔离目录（zip 内建 CRC32 在此阶段逐 entry 校验）
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if err := extractAll(tmpZipPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 6. 落盘元信息
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"source":       rel.AssetName,
		"zipSize":      rel.Size,
		"zipSHA256":    fileSHA256(tmpZipPath),
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

// ResolveExe 返回指定版本的 TranslucentTB.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（translucenttb_2026.2 或 translucenttb_imported-时间戳）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimSpace(version)
	if !yearVersionRe.MatchString(ver) && !importedDirRe.MatchString(ver) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// ImportLocal 导入本地已安装的 TranslucentTB 便携版（整套迁移）。
// 与 ccswitch 的单 exe 导入不同：TranslucentTB 的 settings.json 恒在 exe 同目录
// （上游 DetermineConfigPath 便携分支实证），配置跟着安装位置走，
// 故 exe + 全部伴生 dll + resources.pri + Assets/ + settings.json（若有）整体搬入。
// 调用方需先确保源实例未运行（Windows 下运行中的 exe 被独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (TBVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return TBVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}
	// 导入源必须是完整便携安装：缺伴生文件的"半个目录"导进来也跑不起来
	var missing []string
	for _, c := range companionNames {
		if _, err := os.Stat(filepath.Join(srcDir, c)); err != nil {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return TBVersionInfo{}, fmt.Errorf("源目录缺少必需伴生文件：%s（请选择完整的便携版目录）", strings.Join(missing, "、"))
	}

	version := normalizeImportedVersion(srcExe)
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return TBVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return TBVersionInfo{}, err
	}

	// 白名单迁移：exe + 伴生文件 + 用户配置（存在则搬）。其余文件一概不搬。
	var copied []string
	whitelist := append([]string{exeName}, companionNames...)
	whitelist = append(whitelist, configName)
	for _, name := range whitelist {
		src := filepath.Join(srcDir, name)
		st, statErr := os.Stat(src)
		if statErr != nil || st.IsDir() {
			continue // settings.json 可能尚未生成（从未首启过），缺项跳过
		}
		if err := copyFileTo(src, filepath.Join(targetDir, name)); err != nil {
			_ = os.RemoveAll(targetDir)
			return TBVersionInfo{}, err
		}
		copied = append(copied, name)
	}
	// Assets/（启动画面资源）存在则整体搬入
	if err := copyDirIfAny(filepath.Join(srcDir, "Assets"), filepath.Join(targetDir, "Assets")); err != nil {
		_ = os.RemoveAll(targetDir)
		return TBVersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      strings.Join(copied, ", "),
	})

	return TBVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// normalizeImportedVersion 从 PE 版本资源提取规范版本号：
// 上游 FileVersion 形如 2026.2.0.d4636e4，归一化为与远程 tag 同域的 2026.2；
// 探测失败或非规范格式退化为时间戳兜底目录（与 frpc/ccswitch ImportLocal 同构）。
func normalizeImportedVersion(exePath string) string {
	if fv, err := versioninfo.FileVersion(exePath); err == nil {
		if g := fileVersionRe.FindStringSubmatch(strings.TrimSpace(fv)); g != nil {
			return g[1]
		}
	}
	return "imported-" + time.Now().Format("20060102-150405")
}

// extractAll 全量解压 zip 到目标目录。每个 entry 必须读满——
// completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 提取完成后自检布局（exe 非空 + 伴生文件全套），不符即清理目标目录报错。
func extractAll(zipPath, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	fail := func(err error) error {
		_ = os.RemoveAll(targetDir)
		return err
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		// ZipSlip 防护：拒绝绝对路径与逃逸出目标目录的条目
		clean := filepath.Clean(f.Name)
		if filepath.IsAbs(clean) || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fail(fmt.Errorf("zip 含非法路径条目 %q", f.Name))
		}
		target := filepath.Join(targetDir, clean)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fail(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fail(err)
		}
		rc, err := f.Open()
		if err != nil {
			return fail(err)
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return fail(err)
		}
		// 必须读满：提前返回会跳过 CRC32 校验
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return fail(copyErr)
		}
	}

	// 布局自检：exe + 全部伴生文件（官方 zip 恒有全套）
	if !layoutValid(targetDir) {
		return fail(fmt.Errorf("zip 布局无效：缺少 %s 或必需伴生文件", exeName))
	}
	return nil
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

// copyDirIfAny 目录存在且含文件时递归搬入；源目录不存在直接成功（导入源可能精简）。
func copyDirIfAny(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())
		if e.IsDir() {
			if err := copyDirIfAny(src, dst); err != nil {
				return err
			}
			continue
		}
		if err := copyFileTo(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
