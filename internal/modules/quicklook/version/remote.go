package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	repoOwner  = "QL-Win"
	repoName   = "QuickLook"
	userAgent  = "Hanxi/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 markeron/ccswitch/keyviz 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainSemverTag 严格纯语义版本 tag（格式 x.y.z，无 v 前缀）。
// QuickLook 的正式版 tag 恒为 4.5.0 / 3.7.3 这类裸三段号；本规则同时挡掉
// 名为 "latest" 的滚动预发布标签、历史 0.3.6.1 四段号、以及 v0.1.x 旧式带 v 标签。
var plainSemverTag = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

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

// findPortableAsset 从 release 资产中筛选 Windows 便携 zip。
// 资产名形如 QuickLook-4.5.0.zip——同名并列的还有 .7z/.appx/.exe/.msi，
// 精确小写比对 "quicklook-<ver>.zip" 天然只命中便携 zip（排除安装器与压缩变体）。
func findPortableAsset(assets []asset, version string) (asset, bool) {
	needName := strings.ToLower("QuickLook-" + version + ".zip")
	for _, a := range assets {
		if strings.ToLower(a.Name) == needName {
			return a, true
		}
	}
	// 兜底：宽松匹配（形如 QuickLook-<ver>...zip），排除非 zip 后缀
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".zip") && strings.Contains(lower, strings.ToLower("QuickLook-"+version)) {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []QuickLookRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]QuickLookRelease, error) {
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
// 过滤规则：draft / 非纯语义 tag / 无便携 zip 资产 / 官方 sha256 缺失的 release 一律不入列表。
func parseReleasesBody(body []byte) ([]QuickLookRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []QuickLookRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非纯 x.y.z 直接丢弃
		// （"latest" 滚动预发布、0.3.6.1 四段、v0.1.x 带 v 旧式标签均被此规则挡在门外）
		if r.Draft || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		// （实测 QuickLook 自 4.1.0 起 zip 资产带 digest；更早期缺失的按不可信跳过）
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		list = append(list, QuickLookRelease{
			Version:   r.TagName, // 保持上游裸号 4.5.0，不加 v 前缀
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
			SHA256:    sha,
		})
	}
	return list, nil
}

var remoteCache = &releaseCache{}
