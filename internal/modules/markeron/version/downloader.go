package version

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"
)

const (
	repoOwner  = "ifer47"
	repoName   = "markeron"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute
)

// userAgent 统一引产品身份（构建期 -X 注入版本后自动跟随，不再手抄版本串）
var userAgent = product.UserAgent()

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainSemverTag 严格纯语义版本 tag（格式 vX.Y.Z），防未来非规范 tag 污染版本列表
var plainSemverTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
	Body        string  `json:"body"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // 资产直链（302 到 objects.githubusercontent.com）
	Size int64  `json:"size"`
	// Digest GitHub 官方发布的资产摘要（形如 "sha256:<hex>"）。上游虽无独立
	// checksums 资产，API 摘要即官方信任根——下载校验（artifact.Fetch）以它为准。
	Digest string `json:"digest"`
}

// apiClient GitHub API 请求客户端（12s 超时）
func apiClient() *http.Client {
	return &http.Client{Timeout: 12 * time.Second}
}

// fetchJSON 按候选地址逐个尝试，成功返回响应体
func fetchJSON(urls []string) ([]byte, error) {
	var lastErr error
	for _, base := range urls {
		url := releaseURL
		if !strings.HasPrefix(base, "https://api.github.com") {
			url = base + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"
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

// findPortableAsset 从 release 资产中筛选 Windows x64 便携 zip。
// 资产名形如 MarkerOn_2.9.4_x64_portable.zip，先精确匹配再后缀兜底，
// 后缀天然排除安装包/源码包。
func findPortableAsset(assets []asset, version string) (asset, bool) {
	ver := strings.TrimPrefix(version, "v")
	needName := strings.ToLower("markeron_" + ver + "_x64_portable.zip")
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if lower == needName {
			return a, true
		}
	}
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, "_x64_portable.zip") && strings.Contains(lower, strings.ToLower(ver)) {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。MarkerOn 无独立 checksums 资产，
// 但 GitHub API 的资产 digest 字段提供官方 SHA-256——单独留一份 version→digest
// 映射供下载校验使用（不进 MarkerRelease 对外契约面）。
type releaseCache struct {
	mu        sync.Mutex
	data      []MarkerRelease
	digests   map[string]string // version → 资产 SHA-256（64 位十六进制小写）
	fetchedAt time.Time
}

// digest 返回指定版本的官方资产摘要（未拉取到/上游未提供时为空）
func (c *releaseCache) digest(version string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.digests[version]
}

func (c *releaseCache) get() ([]MarkerRelease, error) {
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

	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []MarkerRelease
	digests := make(map[string]string, len(releases))
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非纯 x.y.z 直接丢弃
		if r.Draft || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		list = append(list, MarkerRelease{
			Version:   r.TagName,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
		})
		if want, sha, ok := parseAssetDigest(arch.Digest); ok && want == "sha256" {
			digests[r.TagName] = sha
		}
	}

	c.data = list
	c.digests = digests
	c.fetchedAt = time.Now()
	return list, nil
}

// parseAssetDigest 解析 GitHub 资产摘要字段（"algo:hex" 形式）；
// 仅接受 sha256 + 64 位十六进制，其余（含缺失/未来换算法）视为无可用摘要。
func parseAssetDigest(raw string) (algo, hex64 string, ok bool) {
	algo, hex64, found := strings.Cut(strings.ToLower(strings.TrimSpace(raw)), ":")
	if !found || algo != "sha256" || len(hex64) != 64 {
		return "", "", false
	}
	if _, err := hex.DecodeString(hex64); err != nil {
		return "", "", false
	}
	return algo, hex64, true
}

var remoteCache = &releaseCache{}
