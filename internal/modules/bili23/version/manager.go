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
)

const (
	exeName       = "Bili23.exe"          // 入口（Python-Static 静态解释器改壳，GUI 子系统，进程常驻）
	bootstrapName = "_pystand_static.int" // 启动引导脚本（与 exe 同级，缺失即布局损坏）
	scriptMainRel = "script/main.py"      // 应用主模块相对路径（布局自检第三锚点）
	topDirName    = "Bili23-Downloader"   // 官方 zip 的顶层单目录名（解压时剥离展平）
	dirPrefix     = "bili23_"             // 版本隔离目录前缀（与 ccswitch_x.y.z / markeron_vX 同构）
)

// plainVersionRe 纯版本号（如 2.15.0 / 2.00.7——允许上游的前导零变体），用于目录名与导入探测校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// appVersionRe 导入版本探测：上游把版本号硬编码在 script/util/common/config.py 的
// Config 类属性 app_version = "2.15.0"。Bili23.exe 是 pythonw 改名壳，PE FileVersion
// 恒为 Python 版本号，不可用作应用版本依据（与 ccswitch 单 exe 导入的关键差异）。
var appVersionRe = regexp.MustCompile(`app_version\s*=\s*["'](\d+\.\d+\.\d+)["']`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（bili23_2.15.0 / bili23_2.00.7）；
// imported- 分支收纳版本探测失败时间戳兜底的导入
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager Bili23 Downloader 版本管理引擎：远程列表、下载完整性校验、
// 保布局解压隔离（整目录）、本地导入（整目录复制）。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时）
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 15 * time.Minute}, // 便携包 ~43MB，镜像网络给足余量
	}
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]Bili23Release, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// 布局三锚点（Bili23.exe 非空 + _pystand_static.int + script/main.py）任一缺失
// 均视为损坏安装跳过——本应用是"运行时+源码"目录形态，缺件即无法启动。
func (m *Manager) ListInstalled() ([]Bili23VersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []Bili23VersionInfo
	for _, e := range entries {
		if !e.IsDir() || !dirNameRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		if !verifyLayout(dir) {
			continue
		}
		exe := filepath.Join(dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil {
			continue
		}

		info := Bili23VersionInfo{
			Version: "v" + strings.TrimPrefix(e.Name(), dirPrefix),
			ExePath: exe,
			Dir:     dir,
			Size:    dirSize(dir),
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

// Download 下载便携 zip 并解压安装到 versions/bili23_X.Y.Z/。
// 上游提供官方 sha256（GitHub API digest），完整性四层兜底：
//  1. 官方 sha256 校验（第一主依据）；
//  2. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  3. archive/zip 读取每个 entry 时强制 CRC32 校验（extractAll 读满不提前返回）；
//  4. 提取后布局自检（Bili23.exe + _pystand_static.int + script/main.py），失败清理目录。
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
	var rel *Bili23Release
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

	tmpZip, err := os.CreateTemp("", "hanxi-bili23-*.zip")
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
	targetDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
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

// ResolveExe 返回指定版本的 Bili23.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（bili23_X.Y.Z 或 bili23_imported-时间戳）
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

// ImportLocal 导入本地已有的 Bili23 Downloader（安装版目录 / 手动解压的便携目录均可）。
// 与 ccswitch 的单 exe 导入不同：本应用是"静态 Python 运行时 + 源码"整目录形态，
// 导入单元就是整个安装目录（复制 ~108MB）。用户配置恒在 %APPDATA%\Bili23 Downloader\，
// 与安装位置无关，不随导入迁移。
// 版本号从 script/util/common/config.py 的 app_version 常量解析（exe 是 pythonw 改名壳，
// PE FileVersion 不可信）。调用方需先确保源实例未运行。
func (m *Manager) ImportLocal(srcDir string) (Bili23VersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	// 容错：用户选中"解压出来的外层目录"时自动下钻一层官方顶层目录
	if !verifyLayout(srcDir) {
		nested := filepath.Join(srcDir, topDirName)
		if verifyLayout(nested) {
			srcDir = nested
		} else {
			return Bili23VersionInfo{}, fmt.Errorf("源目录不是有效的 Bili23 Downloader 安装（缺少 %s / %s / %s）: %s",
				exeName, bootstrapName, scriptMainRel, srcDir)
		}
	}
	// 防呆：不允许从 Hanxi 自己的托管目录导入（自我复制制造孤儿目录）
	if absVersions, err := filepath.Abs(m.versionsDir); err == nil {
		if absSrc, err := filepath.Abs(srcDir); err == nil &&
			strings.EqualFold(filepath.Dir(absSrc), absVersions) {
			return Bili23VersionInfo{}, fmt.Errorf("该版本已由 Hanxi 托管，无需导入")
		}
	}

	version := detectAppVersion(srcDir)
	if !plainVersionRe.MatchString(version) {
		// 版本探测失败（config.py 缺失/格式变更）：时间戳兜底，与 ccswitch ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return Bili23VersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}

	// 复制前最后一道竞态防御：锚点校验与复制之间 exe 可能被移走
	if _, err := os.Stat(filepath.Join(srcDir, exeName)); err != nil {
		return Bili23VersionInfo{}, err
	}

	if err := copyTree(srcDir, targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return Bili23VersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      "整个安装目录（运行时 + script + site-packages + bundle）",
	})

	return Bili23VersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        dirSize(targetDir),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// verifyLayout 安装布局三锚点校验（目录形态应用：缺任一件即无法启动）。
func verifyLayout(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, exeName)); err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, bootstrapName)); err != nil || fi.IsDir() {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(scriptMainRel))); err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	return true
}

// detectAppVersion 从 script/util/common/config.py 解析 app_version 常量（失败返回空串）。
func detectAppVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "script", "util", "common", "config.py"))
	if err != nil {
		return ""
	}
	if m := appVersionRe.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

// dirSize 目录总字节数（展示用；个别文件竞态缺失不计为错误）。
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // 遍历展示尺寸，竞态错误静默跳过
		}
		if fi, ierr := d.Info(); ierr == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// copyTree 递归复制目录（保相对布局）。拒绝非常规文件（符号链接等）防目录逃逸与死链复制。
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
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("安装目录含非常规文件，无法导入: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFileTo(path, target)
	})
}

// extractAll 全量解压 zip 到目标目录，剥离官方包的顶层单目录 Bili23-Downloader/，
// 使 Bili23.exe 直接落在隔离目录根上（与 ResolveExe/布局约定统一）。
// 每个 entry 必须读满——completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 提取完成后自检布局（三锚点），不符即清理目标目录报错。
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

	if len(zr.File) == 0 {
		return fail(fmt.Errorf("zip 为空归档"))
	}
	// 顶层目录探测：官方便携包所有 entry 共享单一顶层目录（7z 在 PowerShell 下
	// 通配符未展开所致，实测 v2.10.0–v2.15.0 稳定如此）；若上游某天改为扁平布局，
	// 这里返回空前缀自然兼容。
	prefix := commonTopDirPrefix(zr.File)

	for _, f := range zr.File {
		// ZipSlip 防护：拒绝绝对路径与逃逸出目标目录的条目
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(clean) || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fail(fmt.Errorf("zip 含非法路径条目 %q", f.Name))
		}
		rel := clean
		if prefix != "" {
			slash := filepath.ToSlash(clean)
			if slash != prefix && !strings.HasPrefix(slash, prefix+"/") {
				return fail(fmt.Errorf("zip 顶层布局异常：条目 %q 不在 %s/ 下", f.Name, prefix))
			}
			if slash == prefix {
				// 顶层目录条目自身（Clean 已去尾斜杠）：目录则跳过，散文件不可能
				continue
			}
			stripped := strings.TrimPrefix(strings.TrimPrefix(slash, prefix), "/")
			rel = filepath.Clean(filepath.FromSlash(stripped))
		}
		target := filepath.Join(targetDir, rel)
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

	// 布局自检：三锚点齐备且 exe 非空（官方 zip 恒有）
	if !verifyLayout(targetDir) {
		return fail(fmt.Errorf("zip 布局无效：缺少 %s / %s / %s 三锚点", exeName, bootstrapName, scriptMainRel))
	}
	return nil
}

// commonTopDirPrefix 返回所有 entry 共享的单一顶层目录名（斜杠形式）；
// entry 数不足、存在多顶层目录或扁平文件时返回空串（不剥离）。
func commonTopDirPrefix(files []*zip.File) string {
	first := ""
	for i, f := range files {
		name := filepath.ToSlash(f.Name)
		// 去掉首段：目录条目 "A/" 与文件条目 "A/b" 取法一致
		idx := strings.Index(name, "/")
		if idx <= 0 { // 无斜杠（根下散文件）或首段为空（绝对路径，ZipSlip 循环内另有拒绝）
			return ""
		}
		head := name[:idx]
		if i == 0 {
			first = head
		} else if head != first {
			return ""
		}
	}
	if first == "" || len(files) == 0 {
		return ""
	}
	// 只有一个 entry 时无法区分"顶层目录"与"单文件"，不冒险剥离
	if len(files) == 1 {
		return ""
	}
	return first
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
