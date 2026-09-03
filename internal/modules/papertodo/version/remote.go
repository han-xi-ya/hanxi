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
	repoOwner  = "snownico0722"
	repoName   = "PaperTodo"
	userAgent  = "Hanxi/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// ReleasesPageURL 上游 Releases 页面（前端"打开官网下载页"兜底）。
func ReleasesPageURL() string { return RepoURL() + "/releases/latest" }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 markeron/ccswitch 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainTag PaperTodo 版本 tag 形如 v3.31 / v3.2.1——2~3 段纯数字。
// 必须数值分段比较（字典序会判 v3.3 > v3.31，见 service.versionCompare）；
// v2.1rc1 之类非规范 tag 直接丢弃（历史上也带 prerelease:true，双重过滤）。
var plainTag = regexp.MustCompile(`^v\d+(\.\d+){1,2}$`)

// assetRe Windows x64 双变体资产的精确形状：
//
//	PaperTodo-v3.31-win-x64-self-contained.exe / …-no-runtime.exe
//
// 锚定 "PaperTodo-v{版本}-win-x64-" 前缀天然排除 win7BestEffort 变体
// （其形如 PaperTodo-v3.31-win7BestEffort-win-x64-self-contained.exe，
// win7 段插在版本号之后，后缀兜底匹配会误纳，故必须精确匹配）。
var assetRe = regexp.MustCompile(`(?i)^papertodo-v(\d+(?:\.\d+)*)-win-x64-(self-contained|no-runtime)\.exe$`)

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // 官方 sha256（格式 "sha256:<hex>"；PaperTodo 实证暂缺）
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

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []PaperRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]PaperRelease, error) {
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

// findRelease 按 tag 全量查找（不区分大小写），命中缓存条目返回副本语义由调用方保证只读。
func (c *releaseCache) findRelease(version string) (PaperRelease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.data {
		if strings.EqualFold(r.Version, version) {
			return r, true
		}
	}
	return PaperRelease{}, false
}

// parseReleasesBody 解析 GitHub API 响应为版本列表（单测直接注入样例响应复用）。
// 过滤规则：draft / prerelease / tag 非 v[X.Y[.Z]] / 双变体资产不齐 /
// 资产版本号与 tag 不一致的 release 一律不入列表。
//
// 与 ccswitch 的关键差异：**不做"官方 digest 缺失即剔除"的硬过滤**——
// 实证 PaperTodo 全部资产均无 digest 字段，照搬会把版本表清空。
// 完整性校验降级链（字节数 + MZ/PE 核对 + sha256 指纹）见 manager.Download。
func parseReleasesBody(body []byte) ([]PaperRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []PaperRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；预发布（v2.1rc1 等）剔除
		if r.Draft || r.Prerelease || !plainTag.MatchString(r.TagName) {
			continue
		}
		tagCore := strings.TrimPrefix(r.TagName, "v")
		var out PaperRelease
		found := map[string]bool{}
		for _, a := range r.Assets {
			m := assetRe.FindStringSubmatch(a.Name)
			if m == nil || !strings.EqualFold(m[1], tagCore) {
				continue
			}
			pa := PaperAsset{Name: a.Name, Size: a.Size, SHA256: normalizeDigest(a.Digest)}
			switch strings.ToLower(m[2]) {
			case "self-contained":
				out.SelfContained = pa
			case "no-runtime":
				out.NoRuntime = pa
			}
			found[strings.ToLower(m[2])] = true
		}
		if !found["self-contained"] || !found["no-runtime"] {
			continue // 变体不齐（如 v2.x 老资产命名不同）不入列表
		}
		out.Version = r.TagName
		out.Published = r.PublishedAt
		list = append(list, out)
	}
	return list, nil
}

// normalizeDigest 剥离 GitHub digest 前缀并归一大小写；非法长度（缺失/截断）返回空串。
func normalizeDigest(digest string) string {
	sha := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(digest)), digestPrefix)
	if len(sha) != 64 {
		return ""
	}
	return sha
}

var remoteCache = &releaseCache{}
