package dbx

import (
	"context"
	"sync/atomic"
	"testing"

	"hanxi/internal/modules/dbx/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.2.3", "v1.2.3"},
		{"v1.2.3", "v1.2.3"},
		{"V1.2.3", "v1.2.3"},
		{"  v1.2.3 ", "v1.2.3"},
		{" V1.2.3\t", "v1.2.3"},
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

// TestDownloadVersionInstalledDedupCaseInsensitive 已装假账下四种同版本入参
// 都回 already-installed，且下载接缝零调用、在途名册零登记。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc := newTestDBXService(t, nil)
	fm := svc.manager.(*fakeManager)
	fm.installed = []version.DBXVersionInfo{{Version: "v1.2.3", ExePath: `X:\dbx_1.2.3\DBX.exe`}}
	var calls atomic.Int32
	svc.downloadDriver = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	for _, in := range []string{"1.2.3", "v1.2.3", "V1.2.3", " 1.2.3 "} {
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

// TestSetActiveVersionCaseInsensitiveSameToken 三形态同 token：V 不再“非法版本号”。
func TestSetActiveVersionCaseInsensitiveSameToken(t *testing.T) {
	svc := newTestDBXService(t, nil)
	fm := svc.manager.(*fakeManager)
	fm.installed = []version.DBXVersionInfo{{Version: "v1.2.3", ExePath: `X:\dbx_1.2.3\DBX.exe`}}

	for _, in := range []string{"1.2.3", "v1.2.3", "V1.2.3", " 1.2.3 "} {
		got, err := svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v1.2.3" || svc.store.GetActive() != "v1.2.3" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v1.2.3", in, got, svc.store.GetActive())
		}
	}
}
