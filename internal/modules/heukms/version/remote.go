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
	"hanxi/packages/go/hostfeed"

	"hanxi/packages/go/netx"
)

// userAgent 统一产品 UA（派生自 internal/product，随构建注入版本自动跟随）。
var userAgent = product.UserAgent()

const (
	repoOwner  = "zbezj"
	repoName   = "HEU_KMS_Activator"
	releaseAPI = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）。
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 形如 "sha256:<hex>"。
	digestPrefix = "sha256:"
)

// RepoURL 上游仓库地址；ReleasePagePrefix 供逐版本拼发布页链接。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// ReleasePagePrefix 发布页前缀（拼 tag 得该版页面，手动取主程序的落点）。
func ReleasePagePrefix() string { return RepoURL() + "/releases/tag/" }

// apiBaseURLs GitHub API 候选前缀：直连优先，镜像逐个回退（markeron/ccswitch 同组）。
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

// plainNumericTag 上游 tag 形状：纯三段数字、**无 v 前缀**（64.0.0）。
// 实测存在 24.4.1RP / 24.4.1Beta 这类带后缀 tag，一并丢弃，防污染版本列表。
var plainNumericTag = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

type release struct {
	TagName     string  `json:"tag_name"`
	HTMLURL     string  `json:"html_url"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"` // 资产 API 直链（302 到 CDN）
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // 官方 sha256（"sha256:<hex>"，63.1.0 前为空）
}

// apiClient GitHub API 请求客户端（12s 超时）。
func apiClient() *http.Client { return netx.NewClient(12*time.Second, nil) }

// fetchJSON 按候选地址逐个尝试，成功返回响应体。
func fetchJSON(bases []string) ([]byte, error) {
	var lastErr error
	for _, base := range bases {
		url := releaseAPI
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

// findHostedAsset 选出本模块可自动安装的资产：**必须是 rar**。
//
// 取舍实证（见本包包注释）：iso 的真实内容在 UDF 侧，纯 Go 与系统 bsdtar 都读不到；
// rar 由 rardecode 支持 RAR5 且实测解出产物哈希与上游逐版公布值一致。iso/rar 双发的
// 新版本取 rar；zip 时代（≤40.0.0）虽然内核 UnpackZip 能解，但那些版本距今三年、
// 不支持新系统与新版 Office，纳入下载通道只会制造"能装但没用"的假选项，故一律不收。
func findHostedAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if strings.EqualFold(extOf(a.Name), ".rar") {
			return a, true
		}
	}
	return asset{}, false
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// releaseCache 远程列表缓存（防 GitHub 限流；网络异常降级回旧值）。
type releaseCache struct {
	mu        sync.Mutex
	data      []HeuRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]HeuRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	body, err := fetchJSON(apiBaseURLs)
	if err != nil {
		if !c.fetchedAt.IsZero() {
			return c.data, nil // 降级返回旧缓存
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

// parseReleasesBody 解析 GitHub API 响应为可安装版本列表（单测直接注入样例响应复用）。
//
// 四条入选闸（缺一不入列，与"宁拒不猜"的内核纪律同源）：
//  1. 非 draft；
//  2. tag 为纯三段数字（丢弃 24.4.1RP / 24.4.1Beta 之类）；
//  3. 存在可解包的 rar 资产（zip 时代与仅 iso 的形态不入列）；
//  4. 该资产带 GitHub 官方 `digest`（`artifact.Fetch` 强制摘要校验，无摘要
//     根本装不了——把它列进可安装表只会造出"看得见点不动"的行）。
func parseReleasesBody(body []byte) ([]HeuRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []HeuRelease
	for _, r := range releases {
		if r.Draft || !plainNumericTag.MatchString(r.TagName) {
			continue
		}
		chosen, ok := findHostedAsset(r.Assets)
		if !ok {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(chosen.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		page := r.HTMLURL
		if page == "" {
			page = ReleasePagePrefix() + r.TagName
		}
		list = append(list, HeuRelease{
			Version:   r.TagName,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			PageURL:   page,
			AssetName: chosen.Name,
			AssetURL:  chosen.URL,
			Size:      chosen.Size,
			SHA256:    sha,
			Form:      hostedForm,
			Assets:    notesOf(r.Assets, chosen.Name),
		})
	}
	return list, nil
}

var remoteCache = &releaseCache{}

// notesOf 全资产平台/形态矩阵（N13 展示层）；chosen 标"本托管"。
func notesOf(assets []asset, chosen string) []hostfeed.AssetNote {
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		names = append(names, a.Name)
	}
	return hostfeed.Notes(names, chosen)
}
