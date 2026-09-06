// Package releases 查询 microsoft/WSL 官方 GitHub Releases 版本列表，
// 与 gitversion 同款「固定官方地址 + 缓存 + 过期降级 stale」模式；
// 资产仅保留 Windows MSI 安装包（x64 / ARM64）。
package releases

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
	"hanxi/internal/modules/wsl/netx"
	"hanxi/internal/platform/versioncmp"
)

// versionTagRe 官方 WSL 版本 tag 形如 2.9.10（可带第四段），必须纯数字点分。
var versionTagRe = regexp.MustCompile(`^\d+\.\d+(\.\d+){1,2}$`)

const (
	releasesAPIURL  = "https://api.github.com/repos/microsoft/WSL/releases?per_page=10"
	releasesAtomURL = "https://github.com/microsoft/WSL/releases.atom"
	releasesPage    = "https://github.com/microsoft/WSL/releases"
	cacheTTL        = 10 * time.Minute
	maxResponseBody = 4 << 20
	maxReleaseCount = 10
)

// ReleasesPageURL 返回官方发布页固定地址。
func ReleasesPageURL() string { return releasesPage }

// Asset 单个 MSI 安装资产。
type Asset struct {
	Platform string `json:"platform"` // "x64" | "ARM64"
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

// Release 一个官方版本及其 MSI 资产。
type Release struct {
	Tag        string  `json:"tag"`
	Published  string  `json:"published"`
	Prerelease bool    `json:"prerelease"`
	PageURL    string  `json:"pageUrl"`
	Assets     []Asset `json:"assets"`
}

// Overview 版本管理面板总载荷：远程列表 × 本机版本关系 + 缓存新鲜度。
type Overview struct {
	Releases       []Release `json:"releases"`
	Latest         string    `json:"latest"`
	LocalVersion   string    `json:"localVersion"`
	Relation       string    `json:"relation"` // update | latest | ahead | unknown
	RelationDetail string    `json:"relationDetail"`
	IsStale        bool      `json:"isStale"`
	// Fallback=true 表示 API 被拦（典型 403）、列表来自 Releases Atom 订阅源：
	// tag/链接/命名规律齐全，唯独没有文件大小——前端必须如实标注降级来源。
	Fallback  bool   `json:"fallback"`
	FetchedAt string `json:"fetchedAt"`
}

type githubAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	APIURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Assets      []githubAsset `json:"assets"`
}

type source struct {
	client       *http.Client
	endpoint     string
	atomEndpoint string
}

func defaultSource() source {
	// netx 客户端：直连被 403 时自动跟随系统代理（浏览器同路）；
	// 重定向守卫维持"仅官方主机"约束（与 remoteversion 同款纪律）。
	client := netx.NewClient(remoteversion.ProbeTimeout, func(req *http.Request, _ []*http.Request) error {
		host := strings.ToLower(req.URL.Hostname())
		if req.URL.Scheme != "https" || (host != "api.github.com" && host != "github.com") {
			return fmt.Errorf("拒绝 GitHub 发布列表重定向到非官方地址: %s", req.URL.Redacted())
		}
		return nil
	})
	return source{client: client, endpoint: releasesAPIURL, atomEndpoint: releasesAtomURL}
}

type cache struct {
	mu           sync.Mutex
	data         []Release
	dataFallback bool
	fetchedAt    time.Time
	src          source
	now          func() time.Time
}

func newCache(src source) *cache {
	return &cache{src: src, now: time.Now}
}

var defaultCache = newCache(defaultSource())

// ReleaseSnapshot 一次列表读取的完整结果（含数据新鲜度与来源通道）。
type ReleaseSnapshot struct {
	Releases  []Release
	FetchedAt time.Time
	IsStale   bool
	Fallback  bool // true=API 被拦、来自 Atom 订阅源（无文件大小）
}

// Fetch 返回官方发布列表：API 优先，403 等失败时自动降级 Atom 订阅源；
// 双源皆挂但有过期缓存 → stale 展示。
func Fetch() (ReleaseSnapshot, error) {
	return defaultCache.get()
}

// OverviewFor 组装面板载荷：拉取列表 + 本机 WSL 版本与最新正式版的关系判定。
func OverviewFor(localVersion string) (Overview, error) {
	snap, err := Fetch()
	if err != nil {
		return Overview{}, err
	}
	return overviewFrom(snap, localVersion), nil
}

// overviewFrom 是纯函数（快照 × 本机版本 → 总览），与网络解耦便于单测。
func overviewFrom(snap ReleaseSnapshot, localVersion string) Overview {
	list := snap.Releases
	ov := Overview{
		Releases:  list,
		IsStale:   snap.IsStale,
		Fallback:  snap.Fallback,
		FetchedAt: snap.FetchedAt.Local().Format("2006-01-02 15:04:05"),
	}
	ov.Latest = LatestStable(list)
	switch {
	case localVersion == "":
		ov.Relation = "unknown"
		ov.RelationDetail = "本机 WSL 未安装或版本未知，无法比较"
	case ov.Latest == "":
		ov.Relation = "unknown"
		ov.RelationDetail = "官方未发布可用的正式版"
	default:
		switch versioncmp.Compare(localVersion, ov.Latest) {
		case -1:
			ov.Relation = "update"
			ov.RelationDetail = fmt.Sprintf("本机 %s 落后于最新正式版 %s，建议更新", localVersion, ov.Latest)
		case 0:
			ov.Relation = "latest"
			ov.RelationDetail = "本机已是最新正式版"
		default:
			ov.Relation = "ahead"
			ov.RelationDetail = "本机版本高于官方正式版（可能来自商店通道或预览版）"
		}
	}
	ov.LocalVersion = localVersion
	return ov
}

func (c *cache) get() (ReleaseSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fresh := func(list []Release, fallback bool, at time.Time) ReleaseSnapshot {
		return ReleaseSnapshot{Releases: cloneReleases(list), FetchedAt: at, Fallback: fallback}
	}
	if len(c.data) > 0 && c.now().Sub(c.fetchedAt) < cacheTTL {
		return fresh(c.data, c.dataFallback, c.fetchedAt), nil
	}
	list, fallback, err := c.src.fetchList()
	if err == nil && len(list) > 0 {
		c.data = cloneReleases(list)
		c.dataFallback = fallback
		c.fetchedAt = c.now()
		return fresh(c.data, c.dataFallback, c.fetchedAt), nil
	}
	if len(c.data) > 0 { // 双源皆挂但有过期缓存：降级 stale（前端须标注"数据可能过期"）
		return ReleaseSnapshot{Releases: cloneReleases(c.data), FetchedAt: c.fetchedAt, IsStale: true, Fallback: c.dataFallback}, nil
	}
	if err == nil {
		err = fmt.Errorf("microsoft/WSL 发布列表为空")
	}
	return ReleaseSnapshot{}, err
}

// fetchList 双源链：REST API 优先（信息全），失败自动降级 Atom 订阅源
// （api.github.com 对部分云出口 IP 有区域性拦截，而 github.com 本体通常畅通）。
// 上报的错误以 API 为准（更精确），Atom 仅在救活列表时静默接管。
func (s source) fetchList() ([]Release, bool, error) {
	list, err := s.fetch()
	if err == nil && len(list) > 0 {
		return list, false, nil
	}
	apiErr := err
	if apiErr == nil {
		apiErr = fmt.Errorf("API 返回空列表")
	}
	if s.atomEndpoint == "" {
		return nil, false, apiErr
	}
	atom, atomErr := s.fetchAtom()
	if atomErr == nil && len(atom) > 0 {
		return atom, true, nil
	}
	return nil, false, apiErr
}

func (s source) fetch() ([]Release, error) {
	headers := map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	body, err := remoteversion.Fetch(s.client, s.endpoint, maxResponseBody, headers)
	if err != nil {
		return nil, fmt.Errorf("获取 WSL 官方发布列表失败: %w", err)
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 WSL 官方发布列表失败: %w", err)
	}
	return normalizeReleases(releases), nil
}

func normalizeReleases(src []githubRelease) []Release {
	seen := make(map[string]struct{}, len(src))
	out := make([]Release, 0, min(len(src), maxReleaseCount))
	for _, item := range src {
		if item.Draft {
			continue
		}
		tag := strings.TrimPrefix(strings.TrimSpace(item.TagName), "v")
		if tag == "" || !versionTagRe.MatchString(tag) {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		var assets []Asset
		for _, a := range item.Assets {
			if !strings.HasSuffix(strings.ToLower(a.Name), ".msi") {
				continue
			}
			platform := "x64"
			if strings.Contains(strings.ToLower(a.Name), "arm64") {
				platform = "ARM64"
			}
			assets = append(assets, Asset{Platform: platform, Name: a.Name, Size: a.Size, URL: a.APIURL})
		}
		if len(assets) == 0 { // 无 Windows MSI 的条目（如仅源码包）不进列表
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, Release{
			Tag: tag, Published: formatPublished(item.PublishedAt),
			Prerelease: item.Prerelease, PageURL: item.HTMLURL, Assets: assets,
		})
	}
	if len(out) > maxReleaseCount {
		out = out[:maxReleaseCount]
	}
	return out
}

var (
	atomEntryRe     = regexp.MustCompile(`(?s)<entry>(.*?)</entry>`)
	atomTagRe       = regexp.MustCompile(`releases/tag/(v[0-9][^<"'\s]*|[0-9][^<"'\s]*)`)
	atomPublishedRe = regexp.MustCompile(`<published>([^<]+)</published>`)
)

// fetchAtom 解析 microsoft/WSL 的 Releases Atom 订阅源（github.com 域，API 被拦时的救生通道）。
// 订阅源不含资产清单与预发布标记：tag/日期/链接取真值，MSI 文件名按官方
// 命名规律 wsl.<四段版本>.<arch>.msi 合成，大小未知置 0（前端渲染"—"，不编数字）。
func (s source) fetchAtom() ([]Release, error) {
	body, err := remoteversion.Fetch(s.client, s.atomEndpoint, maxResponseBody, map[string]string{"Accept": "application/atom+xml"})
	if err != nil {
		return nil, fmt.Errorf("获取 WSL 发布订阅源失败: %w", err)
	}
	seen := make(map[string]struct{})
	var out []Release
	for _, entry := range atomEntryRe.FindAllSubmatch(body, -1) {
		block := entry[1]
		m := atomTagRe.FindSubmatch(block)
		if m == nil {
			continue
		}
		tag := strings.TrimPrefix(strings.TrimSpace(string(m[1])), "v")
		if !versionTagRe.MatchString(tag) {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		published := ""
		if p := atomPublishedRe.FindSubmatch(block); p != nil {
			published = formatPublished(string(p[1]))
		}
		page := "https://github.com/microsoft/WSL/releases/tag/" + tag
		dlBase := "https://github.com/microsoft/WSL/releases/download/" + tag
		x64Name, armName := msiAssetName(tag, "x64"), msiAssetName(tag, "arm64")
		out = append(out, Release{
			Tag: tag, Published: published, PageURL: page,
			Assets: []Asset{
				{Platform: "x64", Name: x64Name, URL: dlBase + "/" + x64Name},
				{Platform: "ARM64", Name: armName, URL: dlBase + "/" + armName},
			},
		})
		if len(out) >= maxReleaseCount {
			break
		}
	}
	return out, nil
}

// msiAssetName 官方 MSI 命名规律：tag 补齐四段后拼 wsl.<版本>.<arch>.msi。
func msiAssetName(tag, arch string) string {
	full := tag + strings.Repeat(".0", 4-len(strings.Split(tag, ".")))
	return fmt.Sprintf("wsl.%s.%s.msi", full, arch)
}

// LatestStable 返回正式版（非预发布）列表按版本降序的首项；无则空串。
func LatestStable(list []Release) string {
	stable := make([]Release, 0, len(list))
	for _, r := range list {
		if !r.Prerelease {
			stable = append(stable, r)
		}
	}
	sort.SliceStable(stable, func(i, j int) bool {
		return versioncmp.Compare(stable[i].Tag, stable[j].Tag) > 0
	})
	if len(stable) == 0 {
		return ""
	}
	return stable[0].Tag
}

func formatPublished(raw string) string {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format("2006-01-02")
}

func cloneReleases(src []Release) []Release {
	out := make([]Release, len(src))
	copy(out, src)
	return out
}

// 固定地址自检：未来被误改时启动即暴露（与 gitversion 同款纪律）。
func init() {
	for _, raw := range []string{releasesAPIURL, releasesAtomURL, releasesPage} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" {
			panic("wsl/releases: invalid official URL")
		}
	}
}
