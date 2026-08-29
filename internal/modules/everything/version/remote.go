package version

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	siteBase             = "https://www.voidtools.com"
	downloadsPageURL     = siteBase + "/downloads/"
	userAgent            = "Hanxi/0.2"
	assetURLFormat       = siteBase + "/Everything-%s.x64.zip" // 官方资产命名模板（已用 HEAD 实测稳定）
	shaURLFormat         = siteBase + "/Everything-%s.sha256"  // 官方 sha256 清单（每版本一份）
	probeTimeout         = 10 * time.Second
	cacheTTL             = 10 * time.Minute // 远程槽位内存缓存时长
	maxPageBody          = 4 << 20          // 下载页 HTML 上限（页面实际约百 KB 级）
	shaManifestLineLimit = 1 << 20          // sha256 清单上限
)

// 下载页结构：每个发布通道一个 <h2 id="dl[通道后缀]" ...>Download Everything <版本>[ 通道名]</h2> 区块，
// 区块后跟该版本各变体资产链接。只取每通道的 x64 便携 zip。
var (
	sectionTitleRe = regexp.MustCompile(`<h2[^>]*id="dl[^"]*"[^>]*>Download Everything ([^<]+)</h2>`)
	plainVersionRe = regexp.MustCompile(`^[0-9][0-9a-zA-Z.]+$`)
	shaEntryRe     = regexp.MustCompile(`(?m)^([0-9a-f]{64})\s+\*?([^\s]+)\s*$`)
)

// 内置快照兜底：官网不可达且无缓存时的最后防线（Stale=true 提示用户数据非实时）。
// 数据来源于 2026-08-26 实测的官方下载页与资产 HEAD——版本更替后由页面解析自动覆盖。
var snapshotReleases = []EverythingRelease{
	{Version: "1.4.1.1032", Channel: "stable", Published: "2026-01-23",
		AssetURL: fmt.Sprintf(assetURLFormat, "1.4.1.1032"),
		SHA256:   "698df475ec44e638f66f1b6a32d28fea613cec78d3b6310e6abe53431eeb940c",
		Size:     1906504, Stale: true},
	{Version: "1.5.0.1422b", Channel: "beta", Published: "2026-08-13",
		AssetURL: fmt.Sprintf(assetURLFormat, "1.5.0.1422b"),
		SHA256:   "e011e7f7b2d7e65aaeb02793685370017da007fb04828531e19155eab201dfe6",
		Size:     3500394, Stale: true},
}

// releaseCache 远程槽位列表缓存：TTL 内直返，过期重抓，网络失败降级旧缓存，再无缓存退内置快照。
type releaseCache struct {
	mu        sync.Mutex
	data      []EverythingRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]EverythingRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL && len(c.data) > 0 {
		return c.data, nil
	}

	list, err := fetchRemote()
	if err == nil && len(list) > 0 {
		c.data = list
		c.fetchedAt = time.Now()
		return list, nil
	}
	if len(c.data) > 0 {
		// 网络异常：降级返回旧缓存并打上 stale 标记
		stale := make([]EverythingRelease, len(c.data))
		copy(stale, c.data)
		for i := range stale {
			stale[i].Stale = true
		}
		return stale, nil
	}
	// 无任何可用数据：返回内置快照兜底（含 2026-08 实测的官方哈希，仍可安全下载）
	return snapshotReleases, nil
}

var remoteCache = &releaseCache{}

// fetchRemote 抓取下载页并解析槽位，随后逐资产 HEAD 补大小/时间、拉取官方 sha256。
func fetchRemote() ([]EverythingRelease, error) {
	html, err := fetchPage(downloadsPageURL, maxPageBody)
	if err != nil {
		return nil, err
	}
	list := parseReleases(string(html))
	if len(list) == 0 {
		return nil, fmt.Errorf("解析下载页失败：未找到任何 Everything x64 版本槽位")
	}
	probeAssets(list)
	return list, nil
}

// parseReleases 从下载页 HTML 解析各通道版本槽位。
// 版本与通道名取自区块标题（如 "1.4.1.1032" / "1.5.0.1422b Beta"），
// 资产 href 存在性用精确资产名断言，天然排除 en-US/ARM/Lite/x86 变体。
func parseReleases(html string) []EverythingRelease {
	sections := sectionTitleRe.FindAllStringSubmatch(html, -1)
	var list []EverythingRelease
	seen := map[string]bool{}
	for _, s := range sections {
		fields := strings.Fields(strings.TrimSpace(s[1]))
		if len(fields) == 0 || !plainVersionRe.MatchString(fields[0]) {
			continue
		}
		version := fields[0]
		if seen[version] {
			continue
		}
		channel := "stable"
		if len(fields) > 1 {
			channel = strings.ToLower(fields[1])
		}
		assetName := "Everything-" + version + ".x64.zip"
		if !strings.Contains(html, `href="/`+assetName+`"`) {
			continue // 该区块未提供 x64 便携 zip（未来架构变化时安全跳过）
		}
		seen[version] = true
		list = append(list, EverythingRelease{
			Version:  version,
			Channel:  channel,
			AssetURL: fmt.Sprintf(assetURLFormat, version),
		})
	}
	return list
}

// probeAssets 逐资产 HEAD 探测补充 Size/Published，并拉取官方 sha256 清单。
// 任一探测失败只清空对应字段（降级校验），不整体失败。
func probeAssets(list []EverythingRelease) {
	client := &http.Client{Timeout: probeTimeout}
	for i := range list {
		rel := &list[i]
		// 1. HEAD 补大小与发布时间
		req, err := http.NewRequest(http.MethodHead, rel.AssetURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", userAgent)
			if resp, derr := client.Do(req); derr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					if n, perr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); perr == nil {
						rel.Size = n
					}
					if lm := resp.Header.Get("Last-Modified"); lm != "" {
						if t, terr := time.Parse(http.TimeFormat, lm); terr == nil {
							rel.Published = t.Format("2006-01-02")
						} else {
							rel.Published = lm
						}
					}
				}
			}
		}
		// 2. 官方 sha256 清单（含全部资产，逐行找本版本 x64 zip 条目）
		if manifest, merr := fetchPage(fmt.Sprintf(shaURLFormat, rel.Version), shaManifestLineLimit); merr == nil {
			rel.SHA256 = findSHAInManifest(string(manifest), "Everything-"+rel.Version+".x64.zip")
		}
	}
}

// findSHAInManifest 在 sha256sum 格式清单中查找指定资产名的哈希（大小写敏感，与官方资产名一致）。
func findSHAInManifest(manifest, assetName string) string {
	for _, m := range shaEntryRe.FindAllStringSubmatch(manifest, -1) {
		if m[2] == assetName {
			return m[1]
		}
	}
	return ""
}

// fetchPage GET 指定 URL 并限制响应体大小（防异常响应撑爆内存）。
func fetchPage(url string, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: probeTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	return body, nil
}
