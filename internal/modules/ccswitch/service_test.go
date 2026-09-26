package ccswitch

import (
	"context"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/ccswitch/instance"
	"hanxi/internal/modules/ccswitch/version"
)

// TestShouldIdleQuit 空闲退出判定穷举：仅"自有 running + 主窗口未开 + 超阈值"才退出。
// 关窗驻托盘（窗口不可见）不豁免——无人操作 3 分钟退出是明确需求。
func TestShouldIdleQuit(t *testing.T) {
	running := instance.Snapshot{State: instance.StateRunning, External: false}
	runningExt := instance.Snapshot{State: instance.StateRunning, External: true}
	stopped := instance.Snapshot{State: instance.StateStopped}

	cases := []struct {
		name       string
		snap       instance.Snapshot
		windowOpen bool
		idle       time.Duration
		want       bool
	}{
		{"超阈值无窗口", running, false, idleQuitAfter + time.Minute, true},
		{"恰好阈值", running, false, idleQuitAfter, true},
		{"未达阈值", running, false, idleQuitAfter - time.Minute, false},
		{"窗口开着豁免", running, true, idleQuitAfter + time.Minute, false},
		{"外部实例不碰", runningExt, false, idleQuitAfter + time.Minute, false},
		{"未运行不退出", stopped, false, idleQuitAfter + time.Minute, false},
		{"零空闲", running, false, 0, false},
	}
	for _, c := range cases {
		if got := shouldIdleQuit(c.snap, c.windowOpen, c.idle); got != c.want {
			t.Errorf("%s: shouldIdleQuit = %v, want %v", c.name, got, c.want)
		}
	}
}

// newTestCCSwitchService 装配下载时序单测的最小 service（termora harness 同法：
// 结构体字面量注入；调用门未挂 = Enter/EnterBackground 直通，ops 账本未注入
// = 事务降级 no-op，engine 不在下载路径上故留空）。
func newTestCCSwitchService(t *testing.T) *CCSwitchService {
	t.Helper()
	return &CCSwitchService{
		manager:   version.NewManager(t.TempDir()),
		store:     newCCSwitchStore(t.TempDir()),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
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
	svc := newTestCCSwitchService(t)
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

	// DownloadVersion 归一化 v 前缀，落账与回执都应按归一化后的口径核对。
	if state, err := svc.DownloadVersion("1.5.0"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v1.5.0" {
		t.Fatalf("done 回执版本应为归一化的 v1.5.0，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v1.5.0" {
		t.Fatalf("done 成功回执广播位上 active 应已落账为 v1.5.0，实得 %q", rr.activeAtBroadcast)
	}
	// 已有使用版本时不得被新下载覆盖（落账条件仅"未设使用"，语义随迁移保持不变）。
	if state, err := svc.DownloadVersion("1.6.0"); err != nil || state != "started" {
		t.Fatalf("第二次 DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v1.6.0" {
		t.Fatalf("第二次下载应广播 v1.6.0 的 done 回执，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v1.5.0" {
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
