package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"

	"hanxi/packages/go/netx"
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

const (
	// apiRoot VS Code 官方更新/下载网关（update.code.visualstudio.com）。
	// 上游二进制只经此域名与微软 CDN 分发，GitHub releases 无二进制资产，
	// 因此本模块无 GitHub API 镜像回退的必要（everything 官网下载同族先例）。
	apiRoot = "https://update.code.visualstudio.com"

	quality = "stable"

	// 平台标识（官方更新清单 API 的 {platform} 段，实测 200）：
	// win32-x64-archive 便携 zip；win32-x64-user 用户安装器（免 UAC）。
	platformPortable  = "win32-x64-archive"
	platformInstaller = "win32-x64-user"

	// remoteVersionWindow 远程列表展示窗口：版本列表全量返回（约 60 项），
	// 但每个版本的直链/大小/commit 需逐个 HEAD 解析，只解析最新 12 个
	// （月更节奏约覆盖一个季度），更早版本仍可按号下载（Download 现场解析）。
	remoteVersionWindow = 12

	// cacheTTL 远程列表内存缓存时长（HEAD 风暴防护，与 ccswitch 同策略）
	cacheTTL = 10 * time.Minute
)

// testAPIRoot 官方端点前缀的测试替身钩子（生产恒为空 = 走 apiRoot 常量）。
var testAPIRoot string

func effectiveAPIRoot() string {
	if testAPIRoot != "" {
		return testAPIRoot
	}
	return apiRoot
}

// RepoURL 官网主页（前端展示/复制/一键浏览器打开）。
func RepoURL() string { return "https://code.visualstudio.com" }

// plainSemver VS Code 纯语义版本 tag（无 v 前缀，如 1.136.1）。
var plainSemver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// versionListURL 官方版本列表端点（返回 ["1.136.1", ...] 新→旧）。
func versionListURL() string { return effectiveAPIRoot() + "/api/releases/" + quality }

// versionedDownloadURL 指定版本+形态的下载端点（302 到 CDN，Location 含 commit 与资产名）。
func versionedDownloadURL(form Form, version string) string {
	return fmt.Sprintf("%s/%s/%s/%s", effectiveAPIRoot(), version, platformOf(form), quality)
}

// feedURL 官方更新清单端点：传入任意旧 commit 返回最新版 JSON（含官方 sha256hash）。
func feedURL(form Form, currentCommit string) string {
	return fmt.Sprintf("%s/api/update/%s/%s/%s", effectiveAPIRoot(), platformOf(form), quality, currentCommit)
}

func platformOf(form Form) string {
	if form == FormInstaller {
		return platformInstaller
	}
	return platformPortable
}

// manifest 官方更新清单响应（仅取托管所需字段；实测样例见 version_test.go）。
type manifest struct {
	URL            string `json:"url"`
	Version        string `json:"version"`        // commit（40 位 hex）
	ProductVersion string `json:"productVersion"` // 如 1.136.1
	SHA256         string `json:"sha256hash"`     // 官方 sha256（64 位 hex）
}

// downloadClient HEAD/GET 官方端点客户端（12s 超时，与 apiClient 同档）。
func downloadClient() *http.Client { return netx.NewClient(12*time.Second, nil) }

// fetchJSON 拉取 JSON 响应体（错误带 URL 上下文）。
func fetchJSON(client *http.Client, url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// resolveRelease HEAD 版本化下载端点（跟随 302）解析出最终 CDN 直链、
// 资产名、commit 与字节数——全部信息在重定向链上即可拿齐，单次请求。
func resolveRelease(client *http.Client, form Form, version string) (Release, error) {
	req, err := http.NewRequest(http.MethodHead, versionedDownloadURL(form, version), nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("HEAD %s: status %d", req.URL.String(), resp.StatusCode)
	}
	final := resp.Request.URL.String()
	// 最终 URL 形如 .../download/stable/{commit}/{assetName}
	trimmed := strings.TrimSuffix(final, "/")
	i := strings.LastIndex(trimmed, "/")
	if i <= 0 {
		return Release{}, fmt.Errorf("下载直链无法解析资产名: %s", final)
	}
	assetName := trimmed[i+1:]
	j := strings.LastIndex(trimmed[:i], "/")
	commit := ""
	if j > 0 {
		commit = trimmed[j+1 : i]
	}
	rel := Release{
		Version:     version,
		Commit:      commit,
		DownloadURL: final,
		AssetName:   assetName,
		Size:        resp.ContentLength,
	}
	return rel, nil
}

// releaseCache 单形态远程列表缓存（列表 → 逐个 HEAD → 最新版官方哈希）。
type releaseCache struct {
	form      Form
	mu        sync.Mutex
	data      []Release
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	list, err := fetchRemoteList(downloadClient(), c.form)
	if err != nil {
		if !c.fetchedAt.IsZero() {
			return c.data, nil // 网络异常降级返回旧缓存
		}
		return nil, err
	}
	c.data = list
	c.fetchedAt = time.Now()
	return list, nil
}

// fetchRemoteList 组装远程版本列表：
//  1. /api/releases/stable 全量版本（新→旧）；
//  2. 截窗口逐个 HEAD 解析直链/大小/commit（并发受限，单项失败仅剔除该项）;
//  3. 用列表中第二个版本的 commit 调 feed API 拿最新版官方 sha256，
//     命中首项即补 SHA256 字段（官方哈希仅最新版可查——上游接口形态如此）。
//
// client 参数化便于测试注入 httptest。
func fetchRemoteList(client *http.Client, form Form) ([]Release, error) {
	var versions []string
	if err := fetchJSON(client, versionListURL(), &versions); err != nil {
		return nil, fmt.Errorf("获取版本列表失败: %w", err)
	}
	versions = filterSemver(versions, remoteVersionWindow)
	if len(versions) == 0 {
		return nil, fmt.Errorf("版本列表无有效 semver 条目")
	}

	// 并发 HEAD 解析（上限 4，防官方端点限频）
	type slot struct {
		rel Release
		ok  bool
	}
	slots := make([]slot, len(versions))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, v := range versions {
		wg.Add(1)
		go func(i int, v string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rel, err := resolveRelease(client, form, v)
			if err == nil {
				slots[i] = slot{rel, true}
			}
		}(i, v)
	}
	wg.Wait()

	var list []Release
	for _, s := range slots {
		if s.ok {
			list = append(list, s.rel)
		}
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("所有版本直链解析均失败")
	}

	// 最新版官方 sha256：feed 需任意"旧 commit"入参，取列表第二项的 commit；
	// 列表仅一项（理论不可达，上游保留 60 版）或旧版解析失败则放弃哈希（降级三层）。
	if len(list) >= 2 {
		var mf manifest
		if err := fetchJSON(client, feedURL(form, list[1].Commit), &mf); err == nil {
			if strings.EqualFold(mf.ProductVersion, list[0].Version) && isHex64(mf.SHA256) {
				list[0].SHA256 = strings.ToLower(mf.SHA256)
			}
		}
	}
	return list, nil
}

// filterSemver 过滤非纯 semver 项并截窗（保持新→旧原序）。
func filterSemver(versions []string, window int) []string {
	var out []string
	for _, v := range versions {
		if !plainSemver.MatchString(v) {
			continue
		}
		out = append(out, v)
		if len(out) >= window {
			break
		}
	}
	return out
}

// isHex64 64 位小写/大写 hex 校验。
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

var (
	portableCache  = &releaseCache{form: FormPortable}
	installerCache = &releaseCache{form: FormInstaller}
)

// resetRemoteCaches 清空两形态远程缓存（测试钩子：httptest 服务器逐用例更换，
// 旧缓存指向已关闭的服务器）。
func resetRemoteCaches() {
	portableCache = &releaseCache{form: FormPortable}
	installerCache = &releaseCache{form: FormInstaller}
}

// ListRemote 获取指定形态的远程可用版本（10 分钟缓存）。
func ListRemote(form Form) ([]Release, error) {
	if form == FormInstaller {
		return installerCache.get()
	}
	return portableCache.get()
}

// lookupRemote 在缓存列表中查找指定版本（不触发网络）。
func lookupRemote(list []Release, version string) (Release, bool) {
	for _, r := range list {
		if r.Version == version {
			return r, true
		}
	}
	return Release{}, false
}
