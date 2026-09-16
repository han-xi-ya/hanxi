package gitversion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const (
	releasesAPIURL  = "https://api.github.com/repos/git-for-windows/git/releases?per_page=10"
	downloadPageURL = "https://git-scm.com/download/win"
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
		client:   remoteversion.NewHTTPClient("api.github.com"),
		endpoint: releasesAPIURL,
	}
}

func (s remoteSource) fetchRemote() ([]Release, error) {
	body, err := remoteversion.Fetch(s.client, s.endpoint, maxResponseBody, map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	})
	if err != nil {
		return nil, fmt.Errorf("获取 Git for Windows 稳定版本失败: %w", err)
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 GitHub Releases 响应失败: %w", err)
	}
	list := normalizeReleases(releases)
	if len(list) == 0 {
		return nil, fmt.Errorf("Git for Windows 官网稳定版本列表为空")
	}
	return list, nil
}

// releaseGetter 抽象 remoteversion.Cache 的读取接口（TTL/并发合并/stale-if-error
// 语义即该公共缓存本体，单测以假实现覆盖 stale 标记与错误透传两条支路）。
type releaseGetter interface {
	Get() ([]Release, bool, time.Time, error)
}

// cloneReleases 深拷贝：缓存内部数据与返回值共享引用会让调用方误改缓存。
func cloneReleases(src []Release) []Release {
	return append([]Release(nil), src...)
}

func cachedReleases(c releaseGetter) ([]Release, error) {
	list, stale, _, err := c.Get()
	if err != nil {
		return nil, err
	}
	if stale {
		// stale-if-error 回吐：数据可展示但必须标注陈旧
		for i := range list {
			list[i].Stale = true
		}
	}
	return list, nil
}

var remoteCache releaseGetter = remoteversion.NewCache(defaultRemoteSource().fetchRemote, cloneReleases)

// RecentReleases 返回近期最多五个 Git for Windows 官网稳定版本。
func RecentReleases() ([]Release, error) {
	return cachedReleases(remoteCache)
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
