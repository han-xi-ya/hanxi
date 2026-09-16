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
	exeName     = "Paseo.exe"                                           // electron-builder executableName 固定
	asarRelPath = "resources" + string(filepath.Separator) + "app.asar" // Electron 主包，布局自检依据
	dirPrefix   = "paseo_"                                              // 版本隔离目录前缀（与 vscode_/recordly 家族同构）
)

// 多版本隔离目录设计（vscode 便携同款，与 recordly 的 NSIS 单目录相反）：
// 上游 win zip 是"解压即运行"的 win-unpacked 归档，无注册表卸载语义，
// 因此每版本独占 versions/paseo_<ver>/ 目录，共存与切换零成本。
//
// Manager Paseo 版本管理引擎：远程列表（双通道）、zip 下载与四层完整性
// 校验、保布局解压、本地导入、卸载。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（win zip ~180MB，长超时，与 recordly 214MB 同档）
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 20 * time.Minute},
	}
}

// versionDirRe 版本目录名：paseo_0.8.0 / paseo_0.8.0-beta.1；
// imported- 收纳 PE 版本探测失败的本地导入（vscode 同规则）。
var versionDirRe = regexp.MustCompile(`^` + dirPrefix +
	`(?:\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.\-]*)?|imported-\d{8}-\d{6})$`)

// ListRemote 获取远程可用版本（includePre=true 时含 beta 通道；10 分钟缓存）。
func (m *Manager) ListRemote(includePre bool) ([]PaseoRelease, error) {
	return remoteCache.get(includePre)
}

// ListInstalled 扫描本地已安装的 Paseo 版本目录。
// Paseo.exe 缺失/为空或 resources/app.asar 缺失均视为损坏安装跳过。
func (m *Manager) ListInstalled() ([]PaseoVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []PaseoVersionInfo
	for _, e := range entries {
		if !e.IsDir() || !versionDirRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		if !validPortableLayout(dir) {
			continue
		}
		exe := filepath.Join(dir, exeName)
		fi, _ := os.Stat(exe) // validPortableLayout 已保证存在

		info := PaseoVersionInfo{
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
				if vh, ok := mm["verifiedHash"].(bool); ok {
					info.VerifiedHash = vh
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

// validPortableLayout 便携目录布局判定：Paseo.exe 非空 + resources/app.asar 存在
// （win zip = win-unpacked 打包，二者为 electron-builder 恒定成员；
// app.asar.unpacked 由 asarUnpack 派生、随版本配置漂移，不作判据）。
func validPortableLayout(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	if asar, err := os.Stat(filepath.Join(dir, asarRelPath)); err != nil || asar.IsDir() {
		return false
	}
	return true
}

// ResolveExe 返回指定版本 Paseo.exe 路径（不存在返回错误）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（接受带/不带 v 前缀输入）。
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if !versionDirRe.MatchString(dirPrefix + ver) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// Download 下载官方 win zip 并解压到 versions/paseo_<ver>/。
// 完整性四层兜底（recordly 校验链在 zip 形态下的对应物；上游不发布
// SHA256SUMS 类清单，"第二只眼"由 zip 内建 CRC32 承担）：
//  1. 官方 sha256 校验（GitHub API digest，第一主依据；缺 digest 的版本根本不入列表）；
//  2. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  3. 解压逐条目读满触发 archive/zip 内建 CRC32（数据级校验）；
//  4. 解压后布局自检（Paseo.exe 非空 + resources/app.asar），失败清理目录。
//
// 数据说明：解压不创建任何数据目录——Paseo 无便携数据激活器，数据恒在
// %APPDATA%\Paseo 与 ~/.paseo（托管与用户自装实例共享，集成决策见模块注释）。
//
// onProgress 可选：实时上报各阶段进度。
func (m *Manager) Download(version string, onProgress func(p DownloadProgress)) error {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析远程资产（缓存内按全量查找，不受当前通道显示过滤影响）
	rel, ok := remoteCache.findRelease(version)
	if !ok {
		// 冷缓存：先拉一轮列表再查（findRelease 以 Version 字段匹配，缓存已存裸版本）
		if _, err := remoteCache.get(true); err != nil {
			emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
			return err
		}
		rel, ok = remoteCache.findRelease(version)
	}
	if !ok {
		err := fmt.Errorf("远程列表不存在版本 %s（或该版本无本机可用的 Windows 便携资产/官方哈希）", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	if _, err := m.resolveVersionDir(version); err == nil {
		err := fmt.Errorf("版本 %s 已安装，无需重复下载", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpDir, err := os.MkdirTemp("", "hanxi-paseo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmpZip := filepath.Join(tmpDir, rel.AssetName)

	// 2. 下载 zip（直连 + 镜像逐个回退；注意用 Tag 原文拼下载路径）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(rel.Tag, rel.AssetName), tmpZip, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 层 2：字节数校验
	actual, err := fileSize(tmpZip)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 层 1：官方 digest sha256
	emit("verify", 0, 0, "")
	if err := verifySHA256(tmpZip, rel.SHA256); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}

	// 层 3+4：解压（逐条目读满触发 CRC32）+ 布局自检
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if err := extractAll(tmpZip, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 落盘元信息（source 记资产名，verifiedHash 恒 true——无哈希版本不入列表）
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"tag":          rel.Tag,
		"source":       rel.AssetName,
		"zipSize":      actual,
		"assetSHA256":  rel.SHA256,
		"verifiedHash": true,
	}
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), meta)

	emit("done", 100, 100, "")
	return nil
}

// extractAll 全量解压 zip 到目标目录（win zip 为 win-unpacked 打包，
// Paseo.exe 落在目标目录根——中央目录实测无根目录包裹，勿按"包内单根
// 目录"直觉改造，vscode/recordly 点名过的同一陷阱）。
// 每个 entry 必须读满——io.Copy 跑完才触发 archive/zip 内建 CRC32 校验。
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
		if _, copyErr := io.Copy(out, rc); copyErr != nil {
			rc.Close()
			out.Close()
			return fail(copyErr)
		}
		rc.Close()
		out.Close()
	}

	if err := layoutCheck(targetDir); err != nil {
		return fail(err)
	}
	return nil
}

// layoutCheck 解压/导入后的落盘布局自检：exe 非空 + Electron 主包存在。
func layoutCheck(dir string) error {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("布局无效：缺少可用的 %s", exeName)
	}
	if asar, err := os.Stat(filepath.Join(dir, asarRelPath)); err != nil || asar.IsDir() {
		return fmt.Errorf("布局无效：缺少 %s", asarRelPath)
	}
	return nil
}

// Remove 卸载指定版本（删除隔离目录）。
// 刻意不动 %APPDATA%\Paseo 与 ~/.paseo：那是用户目录共享数据，
// 归用户与其自装实例所有，删除托管版本不越权（recordly"卸载保数据"先例）。
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ImportLocal 导入本地 Paseo 程序目录（整套 Electron 目录迁移）。
// 数据恒在 %APPDATA%\Paseo 与 ~/.paseo（与 exe 位置无关），程序目录内没有
// 用户数据，整套拷贝即为完整迁移；与"便携 vs 自装"哪份在用无关。
// 调用方需先确保源实例未运行（Windows 下运行中的 exe 被独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (PaseoVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return PaseoVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}
	if asar, err := os.Stat(filepath.Join(srcDir, asarRelPath)); err != nil || asar.IsDir() {
		return PaseoVersionInfo{}, fmt.Errorf("源目录缺少 %s，不是 Paseo 程序目录: %s", asarRelPath, srcDir)
	}
	if isUnderDir(srcDir, filepath.Join(m.versionsDir)) {
		return PaseoVersionInfo{}, fmt.Errorf("源目录本身就在 Hanxi 托管目录内，无需导入")
	}

	version := ""
	if v, verr := versioninfo.FileVersion(srcExe); verr == nil && v != "" {
		cand := strings.TrimPrefix(v, "v")
		if versionDirRe.MatchString(dirPrefix + cand) {
			version = cand
		}
	}
	if version == "" {
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return PaseoVersionInfo{}, fmt.Errorf("版本 %s 已存在，请先卸载该版本再导入", version)
	}

	if err := copyTreeExcept(srcDir, targetDir, []string{"meta.json", "hanxi-meta.json"}); err != nil {
		_ = os.RemoveAll(targetDir)
		return PaseoVersionInfo{}, fmt.Errorf("迁移程序目录失败: %w", err)
	}
	if err := layoutCheck(targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return PaseoVersionInfo{}, fmt.Errorf("导入后%w", err)
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt":  now,
		"isImport":     true,
		"source":       srcDir,
		"verifiedHash": false,
	})

	return PaseoVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: now,
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// isUnderDir 判断 path 是否位于 parent 目录内（大小写不敏感，Windows 语义）。
func isUnderDir(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../") && rel != "."
}

// copyTreeExcept 整树复制目录（跳过 skip 名单中的顶层项，跳过符号链接防环）。
func copyTreeExcept(src, dst string, skip []string) error {
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		if skipSet[e.Name()] {
			continue
		}
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			continue // 程序目录理论无符号链接；保守跳过防环路与越权写
		case e.IsDir():
			if err := copyTreeExcept(s, d, nil); err != nil {
				return err
			}
		default:
			if err := copyFileTo(s, d); err != nil {
				return err
			}
		}
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

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
