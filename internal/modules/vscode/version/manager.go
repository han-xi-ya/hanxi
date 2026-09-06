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
	exeName         = "Code.exe"     // 便携版与安装版主程序同名（product.nameShort 实证）
	codeCmdRel      = "bin/code.cmd" // 便携归档恒定成员（CLI 信使脚本，布局自检依据）
	productJSONMark = "resources/app/product.json"
	dataDirName     = "data"    // 官方便携模式触发器：Code.exe 同目录存在即数据全自包含
	dirPrefix       = "vscode_" // 版本隔离目录前缀（与 ccswitch_/frp_ 同构）
)

// portableDirRe 版本目录名（vscode_1.136.1；imported- 收纳版本探测失败的导入）
var portableDirRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager VS Code 版本管理引擎：远程列表、双形态下载、保布局解压隔离、本地导入。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时：安装器 ~120MB / zip ~330MB）
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 15 * time.Minute},
	}
}

// ListRemote 获取指定形态的远程可用版本（10 分钟内命中缓存）。
func (m *Manager) ListRemote(form Form) ([]Release, error) {
	return ListRemote(form)
}

// ---------- 便携版 ----------

// ListInstalled 扫描本地已安装便携版目录。
// Code.exe 缺失/为空或 bin/code.cmd 缺失均视为损坏安装跳过（官方归档恒含二者）。
func (m *Manager) ListInstalled() ([]VersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []VersionInfo
	for _, e := range entries {
		if !e.IsDir() || !portableDirRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		if !validPortableLayout(dir) {
			continue
		}
		exe := filepath.Join(dir, exeName)
		fi, _ := os.Stat(exe) // validPortableLayout 已保证存在

		info := VersionInfo{
			Version: strings.TrimPrefix(e.Name(), dirPrefix),
			ExePath: exe,
			Dir:     dir,
			Size:    fi.Size(),
		}
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
				if v, ok := mm["verifiedHash"].(bool); ok {
					info.Verified = v
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

// validPortableLayout 便携目录布局判定：Code.exe 非空 + bin/code.cmd 存在。
func validPortableLayout(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(codeCmdRel))); err != nil || fi.IsDir() {
		return false
	}
	return true
}

// EnsureDataDir 保证便携版数据目录存在（幂等）：data\ 是官方便携模式激活器，
// 缺失时实例会把数据写回 %APPDATA%\Code，破坏托管隔离承诺——启动前必须兜底。
func (m *Manager) EnsureDataDir(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(dir, dataDirName), 0755)
}

// ResolveExe 返回指定版本便携 Code.exe 路径（不存在返回错误）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// Remove 卸载指定便携版本（删除隔离目录）。
// 刻意不动 data\：用户装过的扩展/配置随目录一起删属预期行为，但调用方
// （service 层）须先确保该版本实例未运行。
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// resolveVersionDir 定位版本隔离目录（vscode_X.Y.Z 或 vscode_imported-时间戳）。
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if !plainSemver.MatchString(ver) && !strings.HasPrefix(ver, "imported-") {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// ImportLocal 导入本地便携版 VS Code 目录（整套迁移，data\ 除外——导入即新环境）。
// 安装版目录（含 unins000.exe）拒绝导入：安装版由注册表自动感知，无需迁移。
func (m *Manager) ImportLocal(srcDir string) (VersionInfo, error) {
	srcDir = strings.TrimSpace(srcDir)
	if !validPortableLayout(srcDir) {
		return VersionInfo{}, fmt.Errorf("源目录不是有效的 VS Code 便携版（缺少 %s 或 %s）: %s", exeName, codeCmdRel, srcDir)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "unins000.exe")); err == nil {
		return VersionInfo{}, fmt.Errorf("源目录是安装版（含卸载器）：安装版会被自动感知，无需导入")
	}

	srcExe := filepath.Join(srcDir, exeName)
	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainSemver.MatchString(version) {
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return VersionInfo{}, fmt.Errorf("版本 %s 已存在，请先卸载再导入", version)
	}

	fi, err := os.Stat(srcExe)
	if err != nil {
		return VersionInfo{}, err
	}

	if err := copyTreeExcept(srcDir, targetDir, []string{dataDirName, "meta.json"}); err != nil {
		_ = os.RemoveAll(targetDir)
		return VersionInfo{}, fmt.Errorf("迁移便携版目录失败: %w", err)
	}
	// 导入即自包含：强制补齐 data\，绝不把源目录旧数据搬进来
	if err := os.MkdirAll(filepath.Join(targetDir, dataDirName), 0755); err != nil {
		_ = os.RemoveAll(targetDir)
		return VersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
	})
	return VersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// Download 下载指定形态版本：
//   - portable：zip → 校验 → 保布局解压到 versions/vscode_X.Y.Z/ → 补建 data\；
//   - installer：exe → 校验 → 静默运行官方 Inno 安装器（安装位置随本机既有安装）。
//
// 完整性策略（上游官方 sha256 仅最新版可查，见 remote.go）：
//  1. latest（列表首项且 SHA256 非空）：官方 sha256 → 四层（哈希+字节数+CRC32/MZ+布局）；
//  2. 历史版本：无官方哈希 → 降级 markeron 三层（字节数 + CRC32/MZ 魔数 + 布局自检），
//     meta.json 记 verifiedHash=false 供 UI 如实展示。
func (m *Manager) Download(version string, form Form, onProgress func(p DownloadProgress)) error {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Form: string(form), Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	rel, err := m.resolveDownloadable(form, version)
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("解析下载直链失败: %v", err))
		return err
	}

	ext := ".zip"
	if form == FormInstaller {
		ext = ".exe"
	}
	tmp, err := os.CreateTemp("", "hanxi-vscode-*"+ext)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	tmp.Close()

	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, rel.DownloadURL, tmpPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 层 2：字节数（防截断/代理篡改）
	actual, err := fileSize(tmpPath)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if rel.Size > 0 && actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 层 1：官方 sha256（仅最新版有；历史版本如实降级）
	verified := false
	emit("verify", 0, 0, "")
	if rel.SHA256 != "" {
		if err := verifySHA256(tmpPath, rel.SHA256); err != nil {
			emit("error", 0, rel.Size, err.Error())
			return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
		}
		verified = true
	} else if form == FormInstaller {
		// 安装器无官方哈希时的形态兜底：MZ 魔数（rustdesk 单 exe 先例）
		if err := verifyMZ(tmpPath); err != nil {
			emit("error", 0, rel.Size, err.Error())
			return err
		}
	}

	if form == FormInstaller {
		emit("install", 0, 0, "")
		if err := runInstallerSilent(tmpPath, rel.Version); err != nil {
			emit("error", 0, 0, fmt.Sprintf("静默安装失败: %v", err))
			return err
		}
		emit("done", 100, 100, "")
		return nil
	}

	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if err := extractAll(tmpPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"source":       rel.AssetName,
		"zipSize":      actual,
		"assetSHA256":  rel.SHA256,
		"computedSHA":  fileSHA256(tmpPath),
		"verifiedHash": verified,
		"commit":       rel.Commit,
	})

	emit("done", 100, 100, "")
	return nil
}

// resolveDownloadable 定位版本下载信息：优先远程缓存；缓存未命中
// （展示窗口之外的旧版本）则现场 HEAD 解析直链。
func (m *Manager) resolveDownloadable(form Form, version string) (Release, error) {
	list, lerr := ListRemote(form)
	if lerr == nil {
		if rel, ok := lookupRemote(list, version); ok {
			return rel, nil
		}
	}
	rel, err := resolveRelease(downloadClient(), form, version)
	if err != nil {
		if lerr != nil {
			return Release{}, fmt.Errorf("版本列表获取失败且 %s 直链解析失败: %v / %v", version, lerr, err)
		}
		return Release{}, fmt.Errorf("版本 %s 无有效下载直链: %w", version, err)
	}
	return rel, nil
}

// extractAll 全量解压 zip 到目标目录（zip 无根目录，Code.exe 落在目标目录根——
// 官方归档布局实测，勿按"包内单根目录"直觉改造）。
// 每个 entry 必须读满——completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 提取完成后自检布局并补建 data\ 便携数据目录，不符即清理目标目录报错。
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

	hasProductJSON := false
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
		// 布局自检素材：Electron 运行时目录名 = commit 前 10 位（随版本漂移，
		// 只能按"存在某个 */resources/app/product.json"判定，不可写死目录名）
		if !hasProductJSON && strings.HasSuffix(filepath.ToSlash(f.Name), productJSONMark) {
			hasProductJSON = true
		}
	}

	// 层 4：布局自检（Code.exe + bin/code.cmd + resources/app/product.json）
	if !validPortableLayout(targetDir) {
		return fail(fmt.Errorf("zip 布局无效：缺少 %s 或 %s", exeName, codeCmdRel))
	}
	if !hasProductJSON {
		return fail(fmt.Errorf("zip 布局无效：缺少 %s", productJSONMark))
	}

	// 便携模式激活器：data\ 目录（官方文档实证——存在即数据全自包含）
	if err := os.MkdirAll(filepath.Join(targetDir, dataDirName), 0755); err != nil {
		return fail(err)
	}
	return nil
}

// copyTreeExcept 整树复制（跳过 skip 名单中的顶层项），用于本地导入。
func copyTreeExcept(srcDir, dstDir string, skip []string) error {
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return os.MkdirAll(dstDir, 0755)
		}
		top := strings.Split(filepath.ToSlash(rel), "/")[0]
		if skipSet[top] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
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
