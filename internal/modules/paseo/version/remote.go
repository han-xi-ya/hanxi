package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

const (
	repoOwner  = "getpaseo"
	repoName   = "paseo"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 recordly/ccswitch 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// semverTag vX.Y.Z 与 vX.Y.Z-pre.N 两类 tag 都接受：上游 beta 通道
// （v0.8.0-beta.1 等）是真实可用的发布资产，通道裁剪在 IsPre 标记层面完成
// （recordly 同纪律）。非规范 tag 拒收。
var semverTag = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.\-]*)?$`)

// localWinArch 本机 GOARCH → electron-builder win 资产架构段。
// 仅 amd64/arm64 有 Windows 产物（上游 win.target 双 arch 实证），其余平台返回
// 空串使资产匹配恒失败——列表自然为空，与"仅 Windows"托管承诺一致。
func localWinArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	}
	return ""
}

// winZipRe Windows 便携 zip 资产名：Paseo-Setup-<ver>-<arch>.zip
// （electron-builder.yml win.artifactName 固定）。必须带 "Paseo-Setup-" 前缀——
// mac 产物 Paseo-<ver>-x64.zip 只隔一个前缀，误收即跨平台炸弹
// （recordly 集成时点名过的"x64.zip 极易被误认作 Windows 便携版"陷阱）。
var winZipRe = map[string]*regexp.Regexp{
	"x64":   regexp.MustCompile(`(?i)^paseo-setup-[0-9][0-9a-zA-Z.\-]*-x64\.zip$`),
	"arm64": regexp.MustCompile(`(?i)^paseo-setup-[0-9][0-9a-zA-Z.\-]*-arm64\.zip$`),
}

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

// findZipAsset 从 release 资产中筛选本机构架构的 Windows 便携 zip。
// zip 资产天然与 blockmap（.exe 才有）互斥，无需再排 blockmap。
func findZipAsset(assets []asset) (asset, bool) {
	re := winZipRe[localWinArch()]
	if re == nil {
		return asset{}, false
	}
	for _, a := range assets {
		if re.MatchString(a.Name) {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。缓存恒存全量（stable+beta），
// 通道裁剪在 get 时按 includePre 完成——切换通道不触发重新拉取。
type releaseCache struct {
	mu        sync.Mutex
	data      []PaseoRelease
	fetchedAt time.Time
}

func (c *releaseCache) get(includePre bool) ([]PaseoRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fetchedAt.IsZero() || time.Since(c.fetchedAt) >= cacheTTL {
		body, err := fetchJSON(apiBaseURLs)
		if err != nil {
			if c.fetchedAt.IsZero() {
				return nil, err
			}
			// 网络异常时降级使用旧缓存（stale-if-error）
		} else {
			list, parseErr := parseReleasesBody(body)
			if parseErr != nil {
				if c.fetchedAt.IsZero() {
					return nil, parseErr
				}
			} else {
				c.data = list
				c.fetchedAt = time.Now()
			}
		}
	}

	if includePre {
		out := make([]PaseoRelease, len(c.data))
		copy(out, c.data)
		return out, nil
	}
	var out []PaseoRelease
	for _, r := range c.data {
		if !r.IsPre {
			out = append(out, r)
		}
	}
	return out, nil
}

// findRelease 在缓存全量列表中定位指定版本（下载解析资产元数据用）。
func (c *releaseCache) findRelease(version string) (PaseoRelease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.data {
		if r.Version == version {
			return r, true
		}
	}
	return PaseoRelease{}, false
}

// parseReleasesBody 解析 GitHub API 响应为版本列表（单测直接注入样例响应复用）。
// 过滤规则：draft / 非语义 tag / 无本机架构 win zip 资产 / 官方 digest 缺失的
// release 一律不入列表。
//
// 数据源铁律：必须以 /releases 为准，绝不拿 /tags 匹配（recordly 集成的
// "空壳 tag"事故教训——按 tag 给前端列版本会展示根本下载不到的版本）。
func parseReleasesBody(body []byte) ([]PaseoRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []PaseoRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非语义版本直接丢弃
		if r.Draft || !semverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findZipAsset(r.Assets)
		if !ok {
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		//（GitHub 自 2024 年起所有新资产均带 digest，本仓库实测 win 资产全覆盖）
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		list = append(list, PaseoRelease{
			Version: strings.TrimPrefix(r.TagName, "v"),
			Tag:     r.TagName,
			// 预发布判定双依据：release 标记 + tag 后缀，两者取或
			IsPre:     r.Prerelease || strings.Contains(r.TagName, "-"),
			Published: r.PublishedAt,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
			SHA256:    sha,
		})
	}
	return list, nil
}

var remoteCache = &releaseCache{}
