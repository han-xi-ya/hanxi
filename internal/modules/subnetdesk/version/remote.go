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
	repoOwner  = "zibo-chen"
	repoName   = "SubnetDesk"
	userAgent  = "Hanxi/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 ccswitch/everything 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainSemverTag 严格纯语义版本 tag（格式 vX.Y.Z）：过滤 nightly 等非规范 tag，
// 防其污染版本列表（SubnetDesk 的 nightly 为 prerelease + 非 semver 双重排除）。
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

// digestHex 解析 GitHub 资产 digest（"sha256:<hex>"）为裸 hex；
// 缺失/畸形（长度不符）返回空串——调用方按"该资产不可用"处理。
func digestHex(digest string) string {
	sha := strings.TrimPrefix(strings.ToLower(digest), digestPrefix)
	if len(sha) != 64 {
		return ""
	}
	return sha
}

// findPortableAsset 从 release 资产中筛选 Windows x64 便携 packer exe。
// 资产名形如 subnetdesk-1.3.0-x86_64.exe；同 release 还含 aarch64.exe / msi /
// deb / rpm / AppImage / apk / tar.gz 等——"-x86_64.exe" 结尾天然排除
// aarch64 变体（-aarch64.exe）与安装器（.msi），显式再挡 sciter 变体。
func findPortableAsset(assets []asset, version string) (asset, bool) {
	ver := strings.TrimPrefix(version, "v")
	needName := strings.ToLower("subnetdesk-" + ver + "-x86_64.exe")
	for _, a := range assets {
		if lower := strings.ToLower(a.Name); lower == needName {
			return a, true
		}
	}
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, "-x86_64.exe") && strings.Contains(lower, ver) &&
			!strings.Contains(lower, "sciter") && !strings.Contains(lower, "aarch64") {
			return a, true
		}
	}
	return asset{}, false
}

// findInstallerAsset 筛选 Windows x64 安装版 MSI（subnetdesk-1.3.0-x86_64.msi，
// WiX perMachine 包，上游 fork 保留 RustDesk 完整 msi 工程，真机实证与便携
// exe 成对发布）。官方 digest 缺失的候选视为不可用（完整性第一层校验不能缺位）。
func findInstallerAsset(assets []asset, version string) (asset, string, bool) {
	ver := strings.TrimPrefix(version, "v")
	needName := strings.ToLower("subnetdesk-" + ver + "-x86_64.msi")
	var fallback asset
	var fallbackSHA string
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		sha := digestHex(a.Digest)
		if sha == "" {
			continue
		}
		if lower == needName {
			return a, sha, true
		}
		if fallback == (asset{}) && strings.HasSuffix(lower, "-x86_64.msi") &&
			strings.Contains(lower, ver) && !strings.Contains(lower, "sciter") &&
			!strings.Contains(lower, "aarch64") {
			fallback, fallbackSHA = a, sha
		}
	}
	if fallback != (asset{}) {
		return fallback, fallbackSHA, true
	}
	return asset{}, "", false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []SDRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]SDRelease, error) {
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
// 过滤规则：draft / 非纯语义 tag（nightly 等）/ 无 x64 便携资产 /
// 官方 sha256 缺失的 release 一律不入列表。
func parseReleasesBody(body []byte) ([]SDRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []SDRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非纯 vX.Y.Z 直接丢弃
		if r.Draft || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		sha := digestHex(arch.Digest)
		if sha == "" {
			continue
		}
		item := SDRelease{
			Version:   r.TagName,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
			SHA256:    sha,
		}
		// 安装版为附属通道：上游未带 msi 只置空该版本字段，不影响版本入列
		if inst, instSHA, ok := findInstallerAsset(r.Assets, r.TagName); ok {
			item.InstallerName = inst.Name
			item.InstallerURL = inst.URL
			item.InstallerSize = inst.Size
			item.InstallerSHA256 = instSHA
		}
		list = append(list, item)
	}
	return list, nil
}

var remoteCache = &releaseCache{}
