package paseo

import (
	"context"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/paseo/version"
)

// TestDownloadDoneSettlesActiveBeforeBroadcast 锁死 termora 2ac9b3b 同构竞态：
// "首装自动设使用"必须在 done 事件广播的同一收口步（emit 闭包内）先落账，
// 而非等 DownloadContext 返回之后——否则前端共享 store 收到 done 即复刷版本区
// 读 GetActiveVersion，瞬时读到空值。以 download 函数接缝注入假实现（无网络），
// 判据取在 done 送达后、DownloadContext 尚未返回的时点（channel 建立同步）。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	newSvc := func(t *testing.T) (*PaseoService, chan string) {
		t.Helper()
		activeAtDone := make(chan string, 1)
		svc := &PaseoService{
			manager:   version.NewManager(t.TempDir()),
			store:     newPaseoStore(t.TempDir()),
			holder:    extapi.NewLeaseHolder(ID),
			downloads: map[string]struct{}{},
		}
		svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
			emit(version.DownloadProgress{Version: ver, Stage: "downloading", Done: 10, Total: 100})
			emit(version.DownloadProgress{Version: ver, Stage: "verify"})
			emit(version.DownloadProgress{Version: ver, Stage: "extract"})
			emit(version.DownloadProgress{Version: ver, Stage: "done", Done: 100, Total: 100})
			activeAtDone <- svc.store.GetActive()
			return nil
		}
		return svc, activeAtDone
	}
	receive := func(t *testing.T, ch chan string) string {
		t.Helper()
		select {
		case got := <-ch:
			return got
		case <-time.After(3 * time.Second):
			t.Fatal("后台下载收口未在时限内送达 done")
			return ""
		}
	}

	t.Run("首装done即落账", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if _, err := svc.DownloadVersion("v0.1.20"); err != nil {
			t.Fatal(err)
		}
		// paseo 版本口径去 v 前缀
		if got := receive(t, activeAtDone); got != "0.1.20" {
			t.Fatalf("done 送达时（DownloadContext 内）active 应已落账为 0.1.20，实得 %q", got)
		}
	})

	t.Run("已手选active不被done覆盖", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if err := svc.store.SetActive("0.1.19"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DownloadVersion("v0.1.20"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "0.1.19" {
			t.Fatalf("done 送达时 active 应保持用户手选的 0.1.19，实得 %q", got)
		}
	})
}

// TestSortInstalledOrdering 冷启动"自动最新已装"回退依赖此排序：
// stable 恒大于同名预发布，beta 间按序号，imported 时间戳退化字典序排最后。
func TestSortInstalledOrdering(t *testing.T) {
	list := []version.PaseoVersionInfo{
		{Version: "imported-20260915-010203"},
		{Version: "0.8.0-beta.2"},
		{Version: "0.7.2"},
		{Version: "0.8.0"},
		{Version: "0.8.0-beta.1"},
	}
	sortInstalled(list)
	want := []string{"0.8.0", "0.8.0-beta.2", "0.8.0-beta.1", "0.7.2", "imported-20260915-010203"}
	for i := range want {
		if list[i].Version != want[i] {
			t.Fatalf("排序异常: %d = %s, want %s (%v)", i, list[i].Version, want[i], versionsOf(list))
		}
	}
}

func versionsOf(list []version.PaseoVersionInfo) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v.Version
	}
	return out
}
