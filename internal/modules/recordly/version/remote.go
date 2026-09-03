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
	repoOwner  = "webadderallorg"
	repoName   = "Recordly"
	userAgent  = "Hanxi/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"

	// installerAssetName electron-builder artifactName 固定为 ${productName}-windows-${arch}.${ext}
	// （实测 46 个 release 全量一致），无 arm64 Windows 变体
	installerAssetName = "recordly-windows-x64.exe"

	// sumsAssetName 官方随包发布的校验和清单（尽力交叉比对，缺失不阻断）。
	// 注意：release 下载 URL 大小写敏感，此常量以原名直拼 assetMirrors
	sumsAssetName = "SHA256SUMS.txt"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 ccswitch 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// semverTag vX.Y.Z 与 vX.Y.Z-pre.N 两类 tag 都接受：
// Recordly 的 beta 通道（v1.3.5-beta.2）是真实可用的发布资产，
// 与 ccswitch 只收纯语义 tag 不同——通道过滤在 IsPre 标记层面完成，
// 而非在 tag 格式层面一刀切。非规范 tag（nightly 等）仍拒收。
var semverTag = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.\-]*)?$`)

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

// findInstallerAsset 从 release 资产中筛选 Windows x64 NSIS 安装器。
// 同名排除 blockmap（electron-builder 增量更新元数据）、dmg/zip（macOS 产物，
// 其中 x64.zip 极易被误认作 Windows 便携版——它 mac 专用）、AppImage、latest*.yml。
func findInstallerAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if strings.EqualFold(a.Name, installerAssetName) && !strings.HasSuffix(strings.ToLower(a.Name), ".blockmap") {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。缓存恒存全量（stable+beta），
// 通道裁剪在 get 时按 includePre 完成——切换通道不触发重新拉取。
type releaseCache struct {
	mu        sync.Mutex
	data      []RecordlyRelease
	fetchedAt time.Time
}

func (c *releaseCache) get(includePre bool) ([]RecordlyRelease, error) {
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
		out := make([]RecordlyRelease, len(c.data))
		copy(out, c.data)
		return out, nil
	}
	var out []RecordlyRelease
	for _, r := range c.data {
		if !r.IsPre {
			out = append(out, r)
		}
	}
	return out, nil
}

// findRelease 在缓存全量列表中定位指定版本（安装/卸载解析资产元数据用）。
func (c *releaseCache) findRelease(version string) (RecordlyRelease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.data {
		if r.Version == version {
			return r, true
		}
	}
	return RecordlyRelease{}, false
}

// parseReleasesBody 解析 GitHub API 响应为版本列表（单测直接注入样例响应复用）。
// 过滤规则：draft / 非语义 tag / 无 win-x64 安装器资产 / 官方 digest 缺失的 release
// 一律不入列表。
//
// 数据源铁律：必须以 /releases 为准，绝不拿 /tags 匹配——实测上游存在
// "有 git tag 但 release 从未发布"（v1.3.4 空壳 tag，API 404）的发布事故，
// 按 tag 给前端列版本会展示根本下载不到的版本。
func parseReleasesBody(body []byte) ([]RecordlyRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []RecordlyRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非语义版本直接丢弃
		if r.Draft || !semverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findInstallerAsset(r.Assets)
		if !ok {
			// 如 v1.2.0：exe 资产缺失只剩 blockmap 的残缺发布，跳过
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		// （GitHub 自 2024 年起所有新资产均带 digest，本仓库实测 46/46 覆盖）
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		list = append(list, RecordlyRelease{
			Version: r.TagName,
			// 预发布判定双依据：release 标记 + tag 后缀（上游 beta 手工发布时
			// 偶有 prerelease=false 但 tag 带 -beta.N 的场，两者取或）
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
