package piclite

import (
	"context"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/piclite/instance"
	"hanxi/internal/modules/piclite/version"
)

// TestDownloadDoneSettlesActiveBeforeBroadcast 锁死 termora 2ac9b3b 同构竞态：
// "首装自动设使用"必须在 done 事件广播的同一收口步（emit 闭包内）先落账，
// 而非等 DownloadContext 返回之后——否则前端共享 store 收到 done 即复刷版本区
// 读 GetActiveVersion，瞬时读到空值。以 download 函数接缝注入假实现（无网络），
// 判据取在 done 送达后、DownloadContext 尚未返回的时点（channel 建立同步）。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	newSvc := func(t *testing.T) (*PicLiteService, chan string) {
		t.Helper()
		activeAtDone := make(chan string, 1)
		svc := &PicLiteService{
			manager:   version.NewManager(t.TempDir()),
			store:     newPicliteStore(t.TempDir()),
			holder:    extapi.NewLeaseHolder(ID),
			downloads: map[string]struct{}{},
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
		if _, err := svc.DownloadVersion("1.2.3"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "v1.2.3" {
			t.Fatalf("done 送达时（DownloadContext 内）active 应已落账为 v1.2.3，实得 %q", got)
		}
	})

	t.Run("已手选active不被done覆盖", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if err := svc.store.SetActive("v1.2.2"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DownloadVersion("1.2.3"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "v1.2.2" {
			t.Fatalf("done 送达时 active 应保持用户手选的 v1.2.2，实得 %q", got)
		}
	})
}

// TestShouldIdleQuit 空闲退出判定穷举：仅 running 且非 external、无可见窗口、
// 空闲时长达标才退。
func TestShouldIdleQuit(t *testing.T) {
	running := instance.Snapshot{State: instance.StateRunning}
	external := instance.Snapshot{State: instance.StateExternal, External: true}
	stopped := instance.Snapshot{State: instance.StateStopped}
	long := idleQuitAfter + time.Minute
	short := idleQuitAfter - time.Minute

	cases := []struct {
		name string
		snap instance.Snapshot
		win  bool
		idle time.Duration
		want bool
	}{
		{"running+窗口开+久空闲→豁免", running, true, long, false},
		{"running+窗口关+久空闲→退出", running, false, long, true},
		{"running+窗口关+短空闲→不退", running, false, short, false},
		{"external→永不接管退出", external, false, long, false},
		{"stopped→不退", stopped, false, long, false},
	}
	for _, c := range cases {
		if got := shouldIdleQuit(c.snap, c.win, c.idle); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// TestVersionCompare 数值分段比较：多位数段不被字典序坑。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.4.1", "v1.4.0", 1},
		{"v1.9.0", "v1.10.0", -1}, // 字典序会判反
		{"v1.4.1", "v1.4.1", 0},
		{"v2.0.0", "v1.99.99", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
