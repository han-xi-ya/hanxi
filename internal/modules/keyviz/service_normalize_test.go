package keyviz

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/keyviz/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.0.47", "v1.0.47"},
		{"v1.0.47", "v1.0.47"},
		{"V1.0.47", "v1.0.47"},
		{"  v1.0.47 ", "v1.0.47"},
		{" V1.0.47\t", "v1.0.47"},
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

// newNormalizeTestKeyvizService 带具名 versionsDir 的最小装配（harness 同法，
// engine 不在下载/设定路径上留空）。
func newNormalizeTestKeyvizService(t *testing.T) (*KeyvizService, string) {
	t.Helper()
	versionsDir := t.TempDir()
	return &KeyvizService{
		manager:   version.NewManager(versionsDir),
		store:     newKeyvizStore(t.TempDir()),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}, versionsDir
}

// seedKeyvizInstalled 伪造已落位版本目录（keyviz.exe 非空）。
func seedKeyvizInstalled(t *testing.T, versionsDir, ver string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "keyviz_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keyviz.exe"), []byte("MZ伪造载荷"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装四形态全
// already-installed，下载接缝零调用、在途名册零登记（登记先于 goroutine，
// 判据确定性成立）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir := newNormalizeTestKeyvizService(t)
	seedKeyvizInstalled(t, versionsDir, "1.0.47")
	var calls atomic.Int32
	svc.download = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	for _, in := range []string{"1.0.47", "v1.0.47", "V1.0.47", " 1.0.47 "} {
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
	svc, versionsDir := newNormalizeTestKeyvizService(t)
	seedKeyvizInstalled(t, versionsDir, "1.0.47")

	for _, in := range []string{"1.0.47", "v1.0.47", "V1.0.47", " 1.0.47 "} {
		got, err := svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v1.0.47" || svc.store.GetActive() != "v1.0.47" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v1.0.47", in, got, svc.store.GetActive())
		}
	}
}
