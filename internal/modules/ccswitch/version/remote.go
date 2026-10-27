package version

import (
	"encoding/json"
	"errors"
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

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

const (
	repoOwner = "farion1231"
	repoName  = "cc-switch"

	// perPageFirst 60 条实测响应约 3.4 MB（每个 release 带完整 changelog 正文），
	// 距 8 MiB 上限余量仅 2.4×——一次上游长公告即可越限被静默截断（见 maxFeedBytes）。
	// 列表只用于挑版本，取最近若干条即可：40 条约 2.3 MB，余量回到 3× 以上。
	perPageFirst = 40
	// perPageSmall 仍超限时的降级页（约 0.6 MB）。
	perPageSmall = 10

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute

	// digestPrefix GitHub API 资产 digest 字段格式为 "sha256:<hex>"，解析时剥离
	digestPrefix = "sha256:"
)

// maxFeedBytes 响应体读取上限。
//
// 原来这里是"LimitReader 读满 8 MiB 就当正常结果返回"，而 io.LimitReader 被切断时
// 不报任何错——半截 JSON 与完整响应长得一样，解析层于是报
// `unexpected end of JSON input`：一件"响应太大"的确定事实，被伪装成"上游格式变了"
// （dbx 模块 13.3 MB 真实响应就是这么炸的；本仓库当前 3.4 MB 只是还没到量）。
// 现在多读 1 字节专用于分辨"正好读完"与"被切断"（见 readCapped），超限就明说并降页重试。
//
// 声明成 var 而非 const 是为了留一个测试接缝：把上限压低才能在单测里覆盖
// "超限 → 降页重试"整条路径（仓内 dbx / keyops.writeNewFile 是同型做法）。生产不改它。
var maxFeedBytes = 8 << 20

// RepoURL 上游仓库地址（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退（与 markeron 同组）
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
	return netx.NewClient(12*time.Second, nil)
}

// errResponseTooLarge 响应体超过读取上限。单独一个哨兵，是为了把它与"网络失败"
// "非 200"分开处理——只有前者值得换更小的页重试。
var errResponseTooLarge = errors.New("响应体超过上限")

// fetchJSON 取 release 列表。两层重试各管一件事，不混在一起：
//   - **镜像回退**（fetchReleasesPage 内部）：某个源连不上或返回非 200，换下一个源；
//   - **页面降级**（本函数）：所有源都报"响应过大"，说明不是源的问题而是这一页本身
//     太大，换小页再来一轮。
//
// 若所有源是因别的原因失败，直接上报、**不降级重试**：拿小页去重试网络故障只会把
// 真实原因（限流、断网、UA 被封）掩盖成"试了两轮都不行"，那句错误对排查毫无帮助。
func fetchJSON(urls []string) ([]byte, error) {
	var tooLarge error
	for _, perPage := range []int{perPageFirst, perPageSmall} {
		body, err := fetchReleasesPage(urls, perPage)
		switch {
		case err == nil:
			return body, nil
		case errors.Is(err, errResponseTooLarge):
			tooLarge = err
		default:
			return nil, err
		}
	}
	return nil, fmt.Errorf("release 列表响应体始终超过 %d MiB 上限（per_page 已从 %d 降到 %d）：%w",
		maxFeedBytes>>20, perPageFirst, perPageSmall, tooLarge)
}

// fetchReleasesPage 在候选镜像上依次尝试同一 per_page 的 release 列表。
func fetchReleasesPage(urls []string, perPage int) ([]byte, error) {
	var lastErr error
	for _, base := range urls {
		url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", base, repoOwner, repoName, perPage)
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
		body, over, readErr := readCapped(resp.Body, maxFeedBytes)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("GitHub API %s: status %d", url, resp.StatusCode)
			continue
		}
		if over {
			// 截断的响应**绝不往下交给解析层**：交下去只会得到一句
			// "unexpected end of JSON input"，把排查整个带偏（dbx 的故障就是这么来的）。
			lastErr = fmt.Errorf("%w: %s 的响应超过 %d MiB", errResponseTooLarge, url, maxFeedBytes>>20)
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("all GitHub API mirrors failed: %w", lastErr)
}

// readCapped 最多读 limit+1 字节，返回 (内容, 是否超限, 读错误)。
//
// **多读那 1 字节是重点**：io.LimitReader 被切断时不报任何错，半截响应和完整响应
// 在它手里长得一模一样——所以"读到上限"必须靠"还能再读到字节"来判定，而不是指望
// 读操作自己失败。
func readCapped(r io.Reader, limit int) ([]byte, bool, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > limit {
		return b[:limit], true, nil
	}
	return b, false, nil
}

// findPortableAsset 从 release 资产中筛选 Windows x64 便携 zip（排除 arm64 变体）。
// 资产名形如 CC-Switch-v3.20.0-Windows-Portable.zip，arm64 为
// CC-Switch-v3.20.0-Windows-arm64-Portable.zip——后缀兜底
// "-windows-portable.zip" 天然排除 arm64/msi/sig/安装包。
func findPortableAsset(assets []asset, version string) (asset, bool) {
	ver := strings.TrimPrefix(version, "v")
	needName := strings.ToLower("cc-switch-v" + ver + "-windows-portable.zip")
	for _, a := range assets {
		if lower := strings.ToLower(a.Name); lower == needName {
			return a, true
		}
	}
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, "-windows-portable.zip") && strings.Contains(lower, strings.ToLower(ver)) {
			return a, true
		}
	}
	return asset{}, false
}

// releaseCache 远程列表缓存（防 GitHub 限流）。
type releaseCache struct {
	mu        sync.Mutex
	data      []CCRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]CCRelease, error) {
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
// 过滤规则：draft / 非纯语义 tag / 无 x64 便携资产 / 官方 sha256 缺失的 release 一律不入列表。
func parseReleasesBody(body []byte) ([]CCRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []CCRelease
	for _, r := range releases {
		// 未认证 API 本就不返回 draft，防御性再跳一次；tag 非纯 x.y.z 直接丢弃
		if r.Draft || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		arch, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		// 官方 sha256 缺失的 release 不入列表：完整性校验第一层不能缺位
		// （GitHub 自 2024 年起所有新资产均带 digest，本仓库实测全量覆盖）
		sha := strings.TrimPrefix(strings.ToLower(arch.Digest), digestPrefix)
		if len(sha) != 64 {
			continue
		}
		list = append(list, CCRelease{
			Version:   r.TagName,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
			SHA256:    sha,
			Assets:    notesOf(r.Assets, arch.Name),
		})
	}
	return list, nil
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
