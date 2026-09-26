package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
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
	// 上游发布接口（果核自建，非 GitHub）：GET 返回当前版本 JSON；
	// ?channel=beta 查测试通道。实测接口无鉴权、无历史列表。
	productBase = "https://rj.lovestu.com/download/gh_view"
	siteURL     = "https://pic.ghxi.com" // 官网（前端"访问官网"入口）

	// cacheTTL 远程版本内存缓存时长：接口无分页无历史，10 分钟足以兜住
	// 页面反复进入的重复请求，又不至于错过太久的新版本发布
	cacheTTL = 10 * time.Minute
)

// SiteURL 官网地址（前端展示/一键浏览器打开；上游未开源，没有仓库页）。
func SiteURL() string { return siteURL }

// fourSegVersion 归一化四段版本号（如 3.2.7.98）；导入版本探测与目录名校验共用
var fourSegVersion = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)

// 接口 JSON 形态（实测 3.2.7 样例裁剪）：
//
//	{"code":0,"data":{"channel":"stable","version":"3.2.7","version_code":98,
//	  "files":[{"name":"GuoheView_v3.2.7.98-便携版.zip","url":"…","size":6884498,"md5":"…"}, …]}}
type apiEnvelope struct {
	Code int `json:"code"`
	Data struct {
		Channel     string    `json:"channel"`
		Version     string    `json:"version"`
		VersionCode int       `json:"version_code"`
		Files       []apiFile `json:"files"`
	} `json:"data"`
}

// apiFile 官方接口 files 数组的一条发布物（资产名 + 直链 + 官方摘要三元组）。
type apiFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
	MD5  string `json:"md5"`
}

func apiClient() *http.Client {
	return netx.NewClient(12*time.Second, nil)
}

// fetchChannel 拉取指定通道（stable/beta）的当前版本并转换为 ViewRelease。
// code 字段实测 stable 返 0、public-files 类接口返 200，不可靠——以
// HTTP 200 + data.files 非空为准。beta 通道可能 404/无资产（当前无测试版），
// 返回 error 由调用方容忍降级。
func fetchChannel(channel string) (ViewRelease, error) {
	url := productBase
	if channel == "beta" {
		url += "?channel=beta"
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ViewRelease{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := apiClient().Do(req)
	if err != nil {
		return ViewRelease{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ViewRelease{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ViewRelease{}, fmt.Errorf("发布接口 %s: status %d", url, resp.StatusCode)
	}
	return parseChannelBody(body)
}

// parseChannelBody 解析接口响应（单测注入真实样例复用）。
// 过滤规则：无便携 zip 资产 / 官方 md5 缺失 / 版本形状异常一律拒收——
// 完整性第一层与目录名安全不能缺位。
func parseChannelBody(body []byte) (ViewRelease, error) {
	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return ViewRelease{}, fmt.Errorf("parse release api: %w", err)
	}
	d := env.Data
	if d.Version == "" || len(d.Files) == 0 {
		return ViewRelease{}, fmt.Errorf("发布接口响应缺少版本或资产数据")
	}
	build := d.Version + "." + strconv.Itoa(d.VersionCode)
	if !fourSegVersion.MatchString(build) {
		return ViewRelease{}, fmt.Errorf("非法版本号形状: %q", build)
	}
	arch, ok := findPortableZip(d.Files)
	if !ok {
		return ViewRelease{}, fmt.Errorf("版本 %s 无便携 zip 资产", build)
	}
	md5 := strings.ToLower(strings.TrimSpace(arch.MD5))
	if len(md5) != 32 {
		return ViewRelease{}, fmt.Errorf("便携资产缺少官方 MD5: %s", arch.Name)
	}
	channel := d.Channel
	if channel == "" {
		channel = "stable"
	}
	return ViewRelease{
		Version:   "v" + build,
		Channel:   channel,
		IsPre:     channel == "beta",
		AssetName: arch.Name,
		AssetURL:  arch.URL,
		Size:      arch.Size,
		MD5:       md5,
		Assets:    notesOf(d.Files, arch.Name),
	}, nil
}

// notesOf files 数组逐条投影平台/形态矩阵（N13 展示层）。判据一处偏离共享
// hostfeed.Classify，为本产品实证：果核看图仅存世于 Windows（接口从未暴露
// 非 Windows 发布物），平台按产品级事实直判，不靠文件名推断——资产名走中文
// 「便携版/安装包」字面量约定，Classify 按 zip/7z 分支会把便携族降级
// other/archive。形态词表：「便携」/portable → 便携，「安装包」/setup →
// 安装器，其余交 Classify 兜底（宁降级不猜标）。
func notesOf(files []apiFile, chosen string) []hostfeed.AssetNote {
	out := make([]hostfeed.AssetNote, 0, len(files))
	for _, f := range files {
		if hostfeed.IsMetadata(f.Name) {
			continue
		}
		lower := strings.ToLower(f.Name)
		var form hostfeed.Form
		switch {
		case strings.Contains(lower, "便携") || strings.Contains(lower, "portable"):
			form = hostfeed.FormPortable
		case strings.Contains(lower, "安装包"):
			form = hostfeed.FormInstaller
		default:
			_, form = hostfeed.Classify(f.Name)
		}
		out = append(out, hostfeed.AssetNote{
			Platform: hostfeed.PlatformWindows, Form: form, Label: f.Name,
			Managed: strings.EqualFold(f.Name, chosen),
		})
	}
	return out
}

// findPortableZip 从资产数组挑便携 zip。
// 资产名带中文字面量「便携版」（实测上游命名），大小写与括号变体容错；
// 排除安装包 exe / 7z（解压需外部依赖）/ 测试版名混入 stable 的防御交给通道字段。
func findPortableZip(files []apiFile) (apiFile, bool) {
	for _, f := range files {
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, ".zip") && strings.Contains(f.Name, "便携版") {
			return f, true
		}
	}
	// 上游若未来本地化名漂移（英文 Portable 等），按 zip 后缀 + 非安装包兜底
	for _, f := range files {
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, ".zip") && !strings.Contains(lower, "setup") && !strings.Contains(lower, "安装包") {
			return f, true
		}
	}
	return apiFile{}, false
}

// releaseCache 双通道版本缓存。接口无历史列表，缓存至多 stable+beta 两条；
// beta 构建号不高于 stable 时剔除（实测 beta 通道会滞后于已转正的正式版，
// 列出只会误导用户"降级"）。
type releaseCache struct {
	mu        sync.Mutex
	data      []ViewRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]ViewRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	var list []ViewRelease
	stable, sErr := fetchChannel("stable")
	if sErr != nil {
		if !c.fetchedAt.IsZero() {
			return c.data, nil // stale-if-error：网络异常降级旧缓存
		}
		return nil, sErr
	}
	list = append(list, stable)
	if beta, bErr := fetchChannel("beta"); bErr == nil && buildOf(beta) > buildOf(stable) {
		list = append(list, beta)
	}

	c.data = list
	c.fetchedAt = time.Now()
	return list, nil
}

// buildOf 提取四段版本的构建号（末段）用于 beta/stable 新旧比较；形状异常按 0。
func buildOf(r ViewRelease) int {
	i := strings.LastIndex(r.Version, ".")
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(r.Version[i+1:])
	if err != nil {
		return 0
	}
	return n
}

var remoteCache = &releaseCache{}
