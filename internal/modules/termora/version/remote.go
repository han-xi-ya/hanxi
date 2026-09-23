// remote.go GitHub releases 远程版本列表（TermoraDev/termora）。
//
// 上游资产事实（2026-09-23 经 GitHub API 实测 34 个 release 全量核对）：
//   - 全资产带 digest（sha256）→ 完整性走 artifact.Fetch 官方摘要主链
//     （与 markeron/ccswitch 同规格，无 WindTerm 那般的降级链需求）；
//   - Windows 便携形态 `termora_<ver>-windows-x86-64.zip`（另有 .exe 安装器
//     与 aarch64/Linux/Mac 产物，一律不入列）；
//   - tag 形状 x.y.z[-beta.N]（2.x 线全部 prerelease 标记，beta 即事实主干
//     ——recordly 同款通道现实：通道过滤在 IsPre 标记层，不在 tag 格式层）；
//   - 许可：README 明示双许可模型（AGPL-3.0 或联系作者购商业许可）；API
//     license 字段 null 仅因仓库根无独立 LICENSE 文件。hanxi 仅转链官方
//     releases 供用户自行安装，不搬运不打包，分发义务不触发。
package version

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"
	"hanxi/packages/go/hostfeed"
)

const (
	repoOwner  = "TermoraDev"
	repoName   = "termora"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份。
var userAgent = product.UserAgent()

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（家族同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// semverTag x.y.z 与 x.y.z-beta.N 双形 tag 接受（上游无 v 前缀）；
// beta 通道如实入列（IsPre 标记），nightly 等非规范 tag 拒收。
var semverTag = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.\-]*)?$`)

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // GitHub 官方资产摘要 "sha256:<hex>"（上游实测全量在位）
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

// portableAssetRe Windows x86-64 便携 zip 形状（.exe 安装器/aarch64/其它系统
// 产物与 blockmap 类附属天然不中）。
var portableAssetRe = regexp.MustCompile(`(?i)^termora-[\w.\-]+-windows-x86-64\.zip$`)

// findPortableAsset 从 release 资产筛选 Windows x64 便携 zip：
// 先精确匹配"tag 版本同名"资产，再按形状兜底（资产名版本与 tag 一致性
// 由精确层保证，形状层仅防上游改名罕见场）。
func findPortableAsset(assets []asset, tag string) (asset, bool) {
	for _, a := range assets {
		if portableAssetRe.MatchString(a.Name) && strings.Contains(a.Name, tag) {
			return a, true
		}
	}
	for _, a := range assets {
		if portableAssetRe.MatchString(a.Name) {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）：缓存恒存全量
// （stable+beta 双通道，API 原生"新在前后在旧"序原样保留），通道展示
// 不做裁剪——beta 即 Termora 事实主干，藏起来才是欺骗。
type releaseCache struct {
	mu        sync.Mutex
	data      []TermoraRelease
	digests   map[string]string // version → 资产 SHA-256（64 位十六进制小写）
	fetchedAt time.Time
}

// digest 返回指定版本的官方资产摘要（未拉取到/上游未提供时为空）
func (c *releaseCache) digest(version string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.digests[version]
}

func (c *releaseCache) get() ([]TermoraRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	body, err := fetchJSON(apiBaseURLs)
	if err != nil {
		if !c.fetchedAt.IsZero() {
			return c.data, nil // 网络异常时降级返回旧缓存
		}
		return nil, err
	}

	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []TermoraRelease
	digests := make(map[string]string, len(releases))
	for _, r := range releases {
		if r.Draft || !semverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		version := "v" + r.TagName
		list = append(list, TermoraRelease{
			Version:   version,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
			Assets:    notesOf(r.Assets, arch.Name),
		})
		if want, sha, ok := parseAssetDigest(arch.Digest); ok && want == "sha256" {
			digests[version] = sha
		}
	}

	c.data = list
	c.digests = digests
	c.fetchedAt = time.Now()
	return list, nil
}

// parseAssetDigest 解析 GitHub 资产摘要字段（"algo:hex" 形式）；
// 仅接受 sha256 + 64 位十六进制，其余视为无可用摘要（缺摘要即拒装）。
func parseAssetDigest(raw string) (algo, hex64 string, ok bool) {
	algo, hex64, found := strings.Cut(strings.ToLower(strings.TrimSpace(raw)), ":")
	if !found || algo != "sha256" || len(hex64) != 64 {
		return "", "", false
	}
	if _, err := hex.DecodeString(hex64); err != nil {
		return "", "", false
	}
	return algo, hex64, true
}

var remoteCache = &releaseCache{}

// notesOf 全资产平台/形态矩阵（N13 展示层）：hostfeed 分类器统一判型，
// chosen 标"本托管"；签名/清单类元数据已在分类器闸内过滤。
func notesOf(assets []asset, chosen string) []hostfeed.AssetNote {
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		names = append(names, a.Name)
	}
	return hostfeed.Notes(names, chosen)
}
