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
	repoOwner  = "amiaoapp"
	repoName   = "PicLite"
	userAgent  = "Hanxi/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 markeron/ccswitch 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainSemverTag 严格纯语义版本 tag（格式 vX.Y.Z），防未来非规范 tag 污染版本列表
var plainSemverTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

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

// findMSIAsset 从 release 资产中筛选 Windows x64 MSI 安装包。
// PicLite 上游不发便携 zip：NSIS -setup.exe 是 perMachine 安装器（需提权、写卸载
// 注册表），只有 Tauri WiX 的 MSI 能走 msiexec /a 管理提取免安装拆解。
// 资产名恒为 PicLite_<ver>_x64_en-US.msi，精确形状天然排除 arm64 msi /
// *-setup.exe / dmg / deb / AppImage。
func findMSIAsset(assets []asset, version string) (asset, bool) {
	ver := strings.TrimPrefix(version, "v")
	needName := strings.ToLower("piclite_" + ver + "_x64_en-us.msi")
	for _, a := range assets {
		if lower := strings.ToLower(a.Name); lower == needName {
			return a, true
		}
	}
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, "_x64_en-us.msi") && strings.Contains(lower, "_"+ver+"_") {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []PicRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]PicRelease, error) {
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
// 过滤规则：draft / 非纯语义 tag / 无 x64 MSI 资产 / 官方 sha256 缺失的 release 一律不入列表。
func parseReleasesBody(body []byte) ([]PicRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []PicRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非纯 x.y.z 直接丢弃
		if r.Draft || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findMSIAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		// （GitHub 自 2024 年起所有新资产均带 digest，本仓库实测全量覆盖）
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		list = append(list, PicRelease{
			Version:   r.TagName,
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
