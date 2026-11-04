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

	"hanxi/internal/product"

	"hanxi/packages/go/netx"
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份（与 douzy/ccswitch 同谱）。
var userAgent = product.UserAgent()

const (
	repoOwner  = "can1357"
	repoName   = "oh-my-pi"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"

	// windowsAsset 上游 Windows x64 资产的规范名（omp-windows-arm64.exe 等变体一律不取）
	windowsAsset = "omp-windows-x64.exe"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 rustdesk/ccswitch 同组）。
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainTagRe 匹配纯 semver release tag：vX.Y.Z（omp 实测恒为该形态，
// 未来的通道分叉 tag——如带后缀的 canary——一律不入版本列表）。
var plainTagRe = regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`)

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

// apiClient GitHub API 请求客户端（12s 超时，进程级共享实例；
// 回环恒定直连，外网走 netx 代理链——与 douzy/ccswitch 同一收口纪律）。
var apiClientShared = newSharedAPIClient()

func newSharedAPIClient() *http.Client {
	tr := netx.Transport()
	tr.Proxy = netx.LoopbackAwareProxyFunc()
	return &http.Client{Timeout: 12 * time.Second, Transport: tr}
}

func apiClient() *http.Client { return apiClientShared }

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

// digestHex 解析 GitHub 资产 digest（"sha256:<hex>"）为裸 hex；
// 缺失/畸形（长度不符）返回空串——调用方按"该资产不可用"处理。
func digestHex(digest string) string {
	sha := strings.TrimPrefix(strings.ToLower(digest), digestPrefix)
	if len(sha) != 64 {
		return ""
	}
	return sha
}

// findWindowsAsset 从 release 资产中筛选 Windows x64 单文件 exe。
// 同一 release 还含 darwin/linux 家族、arm64 变体、SHA256SUMS.txt 与 .zip 等。
// 精确名 omp-windows-x64.exe 优先，再走后缀 .exe + 含 windows-x64 兜底
// （显式排除 arm64/裸 latest 形态，防命名漂移误吞变体）。
func findWindowsAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if strings.EqualFold(a.Name, windowsAsset) {
			return a, true
		}
	}
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".exe") && strings.Contains(lower, "windows-x64") &&
			!strings.Contains(lower, "arm64") && !strings.HasSuffix(lower, ".blockmap") {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []OmpRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]OmpRelease, error) {
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
// 过滤规则：draft / 非 vX.Y.Z tag / 无 Windows x64 资产 /
// 官方 sha256 缺失的 release 一律不入列表。prerelease 入列但带标记，
// 更新判定通道在 CheckUpdate 里显式排除（与 ccswitch 同规）。
func parseReleasesBody(body []byte) ([]OmpRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	list := make([]OmpRelease, 0, len(releases))
	for _, r := range releases {
		mm := plainTagRe.FindStringSubmatch(r.TagName)
		if r.Draft || mm == nil {
			continue
		}
		arch, ok := findWindowsAsset(r.Assets)
		if !ok {
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		sha := digestHex(arch.Digest)
		if sha == "" {
			continue
		}
		list = append(list, OmpRelease{
			Version:   r.TagName, // omp tag 与展示版本同形（vX.Y.Z）
			Tag:       r.TagName,
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
