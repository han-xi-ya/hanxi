package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/product"
	"hanxi/packages/go/hostfeed"

	"hanxi/packages/go/netx"
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

const (
	repoOwner  = "TNTcraftHIM"
	repoName   = "Piik"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=100"

	// assetName Windows x64 便携资产裸名（阶段 0 实证 19 连版稳定，名字不随
	// 版本变化）：精确比对天然排除 .sha256 sidecar（v1.2.0 起已停发，且其
	// 可信度不参与任何校验链）与其他平台/形态资产。
	assetName = "piik-app-windows-amd64.zip"

	// minHostedVersion 托管下限版本（不含）以下一律不入列表：更早版本无本机
	// 读模式（PIIK_CLIENT_GATE_NO_BROWSER 机读 stdout + stdin 优雅停机），托管
	// 引擎的组参与退出通道全部失效，装了也起不来——宁拒不猜。
	minHostedVersion = "1.1.0"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时
	// 限流）。Piik 上游日更风暴节奏不构成收紧缓存的理由：托管版本列表是
	// 低频人机动作，正常展示即可，不做轮询加强（更新感知走 CheckUpdate
	// 契约，由宿主级调度器统一裁决频率）。
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 markeron/dbx 同组）
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainSemverTag 严格纯语义版本 tag（格式 vX.Y.Z），防非规范 tag 污染版本列表
// （阶段 0 实证上游 tag 恒 ^v\d+\.\d+\.\d+$）。
var plainSemverTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// sha256HexRe 64 位小写十六进制摘要形（对照 DBX 纪律：digest 必须过格式闸）。
var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

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
	return netx.NewClient(12*time.Second, nil)
}

// fetchJSON 按候选地址逐个尝试，成功返回响应体
func fetchJSON(urls []string) ([]byte, error) {
	var lastErr error
	for _, base := range urls {
		url := releaseURL
		if !strings.HasPrefix(base, "https://api.github.com") {
			url = base + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=100"
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

// findPortableAsset 从 release 资产中筛选 Windows x64 便携 zip：裸名精确
// 比对（assetName 19 连版稳定，名字不含版本段，无须做版本一致性核验；
// 同发布混挂他版本包的风险由"每 tag 各自携带同名资产"的上游事实消解）。
// 同名 .sha256 sidecar 与 mac/linux 变体因名字不同天然出局。
func findPortableAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if a.Name == assetName {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []PiikRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]PiikRelease, error) {
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
// 过滤规则：draft / 预发布 / 非纯语义 tag / 低于托管下限 v1.1.0 / 无裸名便携
// 资产 / size<=0 的 release 一律不入列表；官方摘要缺失或非法（digest 为空、
// 剥前缀后非 64 位 hex）同样不入——GitHub digest 是唯一信任根（.sha256
// sidecar 自 v1.2.0 停发、不可依赖），完整性校验第一层缺位即宁拒不猜，
// 拒无校验安装的判定前移到列表层。
func parseReleasesBody(body []byte) ([]PiikRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []PiikRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非纯 x.y.z、
		// 预发布、低于托管下限直接丢弃
		if r.Draft || r.Prerelease || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		if versioncmp.Compare(strings.TrimPrefix(r.TagName, "v"), minHostedVersion) < 0 {
			continue
		}
		arch, ok := findPortableAsset(r.Assets)
		if !ok || arch.Size <= 0 {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		if !sha256HexRe.MatchString(sha) {
			continue
		}
		list = append(list, PiikRelease{
			Version:   r.TagName,
			Published: r.PublishedAt,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
			SHA256:    sha,
			Assets:    notesOf(r.Assets, arch.Name),
		})
	}
	// 上游发布频繁且 GitHub 返回序不保证语义序；显式按版本降序定序
	//（最新在前，CheckUpdate 与前端都消费此不变式）
	sort.Slice(list, func(i, j int) bool {
		return versioncmp.Compare(strings.TrimPrefix(list[i].Version, "v"),
			strings.TrimPrefix(list[j].Version, "v")) > 0
	})
	return list, nil
}

var remoteCache = &releaseCache{}

// notesOf 全资产平台/形态矩阵（N13 展示层）：hostfeed 分类器统一判型，
// chosen 标"本托管"。上游 .sha256 sidecar（单文件校验和形态）不在
// hostfeed.IsMetadata 的 sig/sha256sums 覆盖面内（会被机械判成 archive
// 混入展示矩阵），本模块自持闸剔除——它不是发布物，更不是校验依据
// （v1.2.0 起已停发，唯一信任根恒为 GitHub digest）。
func notesOf(assets []asset, chosen string) []hostfeed.AssetNote {
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		if strings.HasSuffix(strings.ToLower(a.Name), ".sha256") {
			continue
		}
		names = append(names, a.Name)
	}
	return hostfeed.Notes(names, chosen)
}
