// check_update_test.go 覆盖 zip 托管族代表（Termora）的 UpdateChecker shim：稳定通道
// vs 本机已装账目的档位判定。
//
// 回归锚（2026-11-11 机主实报"可用更新 3 个但实际已追更"）：Termora 主干全在 beta，
// 稳定通道停在旧世代。本机 v2.0.0-beta.16 对远程 v1.0.17 必须判"无更新"；旧比较器把带
// 横杠的预发布形态整体判为非规范串、恒劣后于任何规范串，于是红点永不熄灭，且每轮重启由
// state/updates.json 回灌重亮。反向（稳定通道确实领先本机 beta）仍须点亮，不许把
// beta 一刀切成"永不报更新"。
package version

import (
	"context"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

// seedRemoteList 多条目远程注入（可含预发布）；remoteCache 为包内全局，用例串行执行、
// 用后还原为空，避免把假列表泄漏给后续用例（与 seedRemote 的"setup 重灌"纪律互补）。
func seedRemoteList(t *testing.T, releases ...TermoraRelease) {
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

func TestCheckUpdateBetaAgainstStableChannel(t *testing.T) {
	ctx := context.Background()

	// install 造一个已装版本目录（payload 由 installFixture 补齐）。
	install := func(t *testing.T, token string) *Manager {
		t.Helper()
		root := t.TempDir()
		installFixture(t, root, token, moduleMeta{},
			artifact.Meta{Entry: exeName, ZipSHA256: "b", Source: artifact.SourceRemote})
		return NewManager(root)
	}

	t.Run("本机 beta 领先稳定通道=不点亮", func(t *testing.T) {
		seedRemoteList(t, TermoraRelease{Version: "v1.0.17", AssetName: exeName})
		local, remote, up, err := install(t, "2.0.0-beta.16").CheckUpdate(ctx)
		if err != nil || up || local != "v2.0.0-beta.16" || remote != "v1.0.17" {
			t.Fatalf("got (%q,%q,%v,%v), want (v2.0.0-beta.16,v1.0.17,false,nil)", local, remote, up, err)
		}
	})

	t.Run("稳定通道领先本机 beta=照常点亮", func(t *testing.T) {
		seedRemoteList(t, TermoraRelease{Version: "v2.16.1", AssetName: exeName})
		local, remote, up, err := install(t, "2.0.0-beta.16").CheckUpdate(ctx)
		if err != nil || !up || local != "v2.0.0-beta.16" || remote != "v2.16.1" {
			t.Fatalf("got (%q,%q,%v,%v), want (v2.0.0-beta.16,v2.16.1,true,nil)", local, remote, up, err)
		}
	})

	t.Run("同核心稳定版优先于本机预发布", func(t *testing.T) {
		seedRemoteList(t, TermoraRelease{Version: "v1.0.17", AssetName: exeName})
		_, _, up, err := install(t, "1.0.17-beta.3").CheckUpdate(ctx)
		if err != nil || !up {
			t.Fatalf("got (up=%v, err=%v), want 正式版 1.0.17 > 其 beta → 有更新", up, err)
		}
	})

	t.Run("已装 beta 与稳定并存时最新已装取 beta", func(t *testing.T) {
		seedRemoteList(t, TermoraRelease{Version: "v1.0.17", AssetName: exeName})
		root := t.TempDir()
		installFixture(t, root, "1.0.17", moduleMeta{},
			artifact.Meta{Entry: exeName, ZipSHA256: "a", Source: artifact.SourceRemote})
		installFixture(t, root, "2.0.0-beta.16", moduleMeta{},
			artifact.Meta{Entry: exeName, ZipSHA256: "b", Source: artifact.SourceRemote})
		m := NewManager(root)
		list, err := m.ListInstalled()
		if err != nil || len(list) != 2 {
			t.Fatalf("ListInstalled = (%+v, %v)", list, err)
		}
		// installed[0] 是感知链的"最新已装"口径：数值更高的 beta 必须排在前。
		if list[0].Version != "v2.0.0-beta.16" {
			t.Fatalf("最新已装 = %q, want v2.0.0-beta.16（beta 不得因带横杠被排到稳定版之后）", list[0].Version)
		}
		if _, _, up, err := m.CheckUpdate(ctx); err != nil || up {
			t.Fatalf("CheckUpdate = (up=%v, err=%v), want 无更新", up, err)
		}
	})
}
