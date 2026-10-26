package douzy

import (
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/douzy/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.11.5", "v0.11.5"},
		{"v0.11.5", "v0.11.5"},
		{"V0.11.5", "v0.11.5"},
		{"  v0.11.5 ", "v0.11.5"},
		{" V0.11.5\t", "v0.11.5"},
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

// newNormalizeTestDouzyService 装配归一用例最小 service（本模块不托管进程、
// 无 store，字段面恰够下载去重与删包路径）。
func newNormalizeTestDouzyService(t *testing.T) (*DouzyService, string) {
	t.Helper()
	versionsDir := t.TempDir()
	return &DouzyService{
		manager:   version.NewManager(versionsDir),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: make(map[string]struct{}),
	}, versionsDir
}

// seedDouzyInstalled 伪造已下载安装包目录（douzy_x.y.z 内 *.exe 非空）。
func seedDouzyInstalled(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "douzy_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "douzy-setup.exe"), []byte("MZ伪造安装包"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装四形态都回
// already-installed，且名册零登记（登记先于 goroutine，判据确定性成立）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir := newNormalizeTestDouzyService(t)
	seedDouzyInstalled(t, versionsDir, "0.11.5")

	for _, in := range []string{"0.11.5", "v0.11.5", "V0.11.5", " 0.11.5 "} {
		got, err := svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed", in, got)
		}
	}
	if len(svc.downloads) != 0 {
		t.Fatalf("already-installed 路径不得登记在飞名册，实得 %v", svc.downloads)
	}
}

// TestRemoveVersionUppercaseVConsistent 同一值的三入口一致：V 形态删包不再
// “非法版本号”，删除效果与裸号一致。
func TestRemoveVersionUppercaseVConsistent(t *testing.T) {
	svc, versionsDir := newNormalizeTestDouzyService(t)
	dir := seedDouzyInstalled(t, versionsDir, "0.11.5")

	if err := svc.RemoveVersion("V0.11.5"); err != nil {
		t.Fatalf("RemoveVersion(V 形态): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("V 形态删包应删除目录，存在性检查实得 err=%v", err)
	}
}
