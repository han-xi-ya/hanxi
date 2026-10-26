package papertodo

import (
	"testing"

	"hanxi/internal/modules/papertodo/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"9.9.9", "v9.9.9"},
		{"v9.9.9", "v9.9.9"},
		{"V9.9.9", "v9.9.9"},
		{"  v9.9.9 ", "v9.9.9"},
		{" V9.9.9\t", "v9.9.9"},
		{"imported-0123456789ab", "vimported-0123456789ab"},
		{"vimported-0123456789ab", "vimported-0123456789ab"},
		{"Vimported-0123456789ab", "vimported-0123456789ab"},
		{"", "v"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装（v9.9.9 + 自包含变体）
// 时裸号/v号/V号/带空白四种入参都回 already-installed，且每次调用恰过门一次
// （countGate 同时计 Enter 与后台 EnterBackground——恰 1 即后台下载未启动；
// 修复路径在 go 语句之前同步返回，无赛跑。若归一再退回只剥小写 v，返回值
// 先红，行序保证不会假绿）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	f := newSvcFixture(t)
	f.simInstall(t, "v9.9.9", version.VariantSelfContained)

	for _, in := range []string{"9.9.9", "v9.9.9", "V9.9.9", " 9.9.9 "} {
		before := f.gate.acquired.Load()
		got, err := f.svc.DownloadVersion(in, "")
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed", in, got)
		}
		if d := f.gate.acquired.Load() - before; d != 1 {
			t.Fatalf("DownloadVersion(%q) 应只过一次 RPC 门（后台未入场），门计数增量 %d", in, d)
		}
	}
}
