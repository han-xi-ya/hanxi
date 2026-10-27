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

	"hanxi/packages/go/netx"
)

// userAgent 统一产品 UA：派生自 internal/product 产品身份，构建脚本经 -X
// 注入 Version 后自动跟随真实发布版本，不再手写版本串。
var userAgent = product.UserAgent()

const (
	repoOwner    = "M2Team"
	repoName     = "NanaZip"
	cacheTTL     = 10 * time.Minute
	digestPrefix = "sha256:"
)

var (
	stableVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
	sha256Re        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	apiBaseURLs     = []string{
		"https://api.github.com",
		"https://ghfast.top/https://api.github.com",
		"https://gh-proxy.com/https://api.github.com",
		"https://ghproxy.net/https://api.github.com",
	}
)

// RepoURL 上游仓库主页（apiBaseURLs 为 GitHub API 镜像轮询序列，直连失败自动降级）。
func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type releaseCache struct {
	mu        sync.Mutex
	data      []Release
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return append([]Release(nil), c.data...), nil
	}
	body, err := fetchReleases()
	if err != nil {
		if !c.fetchedAt.IsZero() {
			stale := append([]Release(nil), c.data...)
			for i := range stale {
				stale[i].Stale = true
			}
			return stale, nil
		}
		return nil, err
	}
	list, err := parseReleasesBody(body)
	if err != nil {
		return nil, err
	}
	c.data, c.fetchedAt = list, time.Now()
	return append([]Release(nil), list...), nil
}

// maxFeedBytes releases 列表响应体读取上限。
//
// 与同模块 bundle.go 的 readZipEntry 是同一套"limit+1 截断识别"纪律（机主
// 裁决：两处刻意各自保留一份、不抽公共件）——**超限的响应绝不交给解析层**：
// 半截 JSON 只会得到 "unexpected end of JSON input"，把"响应太大/被劫持"
// 伪装成"上游格式变了"（dbx 刚踩过并入库 ed53f90，就是这个坑）。
//
// 取值 8 MiB 的依据：NanaZip releases?per_page=60 实测仅 0.61 MB（约 13.7 倍
// 余量），这是**防异常而非防正常**的上限——正常响应永远撞不到，撞到的只可能是
// 代理劫持塞大 payload。没有数据支持动 per_page，本闸只做截断识别。
//
// 声明成 var 而非 const 是留测试接缝（dbx maxFeedBytes、keyops.writeNewFile
// 同型做法）：压低上限才能在单测里覆盖"超限 → 继续下一镜像"路径。生产不改它。
var maxFeedBytes = 8 << 20

// errResponseTooLarge 响应体超过读取上限的哨兵，把"超限"与"连不上/非 200"分开。
var errResponseTooLarge = errors.New("响应体超过上限")

// readCapped 最多读 limit+1 字节，返回 (内容, 是否超限, 读错误)。与同模块
// bundle.go 的 readZipEntry 同型（多读 1 字节专用于分辨"正好读完"与"被切断"）。
// io.LimitReader 被切断时不报错，半截响应与完整响应长得一模一样——"读到上限"
// 只能靠"还能再读到字节"判定，不能指望读操作自己失败。
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

func fetchReleases() ([]byte, error) {
	client := netx.NewClient(15*time.Second, nil)
	var lastErr error
	for _, base := range apiBaseURLs {
		url := base + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
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
			// 截断响应**不往下交给解析层**：交下去只会得到 "unexpected end of JSON input"。
			lastErr = fmt.Errorf("%w: %s 的响应超过 %d MiB", errResponseTooLarge, url, maxFeedBytes>>20)
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("all GitHub API mirrors failed: %w", lastErr)
}

func parseReleasesBody(body []byte) ([]Release, error) {
	var raw []release
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}
	list := make([]Release, 0, len(raw))
	for _, item := range raw {
		if item.Draft || item.Prerelease || !stableVersionRe.MatchString(item.TagName) {
			continue
		}
		wanted := "NanaZip_" + item.TagName + ".msixbundle"
		var found *asset
		for i := range item.Assets {
			if item.Assets[i].Name == wanted {
				found = &item.Assets[i]
				break
			}
		}
		if found == nil || found.Size <= 0 {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(found.Digest), digestPrefix)
		if !sha256Re.MatchString(sha) {
			continue
		}
		list = append(list, Release{Version: item.TagName, Published: item.PublishedAt, AssetName: found.Name, AssetURL: found.URL, Size: found.Size, SHA256: sha})
	}
	sort.Slice(list, func(i, j int) bool { return versioncmp.Compare(list[i].Version, list[j].Version) > 0 })
	return list, nil
}

var remoteCache = &releaseCache{}
