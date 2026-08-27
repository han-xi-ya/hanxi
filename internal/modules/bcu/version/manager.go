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

	"hubkit/internal/platform/versioninfo"
)

const (
	exeName   = "BCUninstaller.exe"
	dirPrefix = "bcu_" // 版本隔离目录前缀（与 frp_v0.61.1 / everything_v1.5.0 同构）
)

// dirNameRe 版本目录名（bcu_6.2.0 / bcu_6.1.0.1）；
// imported- 分支收纳版本探测失败时间戳兜底的导入。
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// settingsName BCU 便携版的设置文件（与 exe 同目录），导入/整部迁移时一并携带。
const settingsName = "BCUninstaller.settings"

// Manager BCU 版本管理引擎：远程列表、下载完整性校验、保布局解压隔离、本地导入。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时；便携包约 76MB）
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 15 * time.Minute},
	}
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]BCURelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// exe 缺失/为空视为损坏安装跳过（settings 缺失属首启未配置，正常）。
func (m *Manager) ListInstalled() ([]BCUVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []BCUVersionInfo
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

		info := BCUVersionInfo{
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

// Download 下载自包含便携 zip 并解压安装到 versions/bcu_X.Y.Z/。
// 完整性四层兜底：
//  1. 官方 sha256 校验（第一主依据）；
//  2. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  3. archive/zip 读取每个 entry 时强制 CRC32 校验（extractAll 读满不提前返回）；
//  4. 提取后布局自检（BCUninstaller.exe 非空），失败清理目录。
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
	var rel *BCURelease
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

	tmpZip, err := os.CreateTemp("", "hubkit-bcu-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 下载 zip（直连 + 镜像逐个回退；tag 与资产版本不同形，用 release 自带 tag 拼路径）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(rel.Tag, rel.AssetName), tmpZipPath, func(done int64) {
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

// ResolveExe 返回指定版本的 BCUninstaller.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（bcu_X.Y.Z 或 bcu_imported-时间戳）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimSpace(version)
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

// ImportLocal 导入本地已有的 BCU 便携安装（黑名单整搬）：
// BCU 便携目录 = 完整应用（自含运行时）+ BCUninstaller.settings，全部与 exe 同目录；
// 与 everything 白名单相反——这里"整套搬"才是用户预期语义。
// 调用方需先确保源实例未运行（exe 被写锁时拷贝不可信）。
func (m *Manager) ImportLocal(srcDir string) (BCUVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return BCUVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与其他模块 ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return BCUVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return BCUVersionInfo{}, err
	}

	// 黑名单整搬：临时/系统垃圾文件不搬，其余（exe/dll/settings/cache 等）全量保留
	var copied int
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		_ = os.RemoveAll(targetDir)
		return BCUVersionInfo{}, err
	}
	for _, e := range entries {
		if e.Name() == "meta.json" { // 源目录若已存在我们的元信息则跳过，导入会重写
			continue
		}
		if isTempLike(e.Name()) {
			continue
		}
		src, dst := filepath.Join(srcDir, e.Name()), filepath.Join(targetDir, e.Name())
		if e.IsDir() {
			if copyDirErr := copyDirTo(src, dst); copyDirErr != nil {
				_ = os.RemoveAll(targetDir)
				return BCUVersionInfo{}, copyDirErr
			}
		} else {
			if copyFileErr := copyFileTo(src, dst); copyFileErr != nil {
				_ = os.RemoveAll(targetDir)
				return BCUVersionInfo{}, copyFileErr
			}
		}
		copied++
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      copied,
	})

	return BCUVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
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

	fi, err := os.Stat(filepath.Join(targetDir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fail(fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName))
	}
	return nil
}

// isTempLike 识别导入时不应搬运的临时/锁/系统垃圾文件。
func isTempLike(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".tmp") || strings.HasPrefix(lower, "~") {
		return true
	}
	if strings.Contains(lower, "-wal") || strings.Contains(lower, "-shm") {
		return true
	}
	if lower == "desktop.ini" || lower == "thumbs.db" {
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
