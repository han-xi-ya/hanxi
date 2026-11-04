package omp

import (
	"path/filepath"
	"testing"
)

// TestNormalizeVersion 钉死 douzy 事故同款纪律：裸号/v号/V号/带空白同落一个
// v 前缀规范形；imported- 兜底令牌恒等透传为 vimported-（绝不产出 vvimported-）。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"18.16.0", "v18.16.0"},
		{"v18.16.0", "v18.16.0"},
		{"V18.16.0", "v18.16.0"},
		{"  v18.16.0 ", "v18.16.0"},
		{"imported-0123456789ab", "vimported-0123456789ab"},
		{"vimported-0123456789ab", "vimported-0123456789ab"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q)=%q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestActiveVersionFromPath GetStatus 回退通道的令牌反推：只认 omp_<token> 目录族。
func TestActiveVersionFromPath(t *testing.T) {
	root := filepath.Join("E:", "hanxidata", "versions")
	if got := activeVersionFromPath(filepath.Join(root, "omp_18.16.0", "omp-windows-x64.exe")); got != "v18.16.0" {
		t.Errorf("常规令牌反推失败：%q", got)
	}
	if got := activeVersionFromPath(filepath.Join(root, "omp_imported-0123456789ab", "omp-windows-x64.exe")); got != "vimported-0123456789ab" {
		t.Errorf("imported 令牌反推失败：%q", got)
	}
	if got := activeVersionFromPath(filepath.Join(root, "ccswitch_3.16.1", "cc-switch.exe")); got != "" {
		t.Errorf("外来目录族必须反推为空，实际 %q", got)
	}
}

// TestStoreRoundTrip 激活 store 的落盘-重载循环（jsonstore 原子写口径）。
func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newOmpStore(dir)
	if err := s.SetActive("v18.16.0"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	reloaded := newOmpStore(dir)
	if reloaded.GetActive() != "v18.16.0" {
		t.Errorf("重载后激活版本丢失：%q", reloaded.GetActive())
	}
	if err := reloaded.SetActive(""); err != nil {
		t.Fatalf("清空激活: %v", err)
	}
	if again := newOmpStore(dir); again.GetActive() != "" {
		t.Errorf("清空未持久化：%q", again.GetActive())
	}
}
