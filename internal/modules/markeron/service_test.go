package markeron

import (
	"context"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/markeron/version"
)

// TestDownloadDoneSettlesActiveBeforeBroadcast 锁死 termora 2ac9b3b 同构竞态：
// "首装自动设使用"必须在 done 事件广播的同一收口步（emit 闭包内）先落账，
// 而非等 DownloadContext 返回之后——否则前端共享 store 收到 done 即复刷版本区
// 读 GetActiveVersion，瞬时读到空值。以 download 函数接缝注入假实现（无网络），
// 判据取在 done 送达后、DownloadContext 尚未返回的时点（channel 建立同步）。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	newSvc := func(t *testing.T) (*MarkerOnService, chan string) {
		t.Helper()
		activeAtDone := make(chan string, 1)
		svc := &MarkerOnService{
			manager: version.NewManager(t.TempDir()),
			store:   newMarkeronStore(t.TempDir()),
			holder:  extapi.NewLeaseHolder(ID),
		}
		svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
			emit(version.DownloadProgress{Version: ver, Stage: "downloading", Done: 10, Total: 100})
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
		if _, err := svc.DownloadVersion("1.6.0"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "v1.6.0" {
			t.Fatalf("done 送达时（DownloadContext 内）active 应已落账为 v1.6.0，实得 %q", got)
		}
	})

	t.Run("已手选active不被done覆盖", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if err := svc.store.SetActive("v1.5.0"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DownloadVersion("1.6.0"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "v1.5.0" {
			t.Fatalf("done 送达时 active 应保持用户手选的 v1.5.0，实得 %q", got)
		}
	})
}
