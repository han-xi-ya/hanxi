package supervisor

// Start 失败收口与 wait 收口的两种交错钉定（TestStartAssignFail 偶发红定位）。
//
// 教训：fakeJob.Close 曾不分 Job 成员一律模拟 KILL_ON_JOB_CLOSE——Assign 失败
// 路径里进程从未入账该 Job（真实 Windows 关这条句柄不会触动进程），替身却抢先
// 把它"杀掉"，等于在引擎读到 e.h 之前凭空抽走进程，wait 收口（清 e.h）与
// abortStart 终止（读 e.h）谁先跑成调度运气：红率约七成（-count=1 ×20 实测 14 红，
// 内核首发 3a367f4 同样复现，与 4f11ec6 的代际守卫无关）。
// 替身 faithful 化（Close 只杀已入账成员）后，真实世界的两种交错各有确定形态，
// 本文件用 onAssign 驻留与 closeKillSettle 驻留把两种交错钉成确定序列，
// 断言全部落在外部可观察事实，不靠调度撞、不放大超时。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// terminalSignal 给 harness 的状态回调加闸：引擎状态首次离开 starting 即关通道。
// 该回调由 wait 收口段的 transition 发出，发出时 e.h 必已清空（收口段先清再转），
// 因此"通道已关"等价"wait 已完成收口回写"，可作驻留信号。
func terminalSignal(t *testing.T, hs *harness) chan struct{} {
	t.Helper()
	settled := make(chan struct{})
	var once sync.Once
	base := hs.e.cb
	hs.e.cb = Callbacks{
		OnState: func(s Snapshot) {
			if base.OnState != nil {
				base.OnState(s)
			}
			if s.State != StateStarting {
				once.Do(func() { close(settled) })
			}
		},
		OnLog: base.OnLog,
	}
	return settled
}

// 交错一：abortStart 先终止。Assign 失败但进程在世（真实世界的主形态）：
// faithful 的 fakeJob.Close 不会替引擎触动非成员进程，wait 在 Kill 前绝无机会
// 收口，killSequence 必读得到 e.h 并经句柄终止。closeKillSettle 是照妖镜：
// 若 Close 再退回"不分成员一律杀"，本例确定变红而非变绿——杀掉的进程会先被
// wait 收口清掉 e.h，引擎此后隔着字段再也 Kill 不动任何东西。
func TestStartAssignFailKillsLiveProcessViaHandle(t *testing.T) {
	hs := newHarness(t)
	settled := terminalSignal(t, hs)
	hs.e.jobs = &fakeJobAPI{
		assignErr:       errors.New("boom"),
		h:               hs.h,
		closeKillSettle: settled,
	}
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if !hs.h.wasKilled() {
		t.Fatal("进程在世时 Assign 失败，引擎必须终止自己刚拉起的进程（经句柄 Kill）")
	}
	snap := hs.e.Snapshot()
	if snap.State != StateFailed {
		t.Fatalf("state = %s, want failed", snap.State)
	}
	if snap.Managed {
		t.Fatalf("收口后不应仍判受管: %+v", snap)
	}
}

// 交错二：wait 先收口。进程在 Assign 返回前已退出（真实高频：秒退工具让位竞速，
// Assign 面对的已是死进程）：onAssign 驻留等到 wait 完成收口回写才放行 Start，
// 把"wait 先收口"钉成确定序列。此时进程确实已消失即视为终止达成，引擎不得
// 隔着已清的 e.h 补刀，也不得因 killSequence 的"无可终止进程"报错而卡死或改判。
func TestStartAssignFailAfterWaitSettledCountsAsTerminated(t *testing.T) {
	hs := newHarness(t)
	settled := terminalSignal(t, hs)
	hs.e.jobs = &fakeJobAPI{
		assignErr: errors.New("boom"),
		h:         hs.h,
		onAssign: func() {
			hs.h.exitWith(errors.New("exit status 7"), 7) // 秒退：Assign 返回前进程已亡
			<-settled                                     // 驻留到 wait 收口回写落地
		},
	}
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	snap := hs.e.Snapshot()
	if snap.State != StateFailed {
		t.Fatalf("state = %s, want failed", snap.State)
	}
	if !strings.Contains(snap.Error, "JobObject 绑定失败") {
		t.Fatalf("终态原因应为绑定失败, got %q", snap.Error)
	}
	if !hs.h.wasWaited() {
		t.Fatal("wait 应已收殓该进程（驻留信号即以其完成为前提）")
	}
	if hs.h.wasKilled() {
		t.Fatal("进程已由 wait 收殓（确实消失）即视为终止达成，不得再补刀")
	}
}
