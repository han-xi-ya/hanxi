package softver

// 官方版本通道（BACKLOG F5 裁定，2026-09-17 实连验证）：
// 官方更新页 https://weixin.qq.com/updates?platform=windows 为 SSR，页面内嵌
// 最新版发布块与全平台链接清单。实测锚点：正则 WeChatWin_([0-9.]+)\.exe 一发
// 命中"版本号 + 下载直链"双结果（验证日 4.1.15，直链
// https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe）。
// 口径护栏：页内 8.0.x 系列属移动端（Android apk 文案），绝不混入 Windows 口径。
// 这是 HTML 抓取而非结构化 API——页面改版即失配，解析失败必须如实报错，
// 由调用方降级为"打开官方页 + 显示本机版本"，不猜不编。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioncmp"
)

// UpdatesPageURL 官方 Windows 更新页（解析锚点与降级外呼共用同一常量）。
const UpdatesPageURL = "https://weixin.qq.com/updates?platform=windows"

const (
	officialFetchTimeout = 15 * time.Second
	officialBodyLimit    = 2 << 20 // 页面实测 ~150KB，2MB 封顶防御
	// browserUA 官方页对无 UA 请求的行为未验证，统一带桌面 UA（与 curl 实连验证口径一致）。
	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

var (
	// wechatWinLinkRe 主口径：universal Windows 直链文件名内嵌版本号（引号/尖括号/
	// 反斜杠一律不进字符类，兼容裸 HTML 与 JSON 串两种嵌入形态）。
	wechatWinLinkRe = regexp.MustCompile(`https?://[^"'\s\\<>()]+/WeChatWin_([0-9][0-9]*(?:\.[0-9]+)*)\.exe`)
	// displayRe 备援口径：发布块文案"微信 4.1.15 for Windows"（只出版本号无直链）。
	displayRe = regexp.MustCompile(`微信\s*([0-9][0-9]*(?:\.[0-9]+)*)\s*for Windows`)
)

// errPageUnparsable 官方页无法解析（改版失配）的哨兵语义错误。
var errPageUnparsable = errors.New("官方更新页未能解析出微信 Windows 版本（页面可能已改版）")

// isMobileSeries 8.0.x 为移动端系列（卡片明令过滤，勿混 Windows 口径）。
func isMobileSeries(version string) bool {
	return strings.HasPrefix(version, "8.0")
}

// parseOfficialPage 从更新页 HTML 提取官方最新版（纯函数，真实片段有 fixture 回归锁）。
// 优先直链口径（版本+URL 双结果）；直链全失配时退读发布块文案（仅版本）；
// 两者皆空返回 errPageUnparsable。多版本命中时按 versioncmp 取最高。
func parseOfficialPage(body string) (OfficialRelease, error) {
	best := ""
	bestURL := ""
	for _, m := range wechatWinLinkRe.FindAllStringSubmatch(body, -1) {
		ver := m[1]
		if isMobileSeries(ver) {
			continue
		}
		if best == "" || versioncmp.Compare(ver, best) > 0 {
			best, bestURL = ver, m[0]
		}
	}
	if best != "" {
		return OfficialRelease{Version: best, DownloadURL: bestURL, PageURL: UpdatesPageURL}, nil
	}
	if m := displayRe.FindStringSubmatch(body); m != nil && !isMobileSeries(m[1]) {
		return OfficialRelease{
			Version:   m[1],
			PageURL:   UpdatesPageURL,
			ParseNote: "页面结构可能已调整：只解析出发布文案版本号，未取到下载直链",
		}, nil
	}
	return OfficialRelease{}, errPageUnparsable
}

// fetchUpdatesPage 抓取官方更新页正文（注入 service，单测替换后不碰网络）。
func fetchUpdatesPage(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UpdatesPageURL, nil)
	if err != nil {
		return "", fmt.Errorf("构造官方页请求失败: %w", err)
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	client := &http.Client{Timeout: officialFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("访问官方更新页失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("官方更新页返回异常状态: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, officialBodyLimit))
	if err != nil {
		return "", fmt.Errorf("读取官方更新页失败: %w", err)
	}
	return string(body), nil
}
