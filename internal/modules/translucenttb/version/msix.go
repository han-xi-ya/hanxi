// msix.go TranslucentTB MSIX 打包形态的下载与缓存线（与便携 zip 线并行、互不干涉）。
//
// 上游事实（2026-09 实测 GitHub API，releases?per_page=60 全量 20 条）：
//   - 打包形态资产名恒为 bundle.msixbundle（全架构 MSIX 容器），2021.4~2026.2
//     共 10 个 release 携带且零命名漂移；2024.4/2020.x/2019.x/2018.x 等无此资产
//     （2020.2 的 TranslucentTB-package.msix / vclibs.msix 是散装 .msix，不认）；
//   - GitHub 原生资产 digest（sha256:）仅 2026.1/2026.2 提供（digest 机制上线前
//     的老 release 无摘要）——与便携线同口径：无官方摘要不进列表，
//     宁缺毋假，绝不做无校验下载。
//
// 缓存布局 versions/translucenttb/packages/<ver>/bundle.msixbundle，目录命名对齐
// nanaZip（versions/nanazip/packages/<ver>/）先例；该目录在便携线 Tree 的 dirRe
// （^translucenttb_...）匹配半径外，两线共用 versionsDir 根互不踩踏。
// msixbundle 只作整体容器缓存供系统注册（Add-AppxPackage 链在 service/instance
// 线），本包不解压，ZipSlip 类闸门不适用；完整性由"官方摘要必检 + tmp 落位前
// 复核 + rename 原子换装"三段收口（内核 artifact.Fetch + fileSHA256 家族，
// 不重造 HTTP/校验轮子）。
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/packages/go/artifact"
)

const (
	// msixAssetName 上游打包形态资产名（实测 10 个 release 恒定，精确匹配）。
	msixAssetName = "bundle.msixbundle"
	// packageFileName 缓存落位终名：与资产名同字面量，落盘名不吃上游返回的
	// 动态 name，杜绝资产名漂移把缓存文件名带偏。
	packageFileName = msixAssetName

	// 打包形态下载进度词表（照便携线家族口径，verify 独立成段——本线
	// rename 前有独立复核，不折进 downloading）：
	stageDownloading = "downloading"
	stageVerifySHA   = "verify-sha256"
	stageDone        = "done"
)

// MsixRelease 上游一个可用打包形态（Version 与便携线同一规范化口径：YYYY.N，
// 无 v 前缀）。SHA256 为 GitHub API 资产 digest 剥前缀后的官方摘要。
type MsixRelease struct {
	Version string `json:"version"`
	URL     string `json:"url"`    // github.com 直链下载址（主址，镜像由下载时按 assetMirrors 模板扩展）
	SHA256  string `json:"sha256"` // 官方摘要（64 hex，必检）
	Size    int64  `json:"size"`   // 声明字节数（流式上限对齐）
}

// PackageCached 本地一条已落位 msixbundle 容器缓存。
type PackageCached struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// findMsixAsset 从 release 资产中筛选打包形态容器：bundle.msixbundle 精确
// 匹配（大小写不敏感）。便携 zip、setup.exe、.appinstaller、winui/appx 与
// 散装 .msix（2020.2 形态）一概不认。
func findMsixAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if strings.EqualFold(a.Name, msixAssetName) {
			return a, true
		}
	}
	return asset{}, false
}

// parseMsixReleasesBody 解析 GitHub API 响应为打包形态列表（单测直接注入
// 样例响应复用）。过滤规则与 parseReleasesBody 同纪律：draft / 非年份 tag /
// 无 bundle.msixbundle 资产 / 官方摘要缺失或尺寸非法的 release 一律不入列表。
func parseMsixReleasesBody(body []byte) ([]MsixRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []MsixRelease
	for _, r := range releases {
		if r.Draft || !yearTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findMsixAsset(r.Assets)
		if !ok || arch.Size <= 0 {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		list = append(list, MsixRelease{
			Version: r.TagName,
			URL:     assetMirrors(r.TagName, arch.Name)[0],
			SHA256:  sha,
			Size:    arch.Size,
		})
	}
	return list, nil
}

// msixReleaseCache 打包形态远程列表缓存：与 remoteCache 同一拉取通道
// （fetchJSON + apiBaseURLs 镜像回退 + cacheTTL + 网络故障降级旧缓存），
// 独立存储是因为资产 digest/尺寸按 msixbundle 取值、便携解析不携带原始资产。
// 代价是每 TTL 周期最多多一次 GitHub API 请求（未认证 60 次/小时限流下无虞）。
type msixReleaseCache struct {
	mu        sync.Mutex
	data      []MsixRelease
	fetchedAt time.Time
}

func (c *msixReleaseCache) get() ([]MsixRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	body, err := fetchJSON(apiBaseURLs)
	if err != nil {
		if !c.fetchedAt.IsZero() {
			return c.data, nil // 网络异常时降级返回旧缓存（与 remoteCache 同语义）
		}
		return nil, err
	}
	list, err := parseMsixReleasesBody(body)
	if err != nil {
		return nil, err
	}
	c.data = list
	c.fetchedAt = time.Now()
	return list, nil
}

var msixCache = &msixReleaseCache{}

// packagesRoot 打包形态缓存根（versions/translucenttb/packages，目录命名对齐
// nanaZip 的 versions/nanazip/packages 口径）。
func (m *Manager) packagesRoot() string {
	return filepath.Join(m.versionsDir, treeEntryName, "packages")
}

// ListMsixReleases 获取上游可用打包形态列表（10 分钟内存缓存）。
// 纯远程事实（msixCache 为进程级全局，与 remoteCache 同构），故提供包级
// 函数；Manager 方法薄转发，两种调用面并存。缓存命中交回的是共享切片，
// 先拷贝断开引用再返回（与 ListRemote 同纪律）。
func ListMsixReleases(ctx context.Context) ([]MsixRelease, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	list, err := msixCache.get()
	if err != nil {
		return nil, err
	}
	out := make([]MsixRelease, len(list))
	copy(out, list)
	return out, nil
}

// ListMsixReleases Manager 面薄转发，见包级函数注释。
func (m *Manager) ListMsixReleases(ctx context.Context) ([]MsixRelease, error) {
	return ListMsixReleases(ctx)
}

// HasMsixRelease 指定版本上游是否存在可校验的打包形态（降级钮判定用）。
// 远程列表取不到时按 false 处理（保守降级：宁可少亮钮，不亮死钮）；
// "无摘要的老 msix" 不入列表，天然判 false。包级函数 + Manager 薄转发，
// 与 ListMsixReleases 同口径。
func HasMsixRelease(version string) bool {
	list, err := msixCache.get()
	if err != nil {
		return false
	}
	for _, r := range list {
		if r.Version == version {
			return true
		}
	}
	return false
}

// HasMsixRelease Manager 面薄转发，见包级函数注释。
func (m *Manager) HasMsixRelease(version string) bool {
	return HasMsixRelease(version)
}

// PreparePackage 下载指定版本的 bundle.msixbundle，官方摘要校验后缓存落位
// versions/translucenttb/packages/<ver>/bundle.msixbundle，返回终路径。
//
// 纪律对齐 nanaZip EnsureCached 与便携线 Download 家族：
//   - 下载走 Manager 既有 fetch/mirrors 接缝（内核 artifact.Fetch：官方摘要
//     流式+落盘双核、Content-Length/上限双核、镜像回退，fetchBudget 总超时）；
//   - 临时件与终文件同目录（同卷 rename 原子），任一步失败 defer 删临时件，
//     校验失败绝不让脏字节进入终位；rename 前独立复核 fileSHA256 对官方
//     摘要——只有验明正身的字节才允许换装；
//   - 已存在且摘要对过幂等直返（不重下载）；存在但摘要不符（上游同 tag 重传）
//     走重下换装；
//   - progress 阶段词照家族：downloading（percent 为已收/声明总量百分比）→
//     verify-sha256 → done；错误经返回的 error 上抛，进度面不设 error 阶段
//     （回调形状 percent+stage 无消息位，造 error 事件只会丢信息）。
//
// ctx 为 nil 按 Background 处理。版本号先过 YYYY.N 白名单，杜绝路径注入。
func (m *Manager) PreparePackage(ctx context.Context, version string, progress func(percent float64, stage string)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	emit := func(percent float64, stage string) {
		if progress != nil {
			progress(percent, stage)
		}
	}

	version = strings.TrimSpace(version)
	if !yearVersionRe.MatchString(version) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}

	list, err := m.ListMsixReleases(ctx)
	if err != nil {
		return "", fmt.Errorf("获取远程打包形态列表失败: %w", err)
	}
	var target *MsixRelease
	for i := range list {
		if list[i].Version == version {
			target = &list[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("TranslucentTB %s 无打包形态（上游缺 bundle.msixbundle 资产或无官方摘要）", version)
	}

	dir := filepath.Join(m.packagesRoot(), version)
	final := filepath.Join(dir, packageFileName)

	// 幂等直返：终位已有字节与官方摘要对过即复用缓存，不碰网络。
	if _, statErr := os.Stat(final); statErr == nil {
		if got, hashErr := fileSHA256(final); hashErr == nil && strings.EqualFold(got, target.SHA256) {
			emit(100, stageDone)
			return final, nil
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".bundle-*.msixbundle.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath) // 成功换装后为 no-op；任一步失败不留脏文件

	urls := m.mirrors(version, msixAssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   target.SHA256,
		MaxBytes: target.Size, // 与 release API 声明大小对齐：超限即断
		FileName: packageFileName,
	}
	emit(0, stageDownloading)
	fetchErr := m.fetch(ctx, src, tmpPath, func(p artifact.Progress) {
		if p.Stage == artifact.StageDownload && p.Total > 0 {
			percent := float64(p.Done) / float64(p.Total) * 100
			if percent > 100 {
				percent = 100
			}
			emit(percent, stageDownloading)
		}
	}, fetchBudget)
	if fetchErr != nil {
		return "", fmt.Errorf("下载 msixbundle 失败: %w", fetchErr)
	}

	emit(100, stageVerifySHA)
	if got, err := fileSHA256(tmpPath); err != nil {
		return "", err
	} else if !strings.EqualFold(got, target.SHA256) {
		return "", fmt.Errorf("msixbundle sha256 校验失败：期望 %s，实际 %s", target.SHA256, got)
	}

	// 原子换装：Windows rename 不覆盖既有文件，异摘要旧包先让位再改名
	// （走到这里旧终文件必与官方摘要不符，删除不伤幂等复用链）。
	if _, statErr := os.Stat(final); statErr == nil {
		if err := os.Remove(final); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return "", fmt.Errorf("msixbundle 缓存落位失败: %w", err)
	}
	emit(100, stageDone)
	return final, nil
}

// PackageCachePaths 枚举本地 msixbundle 容器缓存（版本降序）。
// 只列"目录名过 YYYY.N 白名单 + 终文件在位且非空"的条目，不做摘要复核——
// 全量哈希复核在 PreparePackage 幂等路径承担，枚举保持轻量；缓存条目是否
// 可删（该版本当前注册在系统）由调用方 service 线拦截，本层无系统注册态、不判。
func (m *Manager) PackageCachePaths() []PackageCached {
	entries, err := os.ReadDir(m.packagesRoot())
	if err != nil {
		return nil // 缓存根不存在 = 零缓存，非错误
	}
	var out []PackageCached
	for _, e := range entries {
		if !e.IsDir() || !yearVersionRe.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(m.packagesRoot(), e.Name(), packageFileName)
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
			continue
		}
		out = append(out, PackageCached{Version: e.Name(), Path: path, Size: fi.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return versioncmp.Compare(out[i].Version, out[j].Version) > 0 })
	return out
}

// RemovePackageCache 删除指定版本的整目录容器缓存（RemoveAll 幂等，目录不在
// 也成功）。非法版本令牌（含路径穿越）先于任何磁盘访问被拒。
// 注意：该版本当前注册在系统时**由调用方（service 线）拦截**，本层不判。
func (m *Manager) RemovePackageCache(version string) error {
	version = strings.TrimSpace(version)
	if !yearVersionRe.MatchString(version) {
		return fmt.Errorf("非法版本号: %q", version)
	}
	return os.RemoveAll(filepath.Join(m.packagesRoot(), version))
}
