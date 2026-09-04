package version

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
)

const (
	exeName          = "GuoheView.exe" // 主程序（自研解码内核经 ghde.dll 等同目录 DLL 加载）
	portableMarkName = "portable.ini"  // 官方便携标记：存在则 config.ini 落 exe 同目录，程序只读不改
	dirPrefix        = "guoheview_"    // 版本隔离目录前缀（与 ccswitch_ / piclite_ 同构）
)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（guoheview_3.2.7.98，四段构建号）；
// imported- 分支收纳版本探测失败时间戳兜底的导入（无 FileVersion 资源时）
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager 果核看图版本管理引擎：远程当前版本（双通道）、下载 MD5 校验、
// 保布局解压隔离（收割便携根目录）、本地整套导入。
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

// ListRemote 获取远程可用版本（stable + 更新的 beta；10 分钟内命中缓存）。
// 上游接口只发布当前版本，列表至多两条——历史版本无法远程获取是上游特性。
func (m *Manager) ListRemote() ([]ViewRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// exe 缺失/为空或便携标记缺失均视为损坏安装跳过（标记缺失意味着实例会把
// 配置写进 %APPDATA%，破坏托管隔离语义）。
func (m *Manager) ListInstalled() ([]ViewVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []ViewVersionInfo
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
		if _, statErr := os.Stat(filepath.Join(dir, portableMarkName)); statErr != nil {
			continue
		}

		info := ViewVersionInfo{
			Version: "v" + strings.TrimPrefix(e.Name(), dirPrefix),
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

// Download 下载便携 zip 并解压安装到 versions/guoheview_X.Y.Z.W/。
// 完整性四层兜底（官方哈希只有 MD5，第一层强度弱于 GitHub digest，如实说明）：
//  1. 官方 MD5 + HTTPS 传输（接口 md5 字段，防损坏与低级篡改）；
//  2. 下载落盘字节数 == 接口声明 size（防截断）；
//  3. archive/zip 每 entry 强制 CRC32 校验（extractAll 读满不提前返回）；
//  4. 解压后布局自检（GuoheView.exe 非空 + portable.ini 存在），失败清理目录。
//
// onProgress 可选：实时上报各阶段进度。
func (m *Manager) Download(version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程资产
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本失败: %v", err))
		return err
	}
	var rel *ViewRelease
	for i := range releases {
		if releases[i].Version == version {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		err := fmt.Errorf("远程列表不存在版本 %s（上游只发布当前版本，旧版请用「导入本地」）", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-guoheview-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 下载 zip（官方单域，同 URL 多轮重试）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, []string{rel.AssetURL}, tmpZipPath, func(done int64) {
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

	// 4. 官方 MD5 校验
	emit("verify", 0, 0, "")
	if err := verifyMD5(tmpZipPath, rel.MD5); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}

	// 5. 解压安装到隔离目录（zip 内建 CRC32 在此阶段逐 entry 校验）
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
	if err := extractAll(tmpZipPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 6. 落盘元信息
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"source":       rel.AssetName,
		"channel":      rel.Channel,
		"zipSize":      rel.Size,
		"zipMD5":       fileMD5Hex(tmpZipPath),
		"assetMD5":     rel.MD5,
		"zipSHA256":    fileSHA256(tmpZipPath),
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

// ResolveExe 返回指定版本的 GuoheView.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（guoheview_X.Y.Z.W 或 guoheview_imported-时间戳）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !fourSegVersion.MatchString(ver) && !importedDirRe.MatchString(ver) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// ImportLocal 导入本地已有的果核看图便携目录（自行解压/拷贝的整套目录）。
// 果核看图是"整套目录即程序"的便携形态（DLL + resources + plugins 缺一不可），
// 与 everything 同款整套搬运；config.ini 一并带走（便携模式下配置就在目录内）。
// 源目录缺 portable.ini 时补写官方开关（该文件程序只读不改，仅标志存在性，
// 语义安全）——保证托管实例配置永远留在隔离目录，不外溢 %APPDATA%。
// 调用方需先确保源实例未运行（运行中的文件被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (ViewVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return ViewVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !fourSegVersion.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return ViewVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return ViewVersionInfo{}, err
	}

	// 整套搬运；跳过托管侧自有 meta.json（由本次导入重写）与临时残留
	if err := copyTree(srcDir, targetDir, "meta.json"); err != nil {
		_ = os.RemoveAll(targetDir)
		return ViewVersionInfo{}, err
	}
	if _, err := os.Stat(filepath.Join(targetDir, portableMarkName)); err != nil {
		// 上游 portable.ini 原文即"仅作是否便携的开关，程序不会修改它"
		marker := "; Hanxi 托管安装补写的便携模式开关（官方语义，见上游 portable.ini 说明）\n"
		if err := os.WriteFile(filepath.Join(targetDir, portableMarkName), []byte(marker), 0644); err != nil {
			_ = os.RemoveAll(targetDir)
			return ViewVersionInfo{}, err
		}
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
	})

	return ViewVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// extractAll 解压便携 zip 到目标目录，自动收割便携根目录内容。
// 官方便携 zip 顶层是 `GuoheViewPortable/` 包装目录（实测 3.2.7），若原样解压
// exe 会深一层、破坏"版本目录即安装目录"的全家族布局——以 exe 所在 entry 的
// 父目录为 payload 根，只收割根内内容并平铺。每个 entry 必须读满——
// io.Copy 跑完才触发 archive/zip 内建 CRC32 校验。
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

	// 定位 payload 根：exe entry 的父目录（"." 表示 zip 已是平铺布局）
	payloadRoot := ""
	found := false
	wanted := strings.ToLower(exeName)
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if strings.ToLower(pathBase(name)) == wanted {
			payloadRoot = pathDir(name) // "." 或 "GuoheViewPortable"
			found = true
			break
		}
	}
	if !found {
		return fail(fmt.Errorf("zip 内未找到 %s", exeName))
	}

	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		rel, ok := stripPayloadPrefix(name, payloadRoot)
		if !ok {
			continue // 根外的杂质 entry 一概不收
		}
		if rel == "" {
			continue // payload 根目录 entry 本身（"GuoheViewPortable/"）
		}
		// ZipSlip 防护：拒绝绝对路径与逃逸出目标目录的条目
		clean := filepath.Clean(filepath.FromSlash(rel))
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
		_, copyErr := io.Copy(out, rc) // 必须读满：提前返回会跳过 CRC32 校验
		rc.Close()
		out.Close()
		if copyErr != nil {
			return fail(copyErr)
		}
	}

	// 布局自检：exe 存在且非空 + 便携标记存在（官方便携 zip 恒有二者）
	fi, err := os.Stat(filepath.Join(targetDir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fail(fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName))
	}
	if _, err := os.Stat(filepath.Join(targetDir, portableMarkName)); err != nil {
		return fail(fmt.Errorf("zip 布局无效：缺少 %s 便携标记", portableMarkName))
	}
	return nil
}

// stripPayloadPrefix 计算 entry 相对 payload 根的路径；根外 entry 返回 ok=false。
// payloadRoot 为 "." 时全收。
func stripPayloadPrefix(name, payloadRoot string) (string, bool) {
	if payloadRoot == "." || payloadRoot == "" {
		return name, true
	}
	if rest, ok := strings.CutPrefix(name, payloadRoot+"/"); ok {
		return rest, true
	}
	return "", false
}

func pathBase(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// pathDir 返回 zip entry 的父目录（"/" 风格）；根级 entry 返回 "."。
func pathDir(name string) string {
	if i := strings.LastIndexByte(name, '/'); i > 0 {
		return name[:i]
	}
	return "."
}

// copyTree 把 src 目录内容整体复制到 dst（保留相对结构）。
// skip 列表按文件基名匹配（导入场景排除托管侧 meta.json）。
func copyTree(src, dst string, skip ...string) error {
	skipSet := map[string]bool{"meta.json.tmp": false}
	for _, s := range skip {
		skipSet[s] = true
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.Name() != "." && skipSet[d.Name()] {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
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
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
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
