package version

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform/versioncmp"
)

const (
	officialSiteURL  = "https://www.snipaste.com/"
	downloadsPageURL = "https://www.snipaste.com/download.html"
	sha1ManifestURL  = "https://dl.snipaste.com/sha-1.txt"
	userAgent        = "Hanxi/0.2"
	cacheTTL         = 10 * time.Minute
	probeTimeout     = 12 * time.Second
	maxPageBody      = 4 << 20
	maxManifestBody  = 2 << 20
)

var (
	assetLinkRe   = regexp.MustCompile(`(?i)href=["']([^"']*/archives/(Snipaste-([0-9]+(?:\.[0-9]+)+(?:-Beta[0-9]*)?)-x64\.zip))["']`)
	sha1EntryRe   = regexp.MustCompile(`(?im)^([0-9a-f]{40})\s+\*?([^\r\n]+?)\s*$`)
	dateNearRe    = regexp.MustCompile(`(?i)(20\d{2}[-/.]\d{1,2}[-/.]\d{1,2})`)
	versionNameRe = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)+(?:-Beta[0-9]*)?$`)
)

// OfficialSiteURL 返回前端展示和打开的官方站点地址。
func OfficialSiteURL() string { return officialSiteURL }

type remoteSource struct {
	client       *http.Client
	downloadPage string
	manifestURL  string
}

func defaultRemoteSource() remoteSource {
	return remoteSource{
		client:       &http.Client{Timeout: probeTimeout},
		downloadPage: downloadsPageURL,
		manifestURL:  sha1ManifestURL,
	}
}

type releaseCache struct {
	mu        sync.Mutex
	data      []SnipasteRelease
	fetchedAt time.Time
	source    remoteSource
}

func newReleaseCache(source remoteSource) *releaseCache {
	return &releaseCache{source: source}
}

func (c *releaseCache) get() ([]SnipasteRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.data) > 0 && time.Since(c.fetchedAt) < cacheTTL {
		return cloneReleases(c.data, false), nil
	}
	list, err := c.source.fetchRemote()
	if err == nil && len(list) > 0 {
		c.data = list
		c.fetchedAt = time.Now()
		return cloneReleases(list, false), nil
	}
	if len(c.data) > 0 {
		return cloneReleases(c.data, true), nil
	}
	if err == nil {
		err = fmt.Errorf("官网版本列表为空")
	}
	return nil, err
}

func cloneReleases(src []SnipasteRelease, stale bool) []SnipasteRelease {
	out := make([]SnipasteRelease, len(src))
	copy(out, src)
	if stale {
		for i := range out {
			out[i].Stale = true
		}
	}
	return out
}

var remoteCache = newReleaseCache(defaultRemoteSource())

func (s remoteSource) fetchRemote() ([]SnipasteRelease, error) {
	body, err := fetchPage(s.client, s.downloadPage, maxPageBody)
	if err != nil {
		return nil, fmt.Errorf("获取 Snipaste 官网下载页失败: %w", err)
	}
	list := parseDownloadPage(string(body), s.downloadPage)
	if len(list) == 0 {
		return nil, fmt.Errorf("解析 Snipaste 官网下载页失败：未找到 Windows x64 免安装归档")
	}

	manifest := ""
	if bytes, ferr := fetchPage(s.client, s.manifestURL, maxManifestBody); ferr == nil {
		manifest = string(bytes)
	}
	for i := range list {
		rel := &list[i]
		rel.OfficialHash = findHashInManifest(manifest, rel.AssetName)
		if rel.OfficialHash != "" {
			rel.HashAlgorithm = "sha1"
		}
		probeAsset(s.client, rel)
	}
	return list, nil
}

// parseDownloadPage 只接受官网 archives 下严格命名的 Windows x64 zip。
func parseDownloadPage(body, baseURL string) []SnipasteRelease {
	matches := assetLinkRe.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool, len(matches))
	list := make([]SnipasteRelease, 0, len(matches))
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		assetName := match[2]
		version := match[3]
		if !versionNameRe.MatchString(version) || seen[version] {
			continue
		}
		assetURL, err := resolveURL(baseURL, html.UnescapeString(match[1]))
		if err != nil || !isOfficialAssetURL(assetURL) {
			continue
		}
		seen[version] = true
		list = append(list, SnipasteRelease{
			Version:   version,
			Published: findNearbyDate(body, match[0]),
			IsPre:     strings.Contains(strings.ToLower(version), "beta"),
			AssetName: assetName,
			AssetURL:  assetURL,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].IsPre != list[j].IsPre {
			return !list[i].IsPre
		}
		return versioncmp.Compare(list[i].Version, list[j].Version) > 0
	})
	return list
}

func findNearbyDate(body, linkHTML string) string {
	pos := strings.Index(body, linkHTML)
	if pos < 0 {
		return ""
	}
	start := pos - 800
	if start < 0 {
		start = 0
	}
	end := pos + len(linkHTML) + 200
	if end > len(body) {
		end = len(body)
	}
	matches := dateNearRe.FindAllStringSubmatch(body[start:end], -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.ReplaceAll(strings.ReplaceAll(matches[len(matches)-1][1], "/", "-"), ".", "-")
}

func resolveURL(baseURL, raw string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(u).String(), nil
}

func isOfficialAssetURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "download.snipaste.com" || host == "dl.snipaste.com" || host == "www.snipaste.com" || host == "snipaste.com"
}

func findHashInManifest(manifest, assetName string) string {
	for _, match := range sha1EntryRe.FindAllStringSubmatch(manifest, -1) {
		if len(match) >= 3 && strings.TrimSpace(match[2]) == assetName {
			return strings.ToLower(match[1])
		}
	}
	return ""
}

func probeAsset(client *http.Client, rel *SnipasteRelease) {
	req, err := http.NewRequest(http.MethodHead, rel.AssetURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !isOfficialAssetURL(resp.Request.URL.String()) {
		return
	}
	if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && n > 0 {
		rel.Size = n
	}
	if rel.Published == "" {
		if t, err := time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified")); err == nil {
			rel.Published = t.Format("2006-01-02")
		}
	}
}

func fetchPage(client *http.Client, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}
