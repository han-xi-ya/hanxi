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

	"hanxi/internal/product"
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

const (
	repoOwner  = "jiji262"
	repoName   = "douyin-downloader"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 rustdesk/ccswitch 同组）。
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// desktopTagRe 匹配桌面版 release tag：desktop-vX.Y.Z，捕获组为纯版本号。
// 主仓库混合 Python CLI 源码与文档，releases 里也只认 desktop-v 前缀的桌面版产物，
// 其余（如未来的纯 CLI tag）一律不入版本列表。
var desktopTagRe = regexp.MustCompile(`^desktop-v(\d+\.\d+\.\d+)$`)

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

// findInstallerAsset 从 release 资产中筛选 Windows 安装包 Douzy-Setup-<ver>.exe。
// 同一 release 还含 mac 的 dmg/zip、各资产的 .blockmap（差量更新）与 .yml/.json 清单。
// 精确名（douzy-setup-<ver>.exe）优先，再走后缀 .exe + 含 douzy-setup + 含版本号兜底；
// .blockmap 以 .blockmap 结尾天然被 .exe 后缀排除，显式再挡一次防命名漂移。
// 入参 ver 为从 tag 抽出的纯版本号（如 0.11.5）。
func findInstallerAsset(assets []asset, ver string) (asset, bool) {
	needName := strings.ToLower("douzy-setup-" + ver + ".exe")
	for _, a := range assets {
		if strings.ToLower(a.Name) == needName {
			return a, true
		}
	}
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".exe") && strings.Contains(lower, "douzy-setup") &&
			strings.Contains(lower, ver) && !strings.Contains(lower, "blockmap") {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []DouzyRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]DouzyRelease, error) {
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
// 过滤规则：draft / 非 desktop-vX.Y.Z tag / 无 Windows 安装包 /
// 官方 sha256 缺失的 release 一律不入列表。
func parseReleasesBody(body []byte) ([]DouzyRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []DouzyRelease
	for _, r := range releases {
		mm := desktopTagRe.FindStringSubmatch(r.TagName)
		if r.Draft || mm == nil {
			continue
		}
		ver := mm[1]
		arch, ok := findInstallerAsset(r.Assets, ver)
		if !ok {
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		sha := digestHex(arch.Digest)
		if sha == "" {
			continue
		}
		list = append(list, DouzyRelease{
			// tag desktop-vX.Y.Z → 显示规范成 vX.Y.Z，Tag 字段保留原始值供下载 URL 构造
			Version:   "v" + ver,
			Tag:       r.TagName,
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
