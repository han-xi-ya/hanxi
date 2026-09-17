package softver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 官方页解析器回归锁：真实片段来自 2026-09-18 实连抓取的 SSR 核心区，
// 锚点（WeChatWin 直链 / 8.0.x 移动端干扰项）一律保持原样。

func readFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "updates_windows_fragment.html"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	return string(b)
}

func TestParseOfficialPageRealFragment(t *testing.T) {
	rel, err := parseOfficialPage(readFixture(t))
	if err != nil {
		t.Fatalf("真实片段解析失败: %v", err)
	}
	if rel.Version != "4.1.15" {
		t.Errorf("Version = %q, 期望 4.1.15", rel.Version)
	}
	wantURL := "https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe"
	if rel.DownloadURL != wantURL {
		t.Errorf("DownloadURL = %q, 期望 %q", rel.DownloadURL, wantURL)
	}
	if rel.PageURL != UpdatesPageURL {
		t.Errorf("PageURL = %q", rel.PageURL)
	}
	if rel.ParseNote != "" {
		t.Errorf("直链口径不应带 ParseNote，got %q", rel.ParseNote)
	}
}

func TestParseOfficialPagePicksHighestAndFiltersMobile(t *testing.T) {
	body := `<a href="https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.14.exe">旧版</a>
	<a href="https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.9.exe">最新</a>
	<a href="https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_8.0.49.exe">移动端干扰项（卡片口径必须过滤）</a>`
	rel, err := parseOfficialPage(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if rel.Version != "4.1.15.9" {
		t.Errorf("Version = %q, 期望 4.1.15.9（取最高且非 8.0.x）", rel.Version)
	}
	if !strings.Contains(rel.DownloadURL, "WeChatWin_4.1.15.9.exe") {
		t.Errorf("DownloadURL 应指向最高非移动版本，got %q", rel.DownloadURL)
	}
}

func TestParseOfficialPageDisplayOnlyFallback(t *testing.T) {
	body := `<p> 发布版本： 微信 4.0.10 for Windows </p>`
	rel, err := parseOfficialPage(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if rel.Version != "4.0.10" || rel.DownloadURL != "" {
		t.Errorf("rel = %+v, 期望仅版本 4.0.10 无直链", rel)
	}
	if rel.ParseNote == "" {
		t.Error("无直链降级必须带说明（不猜不编）")
	}
}

func TestParseOfficialPageMobileOnlyIsUnparsable(t *testing.T) {
	// 全页只剩移动端读数 → Windows 口径视为失配，交给上层降级，不拿 8.0.x 冒充。
	body := `<a href="https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_8.0.49.exe">m</a><p>微信 8.0.49 for Windows 全新发布</p>`
	if _, err := parseOfficialPage(body); !errors.Is(err, errPageUnparsable) {
		t.Fatalf("err = %v, 期望 errPageUnparsable", err)
	}
}

func TestParseOfficialPageGarbage(t *testing.T) {
	for _, body := range []string{"", "<html>404</html>", "WeChatWin_abc.exe"} {
		if _, err := parseOfficialPage(body); err == nil {
			t.Errorf("输入 %q 应解析失败", body)
		}
	}
}

func TestIsMobileSeries(t *testing.T) {
	for v, want := range map[string]bool{"8.0.49": true, "8.0": true, "4.1.15": false, "3.9.12.31": false} {
		if got := isMobileSeries(v); got != want {
			t.Errorf("isMobileSeries(%q) = %v, 期望 %v", v, got, want)
		}
	}
}
