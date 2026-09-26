package flclash

import (
	"context"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/flclash/instance"
	"hanxi/internal/modules/flclash/version"
)

// TestShouldIdleQuitDisabled 固化代理实例不因空闲自动退出的产品约束。
func TestShouldIdleQuitDisabled(t *testing.T) {
	cases := []struct {
		name       string
		snap       instance.Snapshot
		windowOpen bool
		idle       time.Duration
	}{
		{"自有运行实例", instance.Snapshot{State: instance.StateRunning}, false, 24 * time.Hour},
		{"窗口已打开", instance.Snapshot{State: instance.StateRunning}, true, 24 * time.Hour},
		{"外部运行实例", instance.Snapshot{State: instance.StateRunning, External: true}, false, 24 * time.Hour},
		{"已停止实例", instance.Snapshot{State: instance.StateStopped}, false, 24 * time.Hour},
	}
	for _, c := range cases {
		if shouldIdleQuit(c.snap, c.windowOpen, c.idle) {
			t.Errorf("%s: FlClash 不应因空闲自动退出", c.name)
		}
	}
}

// newTestFlClashService 装配下载时序单测的最小 service（termora harness 同法：
// 结构体字面量注入；调用门未挂 = Enter/EnterBackground 直通，ops 账本未注入
// = 事务降级 no-op，engine 不在下载路径上故留空）。
func newTestFlClashService(t *testing.T) *FlClashService {
	t.Helper()
	return &FlClashService{
		manager: version.NewManager(t.TempDir()),
		store:   newFlClashStore(t.TempDir()),
		holder:  extapi.NewLeaseHolder(ID),
	}
}

// downloadReceipt 下载事件票据：stage/version 为回执载荷，activeAtBroadcast
// 由探针在广播位（downloadProbe 与生产 Wails Emit 同一点位）于下载 goroutine
// 内同步快照——即"前端此刻复刷 GetActiveVersion 会读到什么"的实况。
type downloadReceipt struct {
	stage             string
	version           string
	activeAtBroadcast string
}

// 锁死 termora 2ac9b3b 同型病灶的修复：前端共享 store 收到 done 成功回执即
// 复刷 GetActiveVersion，"首装自动设使用"的落账必须先于事件广播完成（断言取
// 广播位快照，非事后补读——事后读会与被测代码赛跑，负序回归可能漏网）。假驱动
// 经 seam 隔离网络（绝不真下载），事件时序与 journal/租约全走生产闭包实现。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := newTestFlClashService(t)
	seen := make(chan downloadReceipt, 32)
	svc.downloadProbe = func(p version.DownloadProgress) {
		seen <- downloadReceipt{stage: p.Stage, version: p.Version, activeAtBroadcast: svc.store.GetActive()}
	}
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
		emit(version.DownloadProgress{Version: targetVersion, Stage: "downloading", Done: 1, Total: 2})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "extract"})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "done", Done: 100, Total: 100})
		return nil
	}

	if state, err := svc.DownloadVersion("0.8.12"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "0.8.12" {
		t.Fatalf("done 回执版本应为0.8.12，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "0.8.12" {
		t.Fatalf("done 成功回执广播位上 active 应已落账为 0.8.12，实得 %q", rr.activeAtBroadcast)
	}
	// 已有使用版本时不得被新下载覆盖（落账条件仅"未设使用"，语义随迁移保持不变）。
	if state, err := svc.DownloadVersion("0.8.13"); err != nil || state != "started" {
		t.Fatalf("第二次 DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "0.8.13" {
		t.Fatalf("第二次下载应广播 0.8.13 的 done 回执，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "0.8.12" {
		t.Fatalf("active 已有值时广播位不应被后续下载改写，实得 %q", rr.activeAtBroadcast)
	}
}

// waitDoneReceipt 等下载 goroutine 广播到 done 事件（drain 中间阶段），返回其
// 广播位快照；超时判失败。
func waitDoneReceipt(t *testing.T, seen <-chan downloadReceipt) downloadReceipt {
	t.Helper()
	for {
		select {
		case r := <-seen:
			if r.stage == "done" {
				return r
			}
		case <-time.After(5 * time.Second):
			t.Fatal("等待下载 done 事件超时")
			return downloadReceipt{}
		}
	}
}
