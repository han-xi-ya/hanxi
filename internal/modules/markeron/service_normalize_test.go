package markeron

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/markeron/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.14.0", "v2.14.0"},
		{"v2.14.0", "v2.14.0"},
		{"V2.14.0", "v2.14.0"},
		{"  v2.14.0 ", "v2.14.0"},
		{" V2.14.0\t", "v2.14.0"},
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

// newNormalizeTestMarkerOnService 带具名 versionsDir 的最小装配（harness 同法，
// engine 不在下载/设定路径上留空）。
func newNormalizeTestMarkerOnService(t *testing.T) (*MarkerOnService, string) {
	t.Helper()
	versionsDir := t.TempDir()
	return &MarkerOnService{
		manager: version.NewManager(versionsDir),
		store:   newMarkeronStore(t.TempDir()),
		holder:  extapi.NewLeaseHolder(ID),
	}, versionsDir
}

// seedMarkeronInstalled 伪造已落位便携安装（MarkerOn.exe 非空 + markeron.portable 标记）。
func seedMarkeronInstalled(t *testing.T, versionsDir, ver string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "markeron_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MarkerOn.exe"), []byte("MZ伪造载荷"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "markeron.portable"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装四形态全
// already-installed，下载接缝零调用且版本树零新增（修复路径在 go 语句之前
// 同步返回，判据无赛跑）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir := newNormalizeTestMarkerOnService(t)
	seedMarkeronInstalled(t, versionsDir, "2.14.0")
	var calls atomic.Int32
	svc.download = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	for _, in := range []string{"2.14.0", "v2.14.0", "V2.14.0", " 2.14.0 "} {
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
	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
}

// TestSetActiveVersionCaseInsensitiveSameToken V 形态与裸号同 token 落账。
func TestSetActiveVersionCaseInsensitiveSameToken(t *testing.T) {
	svc, versionsDir := newNormalizeTestMarkerOnService(t)
	seedMarkeronInstalled(t, versionsDir, "2.14.0")

	for _, in := range []string{"2.14.0", "v2.14.0", "V2.14.0", " 2.14.0 "} {
		got, err := svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v2.14.0" || svc.store.GetActive() != "v2.14.0" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v2.14.0", in, got, svc.store.GetActive())
		}
	}
}
