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

// verificationMode 记录可信链级别，写入每个缓存包的 meta.json；清单外的包不视为可信。
const verificationMode = "github-sha256+size+zip-crc+bundle-identity+app-manifest+architecture"

// Manager 维护 versions/nanazip/packages/<ver>/ 下的"已校验可信 MSIXBundle 缓存"，
// 供包安装复用（下载一次即可反复部署）。读多写少但写路径由 service 层单操作槽位串行化。
type Manager struct {
	cacheRoot string
	client    *http.Client
}

// NewManager 以 versions 根派生缓存目录；client 15 分钟超时覆盖大包下载。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		cacheRoot: filepath.Join(versionsDir, "nanazip", "packages"),
		client:    &http.Client{Timeout: 15 * time.Minute},
	}
}

// ListReleases 返回远端 stable 发布列表（缓存命中免网络）。
func (m *Manager) ListReleases() ([]Release, error) { return remoteCache.get() }

// ListCached 枚举缓存目录并逐个 readAndVerifyCached：校验不过的条目静默跳过（宁缺毋假）。
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

// EnsureCached 保证指定版本的可信包已缓存：命中且校验通过直接复用；否则从 GitHub 下载
// 并按 verificationMode 全链校验（SHA256/大小/zip CRC/Bundle 身份/AppX 清单/架构），
// 通过后才落缓存目录。版本号先过 stableVersionRe 白名单，杜绝路径注入。
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

// RemoveCached 删除指定版本缓存目录（整目录删除，不做存在性预检——RemoveAll 幂等）。
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
