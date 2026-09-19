// check_update_test.go 覆盖 bespoke 下载链代表（snipaste，官网页面源 +
// 独立 releaseCache 注入）的 UpdateChecker shim。
package version

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newCheckManager 构造离线可控的 Manager：远程列表直接注入缓存实例（TTL 内
// 免网络），PE FileVersion 以固定值替身（真实 exe 才能过布局自检的场景不属于本 shim）。
func newCheckManager(t *testing.T, remote []SnipasteRelease, fileVer string) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.cache = &releaseCache{data: remote, fetchedAt: time.Now()}
	m.fileVersion = func(string) (string, error) { return fileVer, nil }
	return m
}

func installSnipaste(t *testing.T, m *Manager, version string) {
	t.Helper()
	dir := filepath.Join(m.versionsDir, dirPrefix+version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUpdateSnipasteFamily(t *testing.T) {
	ctx := context.Background()
	releases := []SnipasteRelease{
		{Version: "2.12.0"},
		{Version: "2.13.0-Beta1", IsPre: true},
	}

	t.Run("有稳定新版", func(t *testing.T) {
		m := newCheckManager(t, releases, "2.11.3")
		installSnipaste(t, m, "2.11.3")
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || !up || local != "2.11.3" || remote != "2.12.0" {
			t.Fatalf("got (%q,%q,%v,%v)", local, remote, up, err)
		}
	})

	t.Run("已最新", func(t *testing.T) {
		m := newCheckManager(t, releases, "2.12.0")
		installSnipaste(t, m, "2.12.0")
		if _, _, up, err := m.CheckUpdate(ctx); err != nil || up {
			t.Fatalf("up=%v err=%v, want 无更新", up, err)
		}
	})

	t.Run("未安装不误报", func(t *testing.T) {
		m := newCheckManager(t, releases, "2.12.0")
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || up || local != "" || remote != "2.12.0" {
			t.Fatalf("got (%q,%q,%v,%v)", local, remote, up, err)
		}
	})

	t.Run("仅预发布不点亮", func(t *testing.T) {
		m := newCheckManager(t, []SnipasteRelease{{Version: "2.13.0-Beta1", IsPre: true}}, "2.11.3")
		installSnipaste(t, m, "2.11.3")
		if _, _, up, err := m.CheckUpdate(ctx); err != nil || up {
			t.Fatalf("up=%v err=%v, want 无稳定条目→false", up, err)
		}
	})
}
