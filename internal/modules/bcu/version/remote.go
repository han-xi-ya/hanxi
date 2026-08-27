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
	repoOwner  = "BCUninstaller" // 仓库已被重命名过（Klocman 名下 301 到组织名下），常量记最终归属
	repoName   = "Bulk-Crap-Uninstaller"
	userAgent  = "HubKit/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 markeron/ccswitch 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// assetVersionRe 从自包含便携资产名提取完整版本号：
// BCUninstaller_6.2.0_portable.zip / BCUninstaller_6.1.0.1_portable.zip /
// BCUninstaller_6.0.0_portable-x64.zip——后缀 -x64 与无后缀两种形态并存。
// 天然排除 setup.exe、Localisation_Pack.zip。
var assetVersionRe = regexp.MustCompile(`(?i)^bcuninstaller_([0-9]+(?:\.[0-9]+)+)_portable(?:-x64)?\.zip$`)

// fddVersionRe 从框架依赖资产名提取完整版本号与目标框架：
// BCUninstaller_6.2.0_net8.0-windows10.0.18362.0.zip → 6.2.0, net8.0。
// 目标框架返回出来用于"是否有对应桌面运行时"的推荐判断（net6/net8 不同）。
var fddVersionRe = regexp.MustCompile(`(?i)^bcuninstaller_([0-9]+(?:\.[0-9]+)+)_(net[0-9]+\.[0-9]+)-windows[0-9.]+\.zip$`)

// plainVersionRe 规整版本号（3~4 段纯数字，如 6.2.0 / 6.1.0.1）
var plainVersionRe = regexp.MustCompile(`^\d+(\.\d+){2,3}$`)

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

// matchTagPrefix tag 去 v 后必须是资产版本号的按段前缀：
// v6.2 → 6.2.0 ✅；v6.1 → 6.1.0.1 ✅；v6.0 → 6.0.0 ✅。防未来串版。
func matchTagPrefix(tag, assetVersion string) bool {
	tagVer := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if tagVer == "" {
		return false
	}
	return assetVersion == tagVer || strings.HasPrefix(assetVersion, tagVer+".")
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []BCURelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]BCURelease, error) {
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
// 过滤规则：draft / 资产版本号非 3~4 段 / tag 与资产版本不一致 / 官方 sha256
// 缺失（5.8 时代的旧资产没有 digest，完整性校验第一层不能缺位）一律不入列表。
func parseReleasesBody(body []byte) ([]BCURelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []BCURelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次
		if r.Draft {
			continue
		}
		// 主资产：自包含便携版（缺失/无 digest 则整个 release 不入列表——
		// 首选形态的完整性不能降级）
		portable, portableVer := pickAsset(r.Assets, r.TagName, assetVersionRe)
		if portable == nil {
			continue
		}
		rel := BCURelease{
			Version:   portableVer,
			Tag:       r.TagName,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: portable.Name,
			AssetURL:  portable.URL,
			Size:      portable.Size,
			SHA256:    strings.TrimPrefix(strings.ToLower(portable.Digest), digestPrefix),
		}
		// 可选增强：框架依赖变体（digest 缺失则变体不可用，不影响主资产）
		if fdd, _ := pickAsset(r.Assets, r.TagName, fddVersionRe); fdd != nil {
			rel.FddName = fdd.Name
			rel.FddURL = fdd.URL
			rel.FddSize = fdd.Size
			rel.FddSHA256 = strings.TrimPrefix(strings.ToLower(fdd.Digest), digestPrefix)
		}
		list = append(list, rel)
	}
	return list, nil
}

// pickAsset 按资产名正则与 tag 一致性选出合格资产：
// 版本号须为规整 3~4 段且与 tag 按段前缀一致；digest 必须完整（64 位 hex）。
func pickAsset(assets []asset, tag string, nameRe *regexp.Regexp) (*asset, string) {
	for i := range assets {
		a := &assets[i]
		m := nameRe.FindStringSubmatch(a.Name)
		if m == nil {
			continue
		}
		if !plainVersionRe.MatchString(m[1]) {
			continue
		}
		if !matchTagPrefix(tag, m[1]) {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(a.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		return a, m[1]
	}
	return nil, ""
}

var remoteCache = &releaseCache{}
