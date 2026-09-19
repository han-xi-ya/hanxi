// check_update_test.go 覆盖 zip 托管族代表（markeron）的 UpdateChecker shim：
// 稳定通道比较、预发布忽略、未安装/最新相等/无事实各归其位，取消即败。
package version

import (
	"context"
	"testing"
	"time"
)

// seedRemoteList 多条目注入（含预发布）；remoteCache 为包内全局，测试串行执行、
// 用后还原为空（避免把假列表泄漏给后续用例，与 seedRemote 的"setup 重灌"纪律互补）。
func seedRemoteList(t *testing.T, releases ...MarkerRelease) {
	t.Helper()
	remoteCache.mu.Lock()
	remoteCache.data = releases
	remoteCache.digests = map[string]string{}
	remoteCache.fetchedAt = time.Now()
	remoteCache.mu.Unlock()
	t.Cleanup(func() {
		remoteCache.mu.Lock()
		remoteCache.data = nil
		remoteCache.digests = nil
		remoteCache.fetchedAt = time.Time{}
		remoteCache.mu.Unlock()
	})
}

func TestCheckUpdateZipFamilyMatrix(t *testing.T) {
	ctx := context.Background()

	t.Run("未安装托管资产=无法判定", func(t *testing.T) {
		seedRemoteList(t, MarkerRelease{Version: "v3.0.0"})
		m := NewManager(t.TempDir())
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || up || local != "" || remote != "v3.0.0" {
			t.Fatalf("got (%q,%q,%v,%v)", local, remote, up, err)
		}
	})

	t.Run("远程有稳定新版=有更新", func(t *testing.T) {
		seedRemoteList(t,
			MarkerRelease{Version: "v3.1.0-beta.1", IsPre: true},
			MarkerRelease{Version: "v3.0.0"},
		)
		dir := t.TempDir()
		installFakeVersion(t, dir, "markeron_2.9.4", "x", true)
		m := NewManager(dir)
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || !up || local != "v2.9.4" || remote != "v3.0.0" {
			t.Fatalf("got (%q,%q,%v,%v), want (v2.9.4,v3.0.0,true,nil)", local, remote, up, err)
		}
	})

	t.Run("本机已最新=current", func(t *testing.T) {
		seedRemoteList(t, MarkerRelease{Version: "v2.9.4"})
		dir := t.TempDir()
		installFakeVersion(t, dir, "markeron_2.9.4", "x", true)
		m := NewManager(dir)
		_, _, up, err := m.CheckUpdate(ctx)
		if err != nil || up {
			t.Fatalf("got (up=%v, err=%v), want 无更新", up, err)
		}
	})

	t.Run("仅预发布更新=不点亮全体信号", func(t *testing.T) {
		seedRemoteList(t, MarkerRelease{Version: "v3.1.0-beta.1", IsPre: true})
		dir := t.TempDir()
		installFakeVersion(t, dir, "markeron_2.9.4", "x", true)
		m := NewManager(dir)
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || up || remote != "" || local != "" {
			t.Fatalf("got (%q,%q,%v,%v), want 无稳定条目→false", local, remote, up, err)
		}
	})

	t.Run("历史 v 前缀目录与无 v 远程形状对齐", func(t *testing.T) {
		seedRemoteList(t, MarkerRelease{Version: "v2.9.4"})
		dir := t.TempDir()
		installFakeVersion(t, dir, "markeron_v2.9.4", "x", true) // 历史目录名
		m := NewManager(dir)
		_, _, up, err := m.CheckUpdate(ctx)
		if err != nil || up {
			t.Fatalf("got (up=%v, err=%v), want 形状差异不误报", up, err)
		}
	})

	t.Run("上下文已取消=判定失败", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		m := NewManager(t.TempDir())
		if _, _, _, err := m.CheckUpdate(canceled); err == nil {
			t.Fatal("取消上下文应返回错误")
		}
	})
}
