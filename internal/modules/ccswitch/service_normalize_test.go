package ccswitch

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/ccswitch/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.5.0", "v1.5.0"},
		{"v1.5.0", "v1.5.0"},
		{"V1.5.0", "v1.5.0"},
		{"  v1.5.0 ", "v1.5.0"},
		{" V1.5.0\t", "v1.5.0"},
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

// newNormalizeTestCCSwitchService 带具名 versionsDir 的最小装配（归一用例需向
// 树内伪造已装目录；harness 同法，engine 不在下载/设定路径上留空）。
func newNormalizeTestCCSwitchService(t *testing.T) (*CCSwitchService, string) {
	t.Helper()
	versionsDir := t.TempDir()
	return &CCSwitchService{
		manager:   version.NewManager(versionsDir),
		store:     newCCSwitchStore(t.TempDir()),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}, versionsDir
}

// seedCCSwitchInstalled 伪造已落位便携安装（exe 非空 + portable.ini 标记）。
func seedCCSwitchInstalled(t *testing.T, versionsDir, ver string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "ccswitch_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cc-switch.exe"), []byte("MZ伪造载荷"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "portable.ini"), []byte("[portable]"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装前提下裸号/v号/V号/
// 带空白四种入参都回 already-installed，且下载接缝被调次数=0、在途名册零登记
// （修复路径在名册登记与 go 语句之前同步返回，判据无赛跑）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir := newNormalizeTestCCSwitchService(t)
	seedCCSwitchInstalled(t, versionsDir, "1.5.0")
	var calls atomic.Int32
	svc.downloadDriver = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	for _, in := range []string{"1.5.0", "v1.5.0", "V1.5.0", " 1.5.0 "} {
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
func TestSetActiveVersionCaseInsensitiveSameToken(t *testing.T) {
	svc, versionsDir := newNormalizeTestCCSwitchService(t)
	seedCCSwitchInstalled(t, versionsDir, "1.5.0")

	for _, in := range []string{"1.5.0", "v1.5.0", "V1.5.0", " 1.5.0 "} {
		got, err := svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v1.5.0" || svc.store.GetActive() != "v1.5.0" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v1.5.0", in, got, svc.store.GetActive())
		}
	}
	for _, in := range []string{"9.9.9", "v9.9.9", "V9.9.9"} {
		if _, err := svc.SetActiveVersion(in); err == nil {
			t.Fatalf("未装版本 %q 应被拒（三种形态口径一致）", in)
		}
	}
}
