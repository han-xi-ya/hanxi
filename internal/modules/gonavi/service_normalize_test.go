package gonavi

import (
	"context"
	"sync/atomic"
	"testing"

	"hanxi/internal/modules/gonavi/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.8.9", "v0.8.9"},
		{"v0.8.9", "v0.8.9"},
		{"V0.8.9", "v0.8.9"},
		{"  v0.8.9 ", "v0.8.9"},
		{" V0.8.9\t", "v0.8.9"},
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
// 带空白四种入参都回 already-installed，且下载接缝零调用、在途名册零登记。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	fm := &fakeManager{installed: []version.GoNaviVersionInfo{{Version: "v0.8.9", ExePath: `X:\gonavi_0.8.9\GoNavi.exe`}}}
	svc, _ := newTestGoNaviService(t, fm)
	var calls atomic.Int32
	svc.downloadDriver = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	for _, in := range []string{"0.8.9", "v0.8.9", "V0.8.9", " 0.8.9 "} {
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

// TestSetActiveVersionCaseInsensitiveSameToken V 形态设定版本与裸号同 token
// 落账（旧只剥小写 v 时 V 形态直报“非法版本号”，三入口互异）。
// 假件 ResolveExe 走 resolve 账（同包既有纪律），按归一后 v0.8.9 记账。
func TestSetActiveVersionCaseInsensitiveSameToken(t *testing.T) {
	fm := &fakeManager{
		installed: []version.GoNaviVersionInfo{{Version: "v0.8.9", ExePath: `X:\gonavi_0.8.9\GoNavi.exe`}},
		resolve:   map[string]string{"v0.8.9": `X:\gonavi_0.8.9\GoNavi.exe`},
	}
	svc, _ := newTestGoNaviService(t, fm)

	for _, in := range []string{"0.8.9", "v0.8.9", "V0.8.9", " 0.8.9 "} {
		got, err := svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v0.8.9" || svc.store.GetActive() != "v0.8.9" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v0.8.9", in, got, svc.store.GetActive())
		}
	}
}
