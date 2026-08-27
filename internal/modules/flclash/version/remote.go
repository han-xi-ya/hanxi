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
	repoOwner  = "chen08209"
	repoName   = "FlClash"
	userAgent  = "HubKit/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与各模块同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// assetVersionRe 从 Windows x64 便携资产名提取版本号：
// FlClash-0.8.96-windows-amd64.zip。
// 天然排除 android apk / linux deb-rpm / macos dmg / arm64 zip / setup.exe。
var assetVersionRe = regexp.MustCompile(`(?i)^flclash-([0-9]+(?:\.[0-9]+)+)-windows-amd64\.zip$`)

// plainVersionRe 规整版本号（2~4 段纯数字，如 0.8.96 / 0.8.96.1）
var plainVersionRe = regexp.MustCompile(`^\d+(\.\d+){1,3}$`)

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

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []FlClashRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]FlClashRelease, error) {
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
// 过滤规则：draft / 无 x64 便携资产 / tag 与资产版本不一致 /
// 官方 sha256 缺失的 release 一律不入列表。
func parseReleasesBody(body []byte) ([]FlClashRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []FlClashRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次
		if r.Draft {
			continue
		}
		var best *asset
		var bestVer string
		for i := range r.Assets {
			a := &r.Assets[i]
			m := assetVersionRe.FindStringSubmatch(a.Name)
			if m == nil {
				continue
			}
			if !plainVersionRe.MatchString(m[1]) {
				continue
			}
			// tag 与资产版本必须一致（FlClash 两者同形，任何差异都是异常信号）
			if strings.TrimPrefix(r.TagName, "v") != m[1] {
				continue
			}
			sha := strings.TrimPrefix(strings.ToLower(a.Digest), digestPrefix)
			if len(sha) != 64 {
				continue
			}
			best, bestVer = a, m[1]
			break
		}
		if best == nil {
			continue
		}
		list = append(list, FlClashRelease{
			Version:   bestVer,
			Tag:       r.TagName,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: best.Name,
			AssetURL:  best.URL,
			Size:      best.Size,
			SHA256:    strings.TrimPrefix(strings.ToLower(best.Digest), digestPrefix),
		})
	}
	return list, nil
}

var remoteCache = &releaseCache{}
