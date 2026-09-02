package gitversion

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	releasesAPIURL  = "https://api.github.com/repos/git-for-windows/git/releases?per_page=10"
	downloadPageURL = "https://git-scm.com/download/win"
	userAgent       = "Hanxi/0.2"
	cacheTTL        = 10 * time.Minute
	probeTimeout    = 12 * time.Second
	maxResponseBody = 4 << 20
	maxReleaseCount = 5
)

// DownloadPageURL 返回 Git 官方网站的 Windows 下载页。
func DownloadPageURL() string { return downloadPageURL }

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

type remoteSource struct {
	client   *http.Client
	endpoint string
}

func defaultRemoteSource() remoteSource {
	return remoteSource{
		client: &http.Client{
			Timeout:       probeTimeout,
			CheckRedirect: checkGitHubRedirect,
		},
		endpoint: releasesAPIURL,
	}
}

func checkGitHubRedirect(req *http.Request, _ []*http.Request) error {
	if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Hostname(), "api.github.com") {
		return fmt.Errorf("拒绝 GitHub API 重定向到非官方地址: %s", req.URL.Redacted())
	}
	return nil
}

type releaseCache struct {
	mu        sync.Mutex
	data      []Release
	fetchedAt time.Time
	source    remoteSource
	now       func() time.Time
}

func newReleaseCache(source remoteSource) *releaseCache {
	return &releaseCache{source: source, now: time.Now}
}

func (c *releaseCache) get() ([]Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.data) > 0 && c.now().Sub(c.fetchedAt) < cacheTTL {
		return cloneReleases(c.data, false), nil
	}
	list, err := c.source.fetchRemote()
	if err == nil && len(list) > 0 {
		c.data = cloneReleases(list, false)
		c.fetchedAt = c.now()
		return cloneReleases(c.data, false), nil
	}
	if len(c.data) > 0 {
		return cloneReleases(c.data, true), nil
	}
	if err == nil {
		err = fmt.Errorf("Git for Windows 官网稳定版本列表为空")
	}
	return nil, err
}

func cloneReleases(src []Release, stale bool) []Release {
	out := make([]Release, len(src))
	copy(out, src)
	for i := range out {
		out[i].Stale = stale
	}
	return out
}

var remoteCache = newReleaseCache(defaultRemoteSource())

// RecentReleases 返回近期最多五个 Git for Windows 官网稳定版本。
func RecentReleases() ([]Release, error) {
	return remoteCache.get()
}

func (s remoteSource) fetchRemote() ([]Release, error) {
	req, err := http.NewRequest(http.MethodGet, s.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 GitHub Releases 请求失败: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取 Git for Windows 稳定版本失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 Git for Windows 稳定版本失败: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxResponseBody {
		return nil, fmt.Errorf("GitHub Releases 响应超过 %d 字节限制", maxResponseBody)
	}

	limited := io.LimitReader(resp.Body, maxResponseBody+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("读取 GitHub Releases 响应失败: %w", err)
	}
	if len(body) > maxResponseBody {
		return nil, fmt.Errorf("GitHub Releases 响应超过 %d 字节限制", maxResponseBody)
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 GitHub Releases 响应失败: %w", err)
	}
	return normalizeReleases(releases), nil
}

func normalizeReleases(src []githubRelease) []Release {
	seen := make(map[string]struct{}, len(src))
	out := make([]Release, 0, min(len(src), maxReleaseCount))
	for _, item := range src {
		if item.Draft || item.Prerelease {
			continue
		}
		version := strings.TrimPrefix(strings.TrimSpace(item.TagName), "v")
		parsed, ok := parseVersion(version)
		if !ok || parsed.revision == 0 || !strings.Contains(strings.ToLower(version), ".windows.") {
			continue
		}
		version = fmt.Sprintf("%d.%d.%d.windows.%d", parsed.major, parsed.minor, parsed.patch, parsed.revision)
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		out = append(out, Release{Version: version, Published: formatPublished(item.PublishedAt)})
	}
	sort.Slice(out, func(i, j int) bool {
		result, _ := Compare(out[i].Version, out[j].Version)
		return result > 0
	})
	if len(out) > maxReleaseCount {
		out = out[:maxReleaseCount]
	}
	return out
}

func formatPublished(raw string) string {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format("2006-01-02")
}

// 验证固定地址在未来被修改时仍保持 HTTPS 与官方主机约束。
func init() {
	for _, raw := range []string{releasesAPIURL, downloadPageURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" {
			panic("envcheck/gitversion: invalid official URL")
		}
	}
}
