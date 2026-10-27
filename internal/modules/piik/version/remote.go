package version

import (
	"encoding/json"
	"errors"
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
	repoOwner = "TNTcraftHIM"
	repoName  = "Piik"

	// perPageFirst release 列表首页页数（原 100）：列表只用于挑最近版本，取
	// 若干条即可，与 frpc/douzy/markeron 等家族模块同为 60 档。信息损失如实记：
	// piik 上游 release 总数 2026-10-24 经 API Link 头实测为 39 条，39 < 60，
	// 降到 60 无任何信息损失（连"舍弃最早若干条"都不存在）。总数随上游日更风暴
	// 继续增长，本注释只对本日实测负责。
	perPageFirst = 60
	// perPageSmall 仍超限时的降级页（dbx 同型取首页约 1/3）。
	perPageSmall = 20

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

// apiClient GitHub API 请求客户端（12s 超时）。传输层挂 netx 混合闸
// LoopbackAwareProxyFunc：外网 GitHub 镜像照旧跟随系统代理（踩坑 #35 的
// 收口），回环恒定直连——单测的 httptest 靶（127.0.0.1）若被本机 7890 型
// 代理接管即成 502（netx 纪律与 artifact.Fetch 下载内核同规）。
func apiClient() *http.Client {
	tr := netx.Transport()
	tr.Proxy = netx.LoopbackAwareProxyFunc()
	return &http.Client{Timeout: 12 * time.Second, Transport: tr}
}

// maxFeedBytes 响应体读取上限。
//
// 原来这里是"LimitReader 读满 8 MiB 就当正常结果返回"，而 io.LimitReader 被切断时
// 不报任何错——半截 JSON 与完整响应同形，于是把"响应太大"伪装成"上游格式变了"
// （同族 dbx 已因此炸过，实测 13.3 MB）。piik 当前响应仅 0.66 MB（per_page=100
// 全量 39 条也小），距 8 MiB 余量 12× 以上，本不触发；但截断识别必须补上：上游
// 日更风暴节奏下 changelog 变长随时可能发生，届时报出来的才该是真话而非误导。
// 声明成 var 而非 const 是为了留一个测试接缝——把上限压低才能在单测里覆盖
// "超限 → 降页重试"整条路径（仓内 dbx 同型做法）。生产不改它。
var maxFeedBytes = 8 << 20

// errResponseTooLarge 响应体超过读取上限。单独一个哨兵，是为了把它与"网络失败"
// "非 200"分开处理——只有前者值得换更小的页重试。
var errResponseTooLarge = errors.New("响应体超过上限")

// fetchJSON 取 release 列表。两层重试各管一件事，不混在一起：
//   - 镜像回退（fetchReleasesPage 内部）：某个源连不上或返回非 200，换下一个源；
//   - 页面降级（本函数）：所有源都报"响应过大"，说明不是源的问题而是这一页本身
//     太大，换小页再来一轮。
//
// 若所有源是因别的原因失败，直接上报、不降级重试：拿小页去重试网络故障只会把
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
			// 截断的响应绝不往下交给解析层：交下去只会得到一句
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
// 多读那 1 字节是重点：io.LimitReader 被切断时不报任何错，半截响应和完整响应
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
