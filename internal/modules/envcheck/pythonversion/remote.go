package pythonversion

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const (
	releasesURL  = "https://www.python.org/api/v2/downloads/release/?is_published=true&pre_release=false"
	lifecycleURL = "https://devguide.python.org/versions/"
	downloadURL  = "https://www.python.org/downloads/windows/"
	maxReleases  = 2 << 20
	maxLifecycle = 2 << 20
)

var (
	supportedSectionRe = regexp.MustCompile(`(?is)<section\s+id=["']supported-versions["'][^>]*>(.*?)</section>`)
	tableBodyRe        = regexp.MustCompile(`(?is)<tbody[^>]*>(.*?)</tbody>`)
	tableRowRe         = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	tableCellRe        = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	tagRe              = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe            = regexp.MustCompile(`\s+`)
)

type pythonRelease struct {
	Name        string `json:"name"`
	IsPublished bool   `json:"is_published"`
	PreRelease  bool   `json:"pre_release"`
	ReleaseDate string `json:"release_date"`
}

type source struct {
	releaseClient     *http.Client
	lifecycleClient   *http.Client
	releaseEndpoint   string
	lifecycleEndpoint string
}

func defaultSource() source {
	return source{
		releaseClient:     remoteversion.NewHTTPClient("www.python.org", "python.org"),
		lifecycleClient:   remoteversion.NewHTTPClient("devguide.python.org"),
		releaseEndpoint:   releasesURL,
		lifecycleEndpoint: lifecycleURL,
	}
}

var cache = remoteversion.NewCache(defaultSource().fetch, cloneCatalog)

// DownloadPageURL 返回 Python 官方 Windows 下载页。
func DownloadPageURL() string { return downloadURL }

// Releases 返回规范化后的官方发布与生命周期集合。
func Releases() (Catalog, bool, time.Time, error) {
	return cache.Get()
}

// MinorChannel 是供 service 层注入的本机 minor 通道 API。
type MinorChannel func(local string) ([]remoteversion.Channel, bool, time.Time, error)

// ChannelsForLocal 构建本机 Python 所属 minor 通道。
// 不会把本机自动迁移到最新大版本；只有官方仍支持且存在正式发布时才返回通道。
func ChannelsForLocal(local string) ([]remoteversion.Channel, bool, time.Time, error) {
	catalog, stale, fetchedAt, err := Releases()
	if err != nil {
		return nil, false, time.Time{}, err
	}
	return BuildChannels(local, catalog), stale, fetchedAt, nil
}

// BuildChannels 生成 Python.org 最新稳定版，并在本机 minor 仍受支持时附加该版本线最新补丁。
func BuildChannels(local string, catalog Catalog) []remoteversion.Channel {
	channels := make([]remoteversion.Channel, 0, 2)
	if len(catalog.Releases) > 0 {
		channels = append(channels, remoteversion.Channel{
			Key: "stable", Label: "Latest stable", Detail: "Python.org 最新正式 CPython",
			Releases: []remoteversion.Release{catalog.Releases[0]},
		})
	}
	minorChannels := BuildMinorChannels(local, catalog)
	for _, channel := range minorChannels {
		if len(channels) > 0 && len(channel.Releases) > 0 && channel.Releases[0].Version == channels[0].Releases[0].Version {
			continue
		}
		channels = append(channels, channel)
	}
	return channels
}

func (s source) fetch() (Catalog, error) {
	releasesBody, err := remoteversion.Fetch(s.releaseClient, s.releaseEndpoint, maxReleases, map[string]string{"Accept": "application/json"})
	if err != nil {
		return Catalog{}, fmt.Errorf("获取 Python 官方发布失败: %w", err)
	}
	lifecycleBody, err := remoteversion.Fetch(s.lifecycleClient, s.lifecycleEndpoint, maxLifecycle, map[string]string{"Accept": "text/html"})
	if err != nil {
		return Catalog{}, fmt.Errorf("获取 Python 官方生命周期失败: %w", err)
	}

	var raw []pythonRelease
	if err := json.Unmarshal(releasesBody, &raw); err != nil {
		return Catalog{}, fmt.Errorf("解析 Python 官方发布失败: %w", err)
	}
	releases := normalizeReleases(raw)
	if len(releases) == 0 {
		return Catalog{}, fmt.Errorf("Python 官方响应中未找到正式版本")
	}
	lifecycles, err := parseLifecycles(string(lifecycleBody))
	if err != nil {
		return Catalog{}, fmt.Errorf("解析 Python 官方生命周期失败: %w", err)
	}
	return Catalog{Releases: releases, Lifecycles: lifecycles}, nil
}

func normalizeReleases(src []pythonRelease) []remoteversion.Release {
	seen := make(map[string]struct{}, len(src))
	out := make([]remoteversion.Release, 0, len(src))
	for _, item := range src {
		if !item.IsPublished || item.PreRelease {
			continue
		}
		parsed, ok := parseFinalName(item.Name)
		if !ok || parsed.major != 3 {
			continue
		}
		published, err := time.Parse(time.RFC3339, item.ReleaseDate)
		if err != nil {
			continue
		}
		version := canonicalVersion(parsed)
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		out = append(out, remoteversion.Release{Version: version, Published: published.UTC().Format("2006-01-02")})
	}
	sort.Slice(out, func(i, j int) bool {
		result, _ := Compare(out[i].Version, out[j].Version)
		return result > 0
	})
	return out
}

func parseLifecycles(body string) ([]Lifecycle, error) {
	section := supportedSectionRe.FindStringSubmatch(body)
	if len(section) != 2 {
		return nil, fmt.Errorf("缺少 supported-versions 区块")
	}
	tbody := tableBodyRe.FindStringSubmatch(section[1])
	if len(tbody) != 2 {
		return nil, fmt.Errorf("缺少受支持版本表格")
	}
	rows := tableRowRe.FindAllStringSubmatch(tbody[1], -1)
	out := make([]Lifecycle, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		cells := tableCellRe.FindAllStringSubmatch(row[1], -1)
		if len(cells) != 6 {
			return nil, fmt.Errorf("受支持版本表格列数变化: %d", len(cells))
		}
		minor := plainText(cells[0][1])
		major, minorNumber, ok := parseMinor(minor)
		if !ok || major != 3 {
			continue // main 等未来开发分支不属于正式 minor 通道。
		}
		minor = canonicalMinor(major, minorNumber)
		status := plainText(cells[2][1])
		if status != "bugfix" && status != "security" {
			continue // prerelease/feature 不得进入正式版本通道。
		}
		eol := plainText(cells[4][1])
		if !validLifecycleDate(eol) {
			return nil, fmt.Errorf("版本 %s 的生命周期日期无效: %q", minor, eol)
		}
		if _, exists := seen[minor]; exists {
			return nil, fmt.Errorf("版本 %s 生命周期重复", minor)
		}
		seen[minor] = struct{}{}
		out = append(out, Lifecycle{Minor: minor, Status: status, EndOfLife: eol})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("受支持版本表格中没有正式版本通道")
	}
	sort.Slice(out, func(i, j int) bool {
		left, _ := parseVersion(out[i].Minor)
		right, _ := parseVersion(out[j].Minor)
		return left.major > right.major || left.major == right.major && left.minor > right.minor
	})
	return out, nil
}

func plainText(raw string) string {
	value := tagRe.ReplaceAllString(raw, " ")
	value = html.UnescapeString(value)
	return strings.TrimSpace(spaceRe.ReplaceAllString(value, " "))
}

func validLifecycleDate(raw string) bool {
	if _, err := time.Parse("2006-01-02", raw); err == nil {
		return true
	}
	_, err := time.Parse("2006-01", raw)
	return err == nil
}

// BuildMinorChannels 从缓存集合中构建本机 minor 通道；无效、未发布或 EOL minor 返回空列表。
func BuildMinorChannels(local string, catalog Catalog) []remoteversion.Channel {
	parsed, ok := parseVersion(local)
	if !ok || parsed.major != 3 {
		return nil
	}
	minor := canonicalMinor(parsed.major, parsed.minor)
	var lifecycle *Lifecycle
	for i := range catalog.Lifecycles {
		if catalog.Lifecycles[i].Minor == minor {
			lifecycle = &catalog.Lifecycles[i]
			break
		}
	}
	if lifecycle == nil || lifecycle.Status != "bugfix" && lifecycle.Status != "security" {
		return nil
	}
	for _, release := range catalog.Releases {
		candidate, ok := parseVersion(release.Version)
		if !ok || candidate.major != parsed.major || candidate.minor != parsed.minor {
			continue
		}
		detail := "Bugfix · EOL " + lifecycle.EndOfLife
		if lifecycle.Status == "security" {
			detail = "Security fixes · EOL " + lifecycle.EndOfLife
		}
		return []remoteversion.Channel{{
			Key: "python-" + minor, Label: "Python " + minor, Detail: detail,
			Releases: []remoteversion.Release{release},
		}}
	}
	return nil
}

func cloneCatalog(src Catalog) Catalog {
	return Catalog{
		Releases:   append([]remoteversion.Release(nil), src.Releases...),
		Lifecycles: append([]Lifecycle(nil), src.Lifecycles...),
	}
}

func init() {
	urls := map[string]map[string]struct{}{
		releasesURL:  {"www.python.org": {}, "python.org": {}},
		lifecycleURL: {"devguide.python.org": {}},
		downloadURL:  {"www.python.org": {}, "python.org": {}},
	}
	for raw, hosts := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			panic("envcheck/pythonversion: invalid official URL")
		}
		if err := remoteversion.ValidateURL(u, hosts); err != nil {
			panic("envcheck/pythonversion: invalid official URL")
		}
	}
}
