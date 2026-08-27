package version

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hubkit/internal/platform/versioninfo"
)

const (
	dirPrefix = "everything_v" // 版本隔离目录前缀（与 frp_v0.61.1 / markeron_v2.9.4 同构）
)

// exeCandidates 各通道便携 zip 内的 exe 命名不统一（1.4 为小写 everything.exe、1.5 为 Everything.exe），
// 定位时大小写不敏感逐一尝试。
var exeCandidates = []string{"Everything.exe", "everything.exe"}

// dirNameRe 版本目录名（允许尾字母，如 everything_v1.5.0.1422b）
var dirNameRe = regexp.MustCompile(`^everything_v[0-9][0-9a-zA-Z.]+$`)

// Manager Everything 版本管理引擎：远程槽位、直链下载校验、解压隔离、本地整套导入。
type Manager struct {
	versionsDir string
}

func NewManager(versionsDir string) *Manager {
	return &Manager{versionsDir: versionsDir}
}

// ListRemote 获取远程可用版本槽位（10 分钟缓存，失败降级快照）
func (m *Manager) ListRemote() ([]EverythingRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// exe 缺失/为空视为损坏安装跳过；配置与索引库若损坏属 Everything 运行期问题，不在此拦截。
func (m *Manager) ListInstalled() ([]EverythingVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []EverythingVersionInfo
	for _, e := range entries {
		if !e.IsDir() || !dirNameRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		exe, ok := findExe(dir)
		if !ok {
			continue
		}
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := EverythingVersionInfo{
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
			}
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// Remove 卸载指定版本（删除隔离目录）
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ResolveExe 返回指定版本的 Everything.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe, ok := findExe(dir)
	if !ok {
		return "", fmt.Errorf("版本 %s 安装损坏：缺少 Everything.exe", version)
	}
	return exe, nil
}

// resolveVersionDir 定位版本隔离目录（everything_vX.Y.Z）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimSpace(version)
	if !plainVersionRe.MatchString(ver) {
		return "", fmt.Errorf("非法版本号: %q", ver)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", ver)
}

// ImportLocal 导入本地便携安装整套（exe + 配置 + 语言包 + 索引库），保留用户的定制体验。
// 与 frpc 只拷单 exe 的 ImportLocal 不同：Everything 的价值一半在索引库与配置上。
// 调用方需先确保源目录实例未运行（索引库被写锁时拷贝结果不可信）。
func (m *Manager) ImportLocal(srcDir string) (EverythingVersionInfo, error) {
	srcExe, ok := findExe(srcDir)
	if !ok {
		return EverythingVersionInfo{}, fmt.Errorf("源目录未找到 Everything.exe: %s", srcDir)
	}
	fi, err := os.Stat(srcExe)
	if err != nil {
		return EverythingVersionInfo{}, err
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return EverythingVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return EverythingVersionInfo{}, err
	}

	// 白名单迁移：exe + 配置/语言/索引数据（ini/lng/db）+ 会话 + 插件目录。
	// 临时与锁文件（*.tmp、~*、含 "-wal"/"-shm" 的件）坚决不碰，保证拷贝的是完整一致的索引库。
	var copied []string
	entries, _ := os.ReadDir(srcDir)
	for _, e := range entries {
		name := e.Name()
		if isTempLike(name) {
			continue
		}
		if e.IsDir() {
			if strings.EqualFold(name, "Plugins") {
				if err := copyDirTo(filepath.Join(srcDir, name), filepath.Join(targetDir, name)); err != nil {
					_ = os.RemoveAll(targetDir)
					return EverythingVersionInfo{}, err
				}
				copied = append(copied, name+"/")
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if strings.EqualFold(name, filepath.Base(srcExe)) ||
			ext == ".ini" || ext == ".lng" || ext == ".db" || name == "Session.json" {
			if err := copyFileTo(filepath.Join(srcDir, name), filepath.Join(targetDir, name)); err != nil {
				_ = os.RemoveAll(targetDir)
				return EverythingVersionInfo{}, err
			}
			copied = append(copied, name)
		}
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      strings.Join(copied, ", "),
	})

	return EverythingVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, filepath.Base(srcExe)),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// findExe 在目录内大小写不敏感定位 Everything.exe（1.4 小写 / 1.5 大写）。
func findExe(dir string) (string, bool) {
	for _, name := range exeCandidates {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode().IsRegular() {
			return p, true
		}
	}
	// 兜底：大小写之外的未来命名（如 Everything64.exe）不硬枚举，读目录模糊匹配
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			if n := e.Name(); strings.HasPrefix(strings.ToLower(n), "everything") && strings.HasSuffix(strings.ToLower(n), ".exe") {
				return filepath.Join(dir, n), true
			}
		}
	}
	return "", false
}

// extractAll 全量解压 zip 到目标目录。每个 entry 必须读满——
// completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 提取完成后自检 exe 存在，不符（缺 exe/恶意条目）即清理目标目录报错。
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

	if _, ok := findExe(targetDir); !ok {
		return fail(fmt.Errorf("zip 布局无效：缺少 Everything.exe"))
	}
	return nil
}

// isTempLike 识别导入时不应搬运的临时/锁文件。
func isTempLike(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".tmp") || strings.HasPrefix(lower, "~") {
		return true
	}
	if strings.Contains(lower, "-wal") || strings.Contains(lower, "-shm") {
		return true
	}
	return false
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

func copyDirTo(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirTo(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFileTo(s, d); err != nil {
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