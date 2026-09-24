// remote.go 微软 Sysinternals RAMMap 官方分发侦查件（非 GitHub 家族）。
//
// 上游资产事实（2026-09-23 实证，方案 PLAN_N4_RAMMAP §0）：
//   - 直链唯一：https://download.sysinternals.com/files/RAMMap.zip（HEAD 200，
//     737KB，zip 平铺：RAMMap.exe / RAMMap64.exe / RAMMap64a.exe / Eula.txt，
//     三架构并列非版本目录）；
//   - 无版本历史、同址覆盖更新——Last-Modified 是唯一可用版本信号，远程
//     "列表"恒单条；无第三方镜像生态可回退（单源，见下注记）；
//   - 无官方摘要：完整性走降级三层（bespoke 链，见 manager.go）。
package version

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"

	"hanxi/packages/go/netx"
)

const (
	// zipURL 官方唯一分发直链（Sysinternals 不分版本、不留历史）。
	zipURL = "https://download.sysinternals.com/files/RAMMap.zip"
	// siteURL 官方工具页（前端"访问官网"入口）。
	siteURL = "https://learn.microsoft.com/sysinternals/downloads/rammap"
	// cacheTTL Last-Modified 缓存时长：上游改动以"月"为单位，10 分钟足够。
	cacheTTL = 10 * time.Minute
)

// userAgent 统一产品 UA（构建注入版本自动跟随）。
var userAgent = product.UserAgent()

// SiteURL 官方工具页地址。
func SiteURL() string { return siteURL }

// 下载源事实：Sysinternals 无 GitHub 式镜像生态，第三方"加速站"多为网盘
// 转载（篡改风险高于收益），只走官方直链——降级三层（字节数+CRC+布局）仍在，
// 只是无"多源回退"可言（与 WindTerm 四址链的实质差异，如实记录）。
// dateToken 校验版本令牌形状（YYYY-MM-DD）。
var dateToken = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parseLastModified HTTP 日期（RFC1123）→ "YYYY-MM-DD"；解析失败返回 ""。
func parseLastModified(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	for _, layout := range []string{http.TimeFormat, time.RFC1123, time.RFC1123Z} {
		if t, err := time.Parse(layout, h); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return ""
}

// releaseCache Last-Modified 缓存：HEAD 失败降级旧值（stale-if-error）。
type releaseCache struct {
	mu        sync.Mutex
	data      []RammapRelease
	size      int64
	fetchedAt time.Time
}

// headClient 元数据探测客户端（12s 超时，HEAD 无正文）。
func headClient() *http.Client { return netx.NewClient(12*time.Second, nil) }

// get 返回单条"最新版"列表。size 供降级链字节数核对（Content-Length 实测）。
func (c *releaseCache) get() ([]RammapRelease, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL && len(c.data) > 0 {
		return c.data, c.size, nil
	}

	req, err := http.NewRequest(http.MethodHead, zipURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := headClient().Do(req)
	if err != nil {
		if !c.fetchedAt.IsZero() && len(c.data) > 0 {
			return c.data, c.size, nil // 网络异常降级旧缓存
		}
		return nil, 0, fmt.Errorf("RAMMap 官方源探测失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if !c.fetchedAt.IsZero() && len(c.data) > 0 {
			return c.data, c.size, nil
		}
		return nil, 0, fmt.Errorf("RAMMap 官方源探测: status %d", resp.StatusCode)
	}

	ver := parseLastModified(resp.Header.Get("Last-Modified"))
	if ver == "" {
		if !c.fetchedAt.IsZero() && len(c.data) > 0 {
			return c.data, c.size, nil
		}
		return nil, 0, fmt.Errorf("RAMMap 官方源未给出可解析的 Last-Modified")
	}
	size := resp.ContentLength
	if size <= 0 {
		size = c.size // 无 Content-Length 罕见场：沿用旧探测值
	}
	list := []RammapRelease{{
		Version:   ver,
		Published: resp.Header.Get("Last-Modified"),
		AssetName: "RAMMap.zip",
		AssetURL:  zipURL,
		Size:      size,
	}}
	c.data = list
	c.size = size
	c.fetchedAt = time.Now()
	return list, size, nil
}

// digest 恒空（上游无官方摘要）——签名与 GitHub 家族对齐，供 manager 统一判定。
func (c *releaseCache) digest(string) string { return "" }

var remoteCache = &releaseCache{}
