package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/product"
	"hanxi/packages/go/hostfeed"

	"hanxi/packages/go/netx"
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

const (
	repoOwner  = "Syngnat"
	repoName   = "GoNavi"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=100"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 markeron 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainSemverTag 严格纯语义版本 tag（格式 vX.Y.Z），防非规范 tag 污染版本列表
// （上游 dev-latest 滚动通道 tag 在此即被丢弃）。
var plainSemverTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// portableAssetRe Windows x64 便携 zip 资产名（实证 v0.8.9 起连续稳定）：
// 天然排除 Installer.msi / Portable.exe（SFX 单文件安装器）/ Linux 与 cli 件。
var portableAssetRe = regexp.MustCompile(`^GoNavi-(\d+\.\d+\.\d+)-Windows-Amd64-Portable\.zip$`)

// sumsAssetRe 备用摘要源附件名（SHA256SUMS / sha256sums.txt 等变体）。
var sumsAssetRe = regexp.MustCompile(`(?i)^sha256sums?(\.txt)?$`)

// sha256HexRe 64 位小写十六进制摘要形（对照 nanazip 纪律：digest 必须过格式闸）。
var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"` // 资产 API 直链（302 到 CDN）
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // 官方 sha256（格式 "sha256:<hex>"）
}

// apiClient GitHub API 请求客户端（12s 超时）
func apiClient() *http.Client {
	return netx.NewClient(12*time.Second, nil)
}

// fetchJSON 按候选地址逐个尝试，成功返回响应体
func fetchJSON(urls []string) ([]byte, error) {
	var lastErr error
	for _, base := range urls {
		url := releaseURL
		if !strings.HasPrefix(base, "https://api.github.com") {
			url = base + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=100"
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := apiClient().Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("GitHub API %s: status %d", url, resp.StatusCode)
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("all GitHub API mirrors failed: %w", lastErr)
}

// findPortableAsset 从 release 资产中筛选 Windows x64 便携 zip：资产名须过
// portableAssetRe 且捕获的版本段与 tag 一致（防同发布混挂他版本资产）。
func findPortableAsset(assets []asset, version string) (asset, bool) {
	ver := strings.TrimPrefix(version, "v")
	for _, a := range assets {
		if m := portableAssetRe.FindStringSubmatch(a.Name); m != nil && m[1] == ver {
			return a, true
		}
	}
	return asset{}, false
}

// findSumsAsset 备用摘要源附件（官方 SHA256SUMS 家族命名，实测上游全量
// 携带 digest 时不会走到；仅作为 digest 缺失时的第二官方来源）。
func findSumsAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if sumsAssetRe.MatchString(a.Name) {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []GoNaviRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]GoNaviRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	body, err := fetchJSON(apiBaseURLs)
	if err != nil {
		if !c.fetchedAt.IsZero() {
			// 网络异常时降级返回旧缓存
			return c.data, nil
		}
		return nil, err
	}

	list, err := parseReleasesBody(body)
	if err != nil {
		return nil, err
	}

	c.data = list
	c.fetchedAt = time.Now()
	return list, nil
}

// parseReleasesBody 解析 GitHub API 响应为版本列表（单测直接注入样例响应复用）。
// 过滤规则：draft / 预发布（dev-latest 滚动通道，上游实证必须过滤）/
// 非纯语义 tag / 无 Windows x64 便携资产（v0.8.9 之前天然为空）/ 官方摘要
// 双源俱缺（无 digest 且无 SHA256SUMS 附件）的 release 一律不入列表——
// 完整性校验第一层不能缺位，宁缺毋假。
func parseReleasesBody(body []byte) ([]GoNaviRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []GoNaviRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非纯 x.y.z 直接丢弃；
		// 预发布通道（dev-latest）整体不入托管列表
		if r.Draft || r.Prerelease || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok || arch.Size <= 0 {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		sumsName := ""
		if !sha256HexRe.MatchString(sha) {
			// 第一层主源缺失：降级备用官方摘要源（SHA256SUMS 附件，下载时解析）；
			// 两路皆无则该 release 不入列表（拒无校验安装的列表层前置）
			sums, sok := findSumsAsset(r.Assets)
			if !sok {
				continue
			}
			sha = ""
			sumsName = sums.Name
		}
		list = append(list, GoNaviRelease{
			Version:   r.TagName,
			Published: r.PublishedAt,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
			SHA256:    sha,
			SumsAsset: sumsName,
			Assets:    notesOf(r.Assets, arch.Name),
		})
	}
	// 镜像回退不保证上游顺序语义，显式按版本降序定序（最新在前，
	// CheckUpdate 与前端都消费此不变式）
	sort.Slice(list, func(i, j int) bool {
		return versioncmp.Compare(strings.TrimPrefix(list[i].Version, "v"),
			strings.TrimPrefix(list[j].Version, "v")) > 0
	})
	return list, nil
}

var remoteCache = &releaseCache{}

// notesOf 全资产平台/形态矩阵（N13 展示层）：hostfeed 分类器统一判型，
// chosen 标"本托管"；签名/清单类元数据已在分类器闸内过滤。
func notesOf(assets []asset, chosen string) []hostfeed.AssetNote {
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		names = append(names, a.Name)
	}
	return hostfeed.Notes(names, chosen)
}
