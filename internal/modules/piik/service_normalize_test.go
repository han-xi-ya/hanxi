package piik

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.6.5", "v1.6.5"},
		{"v1.6.5", "v1.6.5"},
		{"V1.6.5", "v1.6.5"},
		{"  v1.6.5 ", "v1.6.5"},
		{" V1.6.5\t", "v1.6.5"},
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

// TestDownloadVersionInstalledDedupCaseInsensitive 已装假账下裸号/v号/V号/
// 带空白四种入参都回 already-installed，下载接缝零调用、在途名册零登记
// （登记先于 goroutine，判据确定性成立；大写 V 旧归一产出 vV1.6.5 去重落空）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, fm, _, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}
	var calls atomic.Int32
	svc.downloadDriver = func(context.Context, string, string, func(DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	for _, in := range []string{"1.6.5", "v1.6.5", "V1.6.5", " 1.6.5 "} {
		got, err := svc.DownloadVersion(in)
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
	if len(svc.downloads) != 0 {
		t.Fatalf("already-installed 路径不得登记在飞名册，实得 %v", svc.downloads)
	}
}

// TestSetActiveVersionCaseInsensitiveSameToken V 形态与裸号同 token 落账。
func TestSetActiveVersionCaseInsensitiveSameToken(t *testing.T) {
	svc, fm, _, _ := newTestPiikService(t)
	fm.installed = []VersionInfo{{Version: "v1.6.5", ExePath: `X:\piik_1.6.5\piik-app.exe`}}

	for _, in := range []string{"1.6.5", "v1.6.5", "V1.6.5", " 1.6.5 "} {
		got, err := svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v1.6.5" || svc.store.GetActive() != "v1.6.5" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v1.6.5", in, got, svc.store.GetActive())
		}
	}
}
