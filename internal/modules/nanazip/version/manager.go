package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"hanxi/internal/platform/versioncmp"
)

const verificationMode = "github-sha256+size+zip-crc+bundle-identity+app-manifest+architecture"

type Manager struct {
	cacheRoot string
	client    *http.Client
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		cacheRoot: filepath.Join(versionsDir, "nanazip", "packages"),
		client:    &http.Client{Timeout: 15 * time.Minute},
	}
}

func (m *Manager) ListReleases() ([]Release, error) { return remoteCache.get() }

func (m *Manager) ListCached() ([]CachedPackage, error) {
	entries, err := os.ReadDir(m.cacheRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	result := make([]CachedPackage, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !stableVersionRe.MatchString(entry.Name()) {
			continue
		}
		cached, err := m.readAndVerifyCached(entry.Name())
		if err == nil {
			result = append(result, cached)
		}
	}
	sort.Slice(result, func(i, j int) bool { return versioncmp.Compare(result[i].Version, result[j].Version) > 0 })
	return result, nil
}

func (m *Manager) EnsureCached(version string, onProgress func(DownloadProgress)) (CachedPackage, error) {
	emit := func(stage string, done, total int64, message string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: message})
		}
	}
	if !stableVersionRe.MatchString(version) {
		return CachedPackage{}, fmt.Errorf("非法 NanaZip 版本号: %q", version)
	}

	if cached, err := m.readAndVerifyCached(version); err == nil {
		emit("done", 1, 1, "已复用可信缓存")
		return cached, nil
	}
	releases, err := m.ListReleases()
	if err != nil {
		return CachedPackage{}, err
	}
	var target *Release
	for i := range releases {
		if releases[i].Version == version {
			target = &releases[i]
			break
		}
	}
	if target == nil {
		return CachedPackage{}, fmt.Errorf("远程 stable 列表不存在版本 %s", version)
	}

	if err := os.MkdirAll(m.cacheRoot, 0o755); err != nil {
		return CachedPackage{}, err
	}
	staging, err := os.MkdirTemp(m.cacheRoot, ".installing-"+version+"-")
	if err != nil {
		return CachedPackage{}, err
	}
	defer os.RemoveAll(staging)
	bundlePath := filepath.Join(staging, target.AssetName)

	emit("downloading", 0, target.Size, "")
	if err := downloadTo(m.client, assetMirrors(version, target.AssetName), bundlePath, func(done int64) { emit("downloading", done, target.Size, "") }); err != nil {
		emit("error", 0, target.Size, err.Error())
		return CachedPackage{}, err
	}
	info, err := os.Stat(bundlePath)
	if err != nil {
		return CachedPackage{}, err
	}
	emit("verify-size", info.Size(), target.Size, "")
	if info.Size() != target.Size {
		return CachedPackage{}, fmt.Errorf("下载大小不匹配：期望 %d，实际 %d", target.Size, info.Size())
	}
	emit("verify-sha256", 0, 0, "")
	if err := verifySHA256(bundlePath, target.SHA256); err != nil {
		return CachedPackage{}, err
	}
	emit("verify-bundle", 0, 0, "")
	architectures, err := inspectBundle(bundlePath, version)
	if err != nil {
		return CachedPackage{}, err
	}

	cachedAt := time.Now().Format(time.RFC3339)
	meta := CachedPackage{Version: version, Path: filepath.Join(m.cacheRoot, version, target.AssetName), Dir: filepath.Join(m.cacheRoot, version), Size: target.Size, SHA256: target.SHA256, CachedAt: cachedAt, VerificationMode: verificationMode, Architectures: architectures}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return CachedPackage{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "meta.json"), metaBytes, 0o644); err != nil {
		return CachedPackage{}, err
	}

	finalDir := filepath.Join(m.cacheRoot, version)
	if err := os.RemoveAll(finalDir); err != nil {
		return CachedPackage{}, err
	}
	if err := os.Rename(staging, finalDir); err != nil {
		return CachedPackage{}, err
	}
	emit("done", 1, 1, "")
	return meta, nil
}

func (m *Manager) RemoveCached(version string) error {
	if !stableVersionRe.MatchString(version) {
		return fmt.Errorf("非法 NanaZip 版本号: %q", version)
	}
	return os.RemoveAll(filepath.Join(m.cacheRoot, version))
}

func (m *Manager) readAndVerifyCached(version string) (CachedPackage, error) {
	dir := filepath.Join(m.cacheRoot, version)
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return CachedPackage{}, err
	}
	var cached CachedPackage
	if err := json.Unmarshal(data, &cached); err != nil {
		return CachedPackage{}, err
	}
	if cached.Version != version || cached.VerificationMode != verificationMode {
		return CachedPackage{}, fmt.Errorf("缓存元数据不匹配")
	}
	bundlePath := cached.Path
	if bundlePath == "" {
		return CachedPackage{}, fmt.Errorf("缓存缺少 Bundle 路径")
	}
	cleanDir, err := filepath.Abs(dir)
	if err != nil {
		return CachedPackage{}, err
	}
	cleanPath, err := filepath.Abs(bundlePath)
	if err != nil || filepath.Dir(cleanPath) != cleanDir {
		return CachedPackage{}, fmt.Errorf("缓存 Bundle 路径越界")
	}
	info, err := os.Stat(cleanPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != cached.Size {
		return CachedPackage{}, fmt.Errorf("缓存 Bundle 文件无效")
	}
	if err := verifySHA256(cleanPath, cached.SHA256); err != nil {
		return CachedPackage{}, err
	}
	architectures, err := inspectBundle(cleanPath, version)
	if err != nil {
		return CachedPackage{}, err
	}
	cached.Architectures = architectures
	cached.Path, cached.Dir = cleanPath, cleanDir
	return cached, nil
}
