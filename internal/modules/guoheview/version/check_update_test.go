// check_update_test.go 覆盖双通道（stable+beta 混合列表） bespoke 源代表
// （果核看图）的 UpdateChecker shim：beta 永不点亮、stable 形状 v+四段对齐。
package version

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedRemoteList 直灌双通道列表（download_test.go 的 seedRemote 只注单条
// stable，多条目场景在本文件自备注入器；全局缓存用后还原，纪律同族）。
func seedRemoteList(t *testing.T, releases ...ViewRelease) {
	t.Helper()
	remoteCache.mu.Lock()
	remoteCache.data = releases
	remoteCache.fetchedAt = time.Now()
	remoteCache.mu.Unlock()
	t.Cleanup(func() {
		remoteCache.mu.Lock()
		remoteCache.data = nil
		remoteCache.fetchedAt = time.Time{}
		remoteCache.mu.Unlock()
	})
}

func installView(t *testing.T, root, version string) {
	t.Helper()
	dir := filepath.Join(root, dirPrefix+version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, portableMarkName), []byte(portableMarkText), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUpdateGuoheViewFamily(t *testing.T) {
	ctx := context.Background()

	t.Run("beta 领先 stable 追新", func(t *testing.T) {
		seedRemoteList(t,
			ViewRelease{Version: "v3.3.0.101", Channel: "beta", IsPre: true},
			ViewRelease{Version: "v3.2.8.99", Channel: "stable"},
		)
		dir := t.TempDir()
		installView(t, dir, "3.2.7.98")
		m := NewManager(dir)
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || !up || local != "v3.2.7.98" || remote != "v3.2.8.99" {
			t.Fatalf("got (%q,%q,%v,%v), want (v3.2.7.98,v3.2.8.99,true,nil)", local, remote, up, err)
		}
	})

	t.Run("仅 beta 领先=不点亮", func(t *testing.T) {
		seedRemoteList(t,
			ViewRelease{Version: "v3.3.0.101", Channel: "beta", IsPre: true},
			ViewRelease{Version: "v3.2.7.98", Channel: "stable"},
		)
		dir := t.TempDir()
		installView(t, dir, "3.2.7.98")
		m := NewManager(dir)
		if _, _, up, err := m.CheckUpdate(ctx); err != nil || up {
			t.Fatalf("up=%v err=%v, want 无更新", up, err)
		}
	})

	t.Run("无稳定通道条目", func(t *testing.T) {
		seedRemoteList(t, ViewRelease{Version: "v3.3.0.101", Channel: "beta", IsPre: true})
		dir := t.TempDir()
		installView(t, dir, "3.2.7.98")
		m := NewManager(dir)
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || up || local != "" || remote != "" {
			t.Fatalf("got (%q,%q,%v,%v)", local, remote, up, err)
		}
	})
}
