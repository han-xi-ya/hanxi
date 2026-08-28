package version

import (
	"debug/pe"
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

const dirPrefix = "mangodisk_"

var (
	plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	dirNameRe      = regexp.MustCompile(`^` + dirPrefix + `\d+\.\d+\.\d+$`)
)

type installMeta struct {
	SchemaVersion    int    `json:"schemaVersion"`
	Version          string `json:"version"`
	InstalledAt      string `json:"installedAt"`
	IsImport         bool   `json:"isImport"`
	Source           string `json:"source"`
	AssetName        string `json:"assetName"`
	ExpectedSize     int64  `json:"expectedSize"`
	InstalledSize    int64  `json:"installedSize"`
	ExpectedSHA256   string `json:"expectedSHA256"`
	InstalledSHA256  string `json:"installedSHA256"`
	FileVersion      string `json:"fileVersion"`
	ProductName      string `json:"productName"`
	VerifiedOfficial bool   `json:"verifiedOfficial"`
}

type Manager struct {
	versionsDir string
	client      *http.Client
}

func NewManager(versionsDir string) *Manager {
	return &Manager{versionsDir: versionsDir, client: &http.Client{Timeout: 10 * time.Minute}}
}

func (m *Manager) ListRemote() ([]MangoDiskRelease, error) { return remoteCache.get() }

func (m *Manager) ListInstalled() ([]MangoDiskVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []MangoDiskVersionInfo
	for _, entry := range entries {
		if !entry.IsDir() || !dirNameRe.MatchString(entry.Name()) {
			continue
		}
		version := "v" + strings.TrimPrefix(entry.Name(), dirPrefix)
		info := m.inspect(filepath.Join(m.versionsDir, entry.Name()), version)
		list = append(list, info)
	}
	return list, nil
}

func (m *Manager) Inspect(version string) (MangoDiskVersionInfo, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return MangoDiskVersionInfo{}, err
	}
	return m.inspect(dir, normalizeVersion(version)), nil
}

func (m *Manager) VerifyBeforeLaunch(version string) (MangoDiskVersionInfo, error) {
	info, err := m.Inspect(version)
	if err != nil {
		return info, err
	}
	switch info.Integrity {
	case IntegrityVerified, IntegrityLocalBaseline:
		return info, nil
	case IntegrityDrifted:
		return info, fmt.Errorf("版本 %s 的程序文件已发生变化，可能由 MangoDisk 内置更新器替换；请重新下载或重新导入", info.Version)
	default:
		return info, fmt.Errorf("版本 %s 安装无效：%s", info.Version, info.IntegrityNote)
	}
}

func (m *Manager) Download(version string, onProgress func(DownloadProgress)) error {
	version = normalizeVersion(version)
	emit := func(stage string, done, total int64, message string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: message})
		}
	}
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	var rel *MangoDiskRelease
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
	if _, err := m.resolveVersionDir(version); err == nil {
		return fmt.Errorf("版本 %s 已安装", version)
	}

	tmp, err := os.CreateTemp("", "hubkit-mangodisk-*.exe")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(version, rel.AssetName), tmpPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	emit("verify", 0, rel.Size, "")
	actualSize, err := fileSize(tmpPath)
	if err != nil {
		return err
	}
	if actualSize != rel.Size {
		return fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actualSize)
	}
	if err := verifySHA256(tmpPath, rel.SHA256); err != nil {
		return fmt.Errorf("官方哈希校验失败: %w", err)
	}
	fileVersion, productName, err := validateExecutable(tmpPath, strings.TrimPrefix(version, "v"))
	if err != nil {
		return err
	}

	emit("install", 0, rel.Size, "")
	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return err
	}
	finalDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
	tmpDir, err := os.MkdirTemp(m.versionsDir, ".mangodisk-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	exePath := filepath.Join(tmpDir, rel.AssetName)
	if err := copyFileTo(tmpPath, exePath); err != nil {
		return err
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	meta := installMeta{
		SchemaVersion: 1, Version: version, InstalledAt: now, Source: rel.AssetName,
		AssetName: rel.AssetName, ExpectedSize: rel.Size, InstalledSize: actualSize,
		ExpectedSHA256: rel.SHA256, InstalledSHA256: fileSHA256(tmpPath), FileVersion: fileVersion,
		ProductName: productName, VerifiedOfficial: true,
	}
	if err := writeJSON(filepath.Join(tmpDir, "meta.json"), meta); err != nil {
		return err
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return fmt.Errorf("安装版本目录失败: %w", err)
	}
	emit("done", rel.Size, rel.Size, "")
	return nil
}

func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func (m *Manager) ResolveExe(version string) (string, error) {
	info, err := m.Inspect(version)
	if err != nil {
		return "", err
	}
	if info.ExePath == "" {
		return "", fmt.Errorf("版本 %s 缺少 MangoDisk 程序文件", version)
	}
	return info.ExePath, nil
}

func (m *Manager) ImportLocal(srcExe string) (MangoDiskVersionInfo, error) {
	srcExe = strings.TrimSpace(srcExe)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return MangoDiskVersionInfo{}, fmt.Errorf("未找到可用的 MangoDisk EXE: %s", srcExe)
	}
	fileVersion, productName, err := validateExecutable(srcExe, "")
	if err != nil {
		return MangoDiskVersionInfo{}, err
	}
	if !plainVersionRe.MatchString(fileVersion) {
		return MangoDiskVersionInfo{}, fmt.Errorf("MangoDisk FileVersion 不是可识别的语义版本: %q", fileVersion)
	}
	version := "v" + fileVersion
	finalDir := filepath.Join(m.versionsDir, dirPrefix+fileVersion)
	if _, err := os.Stat(finalDir); err == nil {
		return MangoDiskVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return MangoDiskVersionInfo{}, err
	}
	tmpDir, err := os.MkdirTemp(m.versionsDir, ".mangodisk-import-*")
	if err != nil {
		return MangoDiskVersionInfo{}, err
	}
	defer os.RemoveAll(tmpDir)
	assetName := filepath.Base(srcExe)
	dst := filepath.Join(tmpDir, assetName)
	if err := copyFileTo(srcExe, dst); err != nil {
		return MangoDiskVersionInfo{}, err
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	hash := fileSHA256(srcExe)
	meta := installMeta{
		SchemaVersion: 1, Version: version, InstalledAt: now, IsImport: true, Source: srcExe,
		AssetName: assetName, InstalledSize: fi.Size(), InstalledSHA256: hash,
		FileVersion: fileVersion, ProductName: productName,
	}
	if err := writeJSON(filepath.Join(tmpDir, "meta.json"), meta); err != nil {
		return MangoDiskVersionInfo{}, err
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return MangoDiskVersionInfo{}, err
	}
	return m.Inspect(version)
}

func (m *Manager) inspect(dir, version string) MangoDiskVersionInfo {
	info := MangoDiskVersionInfo{Version: version, Dir: dir, Integrity: IntegrityInvalid}
	meta, metaErr := readMeta(filepath.Join(dir, "meta.json"))
	if metaErr == nil {
		info.InstalledAt = meta.InstalledAt
		info.IsImport = meta.IsImport
		info.Source = meta.Source
		info.ExpectedSHA256 = meta.ExpectedSHA256
	}
	assetName := meta.AssetName
	if assetName == "" {
		assetName = expectedAssetName(version)
	}
	exe := filepath.Join(dir, assetName)
	fi, err := os.Stat(exe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		info.IntegrityNote = "程序文件缺失或为空"
		return info
	}
	info.ExePath = exe
	info.Size = fi.Size()
	if info.InstalledAt == "" {
		info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
	}
	fileVersion, productName, identityErr := validateExecutable(exe, "")
	info.FileVersion = fileVersion
	info.ProductName = productName
	if identityErr != nil {
		info.IntegrityNote = identityErr.Error()
		return info
	}
	info.CurrentSHA256 = fileSHA256(exe)
	if metaErr != nil || meta.InstalledSHA256 == "" {
		info.IntegrityNote = "安装元信息缺失或损坏"
		return info
	}
	if !strings.EqualFold(info.CurrentSHA256, meta.InstalledSHA256) || fi.Size() != meta.InstalledSize || fileVersion != meta.FileVersion {
		info.Integrity = IntegrityDrifted
		info.IntegrityNote = "程序文件与安装时基线不一致，可能已被上游更新器替换"
		return info
	}
	if meta.VerifiedOfficial {
		if meta.ExpectedSHA256 == "" || !strings.EqualFold(info.CurrentSHA256, meta.ExpectedSHA256) || fi.Size() != meta.ExpectedSize {
			info.Integrity = IntegrityDrifted
			info.IntegrityNote = "程序文件不再匹配官方发布资产"
			return info
		}
		info.Integrity = IntegrityVerified
		info.IntegrityNote = "GitHub 官方 SHA-256 与 PE 身份校验通过"
		return info
	}
	info.Integrity = IntegrityLocalBaseline
	info.IntegrityNote = "本地导入文件与导入时哈希基线一致"
	return info
}

func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(normalizeVersion(version), "v")
	if !plainVersionRe.MatchString(ver) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 v%s 未安装，请先下载或导入", ver)
}

func normalizeVersion(version string) string {
	return "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func expectedAssetName(version string) string {
	return "MangoDisk-" + strings.TrimPrefix(version, "v") + "-windows-portable.exe"
}

func validateExecutable(path, wantVersion string) (string, string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("不是有效的 Windows PE 文件: %w", err)
	}
	machine := f.FileHeader.Machine
	f.Close()
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return "", "", fmt.Errorf("仅支持 Windows x64 MangoDisk，PE machine=0x%04x", machine)
	}
	productName, err := versioninfo.ProductName(path)
	if err != nil || !strings.EqualFold(strings.TrimSpace(productName), "MangoDisk") {
		return "", productName, fmt.Errorf("PE ProductName 不是 MangoDisk")
	}
	fileVersion, err := versioninfo.FileVersion(path)
	if err != nil {
		return "", productName, fmt.Errorf("读取 MangoDisk FileVersion 失败: %w", err)
	}
	fileVersion = normalizeFileVersion(fileVersion)
	if wantVersion != "" && fileVersion != wantVersion {
		return fileVersion, productName, fmt.Errorf("FileVersion 不匹配：期望 %s，实际 %s", wantVersion, fileVersion)
	}
	return fileVersion, strings.TrimSpace(productName), nil
}

func normalizeFileVersion(version string) string {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if idx := strings.IndexAny(version, " ,"); idx >= 0 {
		version = version[:idx]
	}
	parts := strings.Split(version, ".")
	if len(parts) == 4 && parts[3] == "0" {
		return strings.Join(parts[:3], ".")
	}
	return version
}

func readMeta(path string) (installMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return installMeta{}, err
	}
	var meta installMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return installMeta{}, err
	}
	return meta, nil
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

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
