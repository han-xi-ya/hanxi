package keyviz

import (
	"context"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/keyviz/version"
)

// TestDownloadDoneSettlesActiveBeforeBroadcast 锁死 termora 2ac9b3b 同构竞态：
// "首装自动设使用"必须在 done 事件广播的同一收口步（emit 闭包内）先落账，
// 而非等 DownloadContext 返回之后——否则前端共享 store 收到 done 即复刷版本区
// 读 GetActiveVersion，瞬时读到空值。以 download 函数接缝注入假实现（无网络），
// 在 done 送达后、DownloadContext 尚未返回时读 store 即为判据。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	// 假下载在 done 送达后立即（仍在 DownloadContext 调用栈内、广播之后但收口
	// 之前）把读到的 active 经 channel 回传：channel 收发建立 happens-before，
	// 无需忙等，且判据锁在"done 这一步"而非整个下载结束后。
	newSvc := func(t *testing.T) (*KeyvizService, chan string) {
		t.Helper()
		activeAtDone := make(chan string, 1)
		svc := &KeyvizService{
			manager:   version.NewManager(t.TempDir()),
			store:     newKeyvizStore(t.TempDir()),
			holder:    extapi.NewLeaseHolder(ID),
			downloads: map[string]struct{}{},
		}
		svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
			emit(version.DownloadProgress{Version: ver, Stage: "downloading", Done: 10, Total: 100})
			emit(version.DownloadProgress{Version: ver, Stage: "extract"})
			// done 送达：闭包内先落账再广播，此时 DownloadContext 尚未返回
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
		if _, err := svc.DownloadVersion("1.0.47"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "v1.0.47" {
			t.Fatalf("done 送达时（DownloadContext 内）active 应已落账为 v1.0.47，实得 %q", got)
		}
	})

	t.Run("已手选active不被done覆盖", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if err := svc.store.SetActive("v0.9.0"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DownloadVersion("1.0.47"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "v0.9.0" {
			t.Fatalf("done 送达时 active 应保持用户手选的 v0.9.0，实得 %q", got)
		}
	})
}

// TestVersionCompare 数值分段比较：多位数段不被字典序坑。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.1.1", "v2.1.0", 1},
		{"v2.9.0", "v2.10.0", -1}, // 字典序会判反
		{"v2.1.1", "v2.1.1", 0},
		{"v2.0.0", "v1.99.99", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
