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
	"runtime"
	"sort"
	"strings"
	"time"

	"hubkit/internal/platform/versioncmp"
	"hubkit/internal/platform/versioninfo"
)

const (
	exeName   = "Snipaste.exe"
	dirPrefix = "snipaste_v"
)

var plainVersionRe = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)+(?:-Beta[0-9]*)?$`)

// Manager 管理 Snipaste 官网免安装版的隔离安装目录。
type Manager struct {
	versionsDir string
	client      *http.Client
	cache       *releaseCache
	fileVersion func(string) (string, error)
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 10 * time.Minute},
		cache:       remoteCache,
		fileVersion: versioninfo.FileVersion,
	}
}

func (m *Manager) ListRemote() ([]SnipasteRelease, error) {
	return m.cache.get()
}

func (m *Manager) ListInstalled() ([]SnipasteVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []SnipasteVersionInfo
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), dirPrefix) || strings.Contains(entry.Name(), ".installing-") || strings.Contains(entry.Name(), ".removing-") {
			continue
		}
		version := strings.TrimPrefix(entry.Name(), dirPrefix)
		if !plainVersionRe.MatchString(version) {
			continue
		}
		dir := filepath.Join(m.versionsDir, entry.Name())
		exe := filepath.Join(dir, exeName)
		fi, err := os.Stat(exe)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}
		if runtime.GOOS == "windows" {
			if actual, err := m.fileVersion(exe); err != nil || normalizeVersion(actual) != normalizeVersion(version) {
				continue
			}
		}
		info := SnipasteVersionInfo{Version: version, ExePath: exe, Dir: dir, Size: fi.Size()}
		readMeta(filepath.Join(dir, "meta.json"), &info)
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	sort.SliceStable(list, func(i, j int) bool {
		return versioncmp.Compare(list[i].Version, list[j].Version) > 0
	})
	return list, nil
}

func readMeta(path string, info *SnipasteVersionInfo) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var meta struct {
		InstalledAt      string `json:"installedAt"`
		IsImport         bool   `json:"isImport"`
		Source           string `json:"source"`
		PackageSHA256    string `json:"packageSHA256"`
		VerificationMode string `json:"verificationMode"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return
	}
	info.InstalledAt = meta.InstalledAt
	info.IsImport = meta.IsImport
	info.Source = meta.Source
	info.PackageSHA256 = meta.PackageSHA256
	info.VerificationMode = meta.VerificationMode
}

func (m *Manager) Download(targetVersion string, onProgress func(DownloadProgress)) error {
	version := normalizeVersion(targetVersion)
	emit := func(stage string, done, total int64, message string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: message})
		}
	}
	if !plainVersionRe.MatchString(version) {
		return fmt.Errorf("非法版本号: %q", targetVersion)
	}
	if _, err := m.ResolveExe(version); err == nil {
		return fmt.Errorf("版本 %s 已安装", version)
	}

	emit("resolve", 0, 0, "正在解析官网版本")
	releases, err := m.cache.get()
	if err != nil {
		return err
	}
	var rel *SnipasteRelease
	for i := range releases {
		if strings.EqualFold(releases[i].Version, version) {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		return fmt.Errorf("官网版本列表中不存在 %s", version)
	}
	if rel.OfficialHash == "" && rel.Size <= 0 {
		return fmt.Errorf("官网未提供版本 %s 的可验证哈希或文件大小，HubKit 拒绝安装", version)
	}

	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return err
	}
	tmpZip, err := os.CreateTemp(m.versionsDir, ".snipaste-download-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	tmpZip.Close()
	defer os.Remove(tmpZipPath)

	emit("downloading", 0, rel.Size, "")
	_, err = downloadTo(m.client, rel.AssetURL, tmpZipPath, func(done, total int64) {
		if rel.Size > 0 {
			total = rel.Size
		}
		emit("downloading", done, total, "")
	})
	if err != nil {
		return fmt.Errorf("下载 Snipaste %s 失败: %w", version, err)
	}

	emit("verify-size", 0, rel.Size, "正在校验文件大小")
	actualSize, err := fileSize(tmpZipPath)
	if err != nil {
		return err
	}
	if rel.Size > 0 && actualSize != rel.Size {
		return fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actualSize)
	}
	if rel.Size <= 0 {
		rel.Size = actualSize
	}

	verificationMode := "size+crc+layout"
	if rel.OfficialHash != "" {
		emit("verify-hash", 0, 0, "正在校验官网 "+strings.ToUpper(rel.HashAlgorithm))
		if err := verifyOfficialHash(tmpZipPath, rel.HashAlgorithm, rel.OfficialHash); err != nil {
			return fmt.Errorf("官网哈希校验失败: %w", err)
		}
		verificationMode = "official-" + strings.ToLower(rel.HashAlgorithm) + "+size+crc+layout"
	}
	packageSHA256 := fileSHA256(tmpZipPath)

	stagingDir := filepath.Join(m.versionsDir, dirPrefix+version+fmt.Sprintf(".installing-%d", time.Now().UnixNano()))
	defer os.RemoveAll(stagingDir)
	emit("verify-archive", 0, 0, "正在校验 ZIP 与解压布局")
	installRoot, err := extractAll(tmpZipPath, stagingDir)
	if err != nil {
		return err
	}

	exe := filepath.Join(installRoot, exeName)
	if runtime.GOOS == "windows" {
		actualVersion, err := m.fileVersion(exe)
		if err != nil {
			return fmt.Errorf("读取 Snipaste.exe 版本失败: %w", err)
		}
		if normalizeVersion(actualVersion) != normalizeVersion(version) {
			return fmt.Errorf("文件版本不匹配：期望 %s，实际 %s", version, actualVersion)
		}
	}

	meta := map[string]any{
		"installedAt":      time.Now().Format("2006-01-02 15:04:05"),
		"source":           rel.AssetName,
		"packageSize":      actualSize,
		"packageSHA256":    packageSHA256,
		"officialHash":     rel.OfficialHash,
		"hashAlgorithm":    rel.HashAlgorithm,
		"verificationMode": verificationMode,
	}
	if err := writeJSON(filepath.Join(installRoot, "meta.json"), meta); err != nil {
		return err
	}

	finalDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("版本 %s 已安装", version)
	}
	emit("install", 0, 0, "正在完成原子安装")
	if installRoot != stagingDir {
		if err := os.Rename(installRoot, finalDir); err != nil {
			return err
		}
	} else if err := os.Rename(stagingDir, finalDir); err != nil {
		return err
	}
	emit("done", 100, 100, "安装完成")
	return nil
}

func (m *Manager) ImportLocal(srcDir string) (SnipasteVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return SnipasteVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}
	version, err := m.fileVersion(srcExe)
	if err != nil || !plainVersionRe.MatchString(normalizeVersion(version)) {
		return SnipasteVersionInfo{}, fmt.Errorf("无法读取可信的 Snipaste 文件版本: %w", err)
	}
	version = normalizeVersion(version)
	finalDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(finalDir); err == nil {
		return SnipasteVersionInfo{}, fmt.Errorf("版本 %s 已安装", version)
	}
	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return SnipasteVersionInfo{}, err
	}
	stagingDir := finalDir + fmt.Sprintf(".installing-%d", time.Now().UnixNano())
	defer os.RemoveAll(stagingDir)
	if err := copyPortableDir(srcDir, stagingDir); err != nil {
		return SnipasteVersionInfo{}, err
	}
	copiedExe := filepath.Join(stagingDir, exeName)
	copiedInfo, err := os.Stat(copiedExe)
	if err != nil || !copiedInfo.Mode().IsRegular() || copiedInfo.Size() == 0 {
		return SnipasteVersionInfo{}, fmt.Errorf("导入目录布局无效：缺少可用的 %s", exeName)
	}
	installedAt := time.Now().Format("2006-01-02 15:04:05")
	packageSHA256 := fileSHA256(copiedExe)
	if err := writeJSON(filepath.Join(stagingDir, "meta.json"), map[string]any{
		"installedAt":      installedAt,
		"isImport":         true,
		"source":           srcDir,
		"packageSHA256":    packageSHA256,
		"verificationMode": "local-import+layout",
	}); err != nil {
		return SnipasteVersionInfo{}, err
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return SnipasteVersionInfo{}, err
	}
	return SnipasteVersionInfo{
		Version: version, ExePath: filepath.Join(finalDir, exeName), Dir: finalDir,
		Size: copiedInfo.Size(), InstalledAt: installedAt, IsImport: true, Source: srcDir,
		PackageSHA256: packageSHA256, VerificationMode: "local-import+layout",
	}, nil
}

func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	removing := dir + fmt.Sprintf(".removing-%d", time.Now().UnixNano())
	if err := os.Rename(dir, removing); err != nil {
		return fmt.Errorf("无法卸载，相关文件可能正在被 Snipaste 使用；请先从原生托盘退出: %w", err)
	}
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("清理版本目录失败: %w", err)
	}
	return nil
}

func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe := filepath.Join(dir, exeName)
	fi, err := os.Stat(exe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return "", fmt.Errorf("版本 %s 安装损坏：缺少可用的 %s", version, exeName)
	}
	return exe, nil
}

func (m *Manager) resolveVersionDir(version string) (string, error) {
	version = normalizeVersion(version)
	if !plainVersionRe.MatchString(version) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+version)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("版本 %s 未安装，请先下载或导入", version)
	}
	return dir, nil
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	return strings.ReplaceAll(version, " ", "")
}

// extractAll 安全解压并返回实际包含 Snipaste.exe 的安装根目录。
func extractAll(zipPath, stagingDir string) (string, error) {
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", err
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("打开 ZIP 失败: %w", err)
	}
	defer zr.Close()

	var exeCandidates []string
	for _, entry := range zr.File {
		clean := filepath.Clean(filepath.FromSlash(entry.Name))
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("ZIP 含非法路径条目 %q", entry.Name)
		}
		target := filepath.Join(stagingDir, clean)
		rel, err := filepath.Rel(stagingDir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("ZIP 条目逃逸目标目录 %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("ZIP 含不支持的符号链接 %q", entry.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}
		rc, err := entry.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, entry.Mode().Perm())
		if err != nil {
			rc.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return "", fmt.Errorf("读取 ZIP 条目 %q 失败: %w", entry.Name, copyErr)
		}
		if closeErr != nil {
			return "", closeErr
		}
		if strings.EqualFold(filepath.Base(target), exeName) {
			fi, err := os.Stat(target)
			if err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
				exeCandidates = append(exeCandidates, target)
			}
		}
	}
	if len(exeCandidates) != 1 {
		return "", fmt.Errorf("ZIP 布局无效：期望唯一的 %s，实际找到 %d 个", exeName, len(exeCandidates))
	}
	root := filepath.Dir(exeCandidates[0])
	relRoot, err := filepath.Rel(stagingDir, root)
	if err != nil {
		return "", err
	}
	if relRoot != "." && strings.Contains(relRoot, string(filepath.Separator)) {
		return "", fmt.Errorf("ZIP 布局过深：%s 必须位于根目录或单层包装目录", exeName)
	}
	return root, nil
}

func copyPortableDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dstDir, 0755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("导入目录包含不支持的符号链接: %s", path)
		}
		if info.Mode()&os.ModeType != 0 && !info.IsDir() {
			return fmt.Errorf("导入目录包含不支持的特殊文件: %s", path)
		}
		name := strings.ToLower(info.Name())
		if name == "meta.json" || strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, "~") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		return copyFile(path, dst, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
