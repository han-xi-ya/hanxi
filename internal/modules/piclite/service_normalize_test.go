package piclite

import (
	"context"
	"sync/atomic"
	"testing"

	"hanxi/internal/modules/piclite/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.4.1", "v1.4.1"},
		{"v1.4.1", "v1.4.1"},
		{"V1.4.1", "v1.4.1"},
		{"  v1.4.1 ", "v1.4.1"},
		{" V1.4.1\t", "v1.4.1"},
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

// TestDownloadVersionInstalledDedupCaseInsensitive 已装 1.4.1 时四种同版本
// 入参（含大写 V 形态）都回 already-installed，下载接缝被调次数=0，在途名册
// 零登记（登记先于 goroutine，判据确定性成立）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	f := newPicFixture(t, false)
	mkInstalled(t, f.versionsDir, "1.4.1")
	var calls atomic.Int32
	f.svc.download = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	for _, in := range []string{"1.4.1", "v1.4.1", "V1.4.1", " 1.4.1 "} {
		got, err := f.svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed", in, got)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("去重路径不得启动下载，接缝被调 %d 次", n)
	}
	if len(f.svc.downloads) != 0 {
		t.Fatalf("already-installed 路径不得登记在飞名册，实得 %v", f.svc.downloads)
	}
}

// TestSetActiveVersionCaseInsensitiveSameToken V 形态设定版本与裸号同 token
// 落账（旧只剥小写 v 时 V 形态直报“非法版本号”，三入口互异）。
func TestSetActiveVersionCaseInsensitiveSameToken(t *testing.T) {
	f := newPicFixture(t, false)
	mkInstalled(t, f.versionsDir, "1.4.1")

	for _, in := range []string{"1.4.1", "v1.4.1", "V1.4.1", " 1.4.1 "} {
		got, err := f.svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v1.4.1" || f.svc.store.GetActive() != "v1.4.1" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v1.4.1", in, got, f.svc.store.GetActive())
		}
	}
}
