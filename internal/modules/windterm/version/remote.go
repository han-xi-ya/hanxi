// remote.go GitHub releases 远程版本列表（kingToolbox/WindTerm）。
//
// 上游资产事实（2026-09-23 经 GitHub API 实测 32 个 release 全量核对）：
//   - Windows 资产唯一有效形态 `WindTerm_X.Y.Z_Windows_Portable_x86_64.zip`
//     （另有 x86_32 变体与 Linux/Mac 产物，一律不入列表）；
//   - 全部资产无 digest 字段（GitHub 摘要功能对旧式上传不回填）——完整性
//     降级 vscode 三层先例（字节数 + CRC32/布局闸门 + 布局自检，见 manager.go）；
//   - tag 与资产文件名版本存在错位（tag 2.6.0 挂 WindTerm_2.6.1_*.zip，
//     上游修订重传不改 tag）——版本口径以 tag 为准，资产名只做定位；
//   - 预发布 tag 非纯语义（2.7-prerelease-3），plainSemverTag 过滤天然拒收，
//     列表恒为稳定通道（IsPre 字段保留家族契约形状，当前无消费方）。
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

	"hanxi/packages/go/netx"
)

const (
	repoOwner  = "kingToolbox"
	repoName   = "WindTerm"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
// 许可备忘：官方 README 自述"Completely FREE for commercial and
// non-commercial use"、开源部分（除 thirdparty）Apache-2.0，但仓库根
// 无 LICENSE 文件（API license 字段为 null）——托管仅转链官方 releases。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainSemverTag 纯语义 tag（WindTerm 上游 tag 无 v 前缀，如 2.7.0）；
// v0.8 历史 tag 与 2.7-prerelease-3 预发布 tag 均被形状过滤拒收。
var plainSemverTag = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"` // 资产直链（302 到 objects.githubusercontent.com）
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // 上游实测恒缺；在场即按官方摘要升格校验
}

// apiClient GitHub API 请求客户端（12s 超时）
func apiClient() *http.Client {
	return netx.NewClient(12*time.Second, nil)
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

// portableAssetRe Windows x64 便携 zip 形状（x86_32/Linux/Mac/7z 历史形态天然不中）。
var portableAssetRe = regexp.MustCompile(`(?i)^WindTerm_[\d.]+_Windows_Portable_x86_64\.zip$`)

// findPortableAsset 从 release 资产筛选 Windows x64 便携 zip：先精确匹配
// "tag 版本同名"资产（抵御上游混挂多版本资产的罕见场），再按形状兜底
// （上游实测存在 tag 2.6.0 挂 2.6.1 资产的修订重传，精确名不可作硬门）。
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

// releaseCache 远程列表缓存（防 GitHub 限流）。digest 映射与列表同源维护：
// 上游当前全量缺位，未来回填即自动升格 artifact.Fetch 官方摘要主流程。
type releaseCache struct {
	mu        sync.Mutex
	data      []WindTermRelease
	digests   map[string]string // version → 资产 SHA-256（64 位十六进制小写）
	fetchedAt time.Time
}

// digest 返回指定版本的官方资产摘要（上游未提供时为空串）
func (c *releaseCache) digest(version string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.digests[version]
}

func (c *releaseCache) get() ([]WindTermRelease, error) {
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

	var list []WindTermRelease
	digests := make(map[string]string, len(releases))
	for _, r := range releases {
		if r.Draft || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		// 版本展示统一 vX.Y.Z（与家族模块对齐；tag 本身无 v 前缀）
		version := "v" + r.TagName
		list = append(list, WindTermRelease{
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
// 仅接受 sha256 + 64 位十六进制，其余（含缺失/未来换算法）视为无可用摘要。
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
