package supervisor

// A1 审查批次：秒退竞态三连（Job 句柄入账闸 / 外部读数代际守卫 / 就绪轮询可中断）。
//
// 手法：不 sleep 猜时序，用"探针/JobAPI 在指定步骤驻留、由测试方释放"把
// wait 收口与 Start 后半段的交错钉成确定序列；断言全部落在外部可观察事实
// （Job 句柄关闭次数、终态与事件流、Start 返回时限）。

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/platform"
)

// ---------- 1. 秒退竞态：Job 句柄不得无主泄露、终态不得被覆盖 ----------

// lateBoundJob 是被"迟到创建"的 Job 句柄替身，计数其关闭次数。
type lateBoundJob struct {
	assigned   atomic.Int64
	terminated atomic.Int64
	closed     atomic.Int64
}

func (j *lateBoundJob) Assign(uint32) error            { j.assigned.Add(1); return nil }
func (j *lateBoundJob) Terminate(uint32) error         { j.terminated.Add(1); return nil }
func (j *lateBoundJob) SetAllowKillOnClose(bool) error { return nil }
func (j *lateBoundJob) Close() error                   { j.closed.Add(1); return nil }

// lateBoundJobAPI 把"进程秒退且 wait 已收口"的竞态窗口钉成确定序列：
// Create 先令进程退场，再驻留等到引擎状态离开 starting（即 wait 完成分类与
// 资源收口）后才产出句柄——此时 Start 若把句柄入账 e.job，就永远无人再关闭。
type lateBoundJobAPI struct {
	e    *Engine
	h    *fakeHandle
	last atomic.Pointer[lateBoundJob]
}

func (a *lateBoundJobAPI) Create() (platform.Job, error) {
	a.h.exitWith(nil, 0) // 单实例转发型秒退（markeron 蓝本场景）
	deadline := time.Now().Add(5 * time.Second)
	for a.e.Snapshot().State == StateStarting {
		if time.Now().After(deadline) {
			return nil, errors.New("测试等待 wait 收口超时")
		}
		time.Sleep(time.Millisecond)
	}
	j := &lateBoundJob{}
	a.last.Store(j)
	return j, nil
}

func TestStartJobBoundAfterImmediateExitIsClosed(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(false, nil)
	jobs := &lateBoundJobAPI{h: hs.h, e: hs.e}
	hs.e.jobs = jobs

	err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`})
	if err == nil {
		t.Fatal("进程在 Start 返回前已退出，必须如实报错而非假装启动成功")
	}
	snap := hs.e.Snapshot()
	if snap.State != StateStopped || snap.Managed {
		t.Fatalf("秒退终态不得被迟到的 running 覆盖: %+v", snap)
	}
	j := jobs.last.Load()
	if j == nil {
		t.Fatal("Job 应已被创建")
	}
	if got := j.closed.Load(); got != 1 {
		t.Fatalf("代际失配的 Job 句柄必须当场关闭（泄露检查）: closed=%d, want 1", got)
	}

	// 引擎未被悬空 e.job 毒化：换真实例与 JobAPI 后可正常再起，
	// 且上一代的句柄不会被下一代 wait 重复关闭或继续持有。
	h2 := newFakeHandle(4243)
	hs.e.open = func(Spec) (processHandle, error) { return h2, nil }
	hs.e.jobs = &fakeJobAPI{h: h2}
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("收口后下一代启动: %v", err)
	}
	if got := hs.e.Snapshot().State; got != StateRunning {
		t.Fatalf("下一代 state = %s, want running", got)
	}
	if got := j.closed.Load(); got != 1 {
		t.Fatalf("上代 Job 句柄被重复处置: closed=%d, want 恒为 1", got)
	}
}

// ---------- 2. 慢探针补偿与 Start 竞态：过期 external 读数不得覆盖在世代 ----------

// gatedProbe 把 Inspect 钉成"进入即上报、释放才返回 running=true"，
// 用于确定性地复现"探针飞行途中引擎完成了启动"。
type gatedProbe struct {
	entered chan struct{}
	release chan struct{}
	info    *platform.ProcInfo
	once    sync.Once
}

func (p *gatedProbe) Inspect(context.Context) (bool, *platform.ProcInfo, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return true, p.info, nil
}

func TestRefreshExternalCannotStompLiveGeneration(t *testing.T) {
	hs := newHarness(t)
	gp := &gatedProbe{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		info:    &platform.ProcInfo{PID: 999, ExePath: `D:\x\tool.exe`},
	}
	hs.e.probe = gp

	refreshed := make(chan struct{})
	go func() {
		hs.e.RefreshExternal()
		close(refreshed)
	}()

	<-gp.entered // 探针在飞行中；此时引擎状态仍是静止态（RefreshExternal 已过前置闸）
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	close(gp.release) // 过期读数此刻才落地
	<-refreshed

	snap := hs.e.Snapshot()
	if snap.State != StateRunning || !snap.Managed || snap.PID != 4242 {
		t.Fatalf("过期 external 读数不得覆盖在世受管代际: %+v", snap)
	}
	for _, s := range hs.events.states() {
		if s == StateExternal {
			t.Fatalf("守卫生效时不应广播 external 事件: %v", hs.events.states())
		}
	}
}

// ---------- 3. 就绪轮询的可中断性（外部事实：取消/秒退即时生效，不烧完轮询间隔） ----------

// 生产 readyPollInterval=150ms 会掩盖响应性差异，本组用例放大到 2s：
// 裸 time.Sleep 的旧实现必须空耗整段间隔才回到判定环，选择定时器则毫秒级返回。

func TestStartReadyCancelRespondsWithinPollGap(t *testing.T) {
	hs := newHarness(t)
	oldPoll := readyPollInterval
	readyPollInterval = 2 * time.Second
	t.Cleanup(func() { readyPollInterval = oldPoll })
	hs.probe.set(false, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := hs.e.Start(ctx, Spec{Exe: `C:\tool\tool.exe`, ReadyTimeout: 30 * time.Second})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("上下文取消必须中止就绪等待")
	}
	if elapsed > time.Second {
		t.Fatalf("取消须即时生效而非烧完整段 2s 轮询间隔: elapsed=%v", elapsed)
	}
	if _, terminated, _ := hs.jobs.last().counts(); terminated != 1 {
		t.Fatal("取消后必须终止受管进程")
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("取消收口 state = %s, want failed", got)
	}
}

// TestStartReadyTimeoutClampHonorsDeadline 锁"末轮睡眠 min(轮询间隔, 剩余预算)"
// 的外部行为：轮询间隔被放大到 2s、ReadyTimeout 只有 150ms 时，超时报错必须落在
// 截止线附近（钳到 remain），而不是被整段轮询间隔带飞（≥2s 才回到 deadline 判定）。
// 断言的是实际用时与状态终局这两个可观察事实，不是 min/if 的代码形状。
func TestStartReadyTimeoutClampHonorsDeadline(t *testing.T) {
	hs := newHarness(t)
	oldPoll := readyPollInterval
	readyPollInterval = 2 * time.Second
	t.Cleanup(func() { readyPollInterval = oldPoll })
	hs.probe.set(false, nil)

	start := time.Now()
	err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`, ReadyTimeout: 150 * time.Millisecond})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("永不可就绪必须超时失败")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("超时判定被整段轮询间隔带飞（钳制失效）: elapsed=%v", elapsed)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("不得未到期就谎报超时: elapsed=%v", elapsed)
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("超时收口 state = %s, want failed", got)
	}
}

func TestStartReadyAbortsPromptlyOnEarlyExitDuringPollGap(t *testing.T) {
	hs := newHarness(t)
	oldPoll := readyPollInterval
	readyPollInterval = 2 * time.Second
	t.Cleanup(func() { readyPollInterval = oldPoll })
	hs.probe.set(false, nil)

	go func() {
		time.Sleep(80 * time.Millisecond)
		hs.h.exitWith(errors.New("exit status 7"), 7)
	}()
	start := time.Now()
	err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`, ReadyTimeout: 30 * time.Second})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("进程退出必须中止就绪等待")
	}
	if elapsed > time.Second {
		t.Fatalf("秒退检测须即时生效而非等满 2s 轮询间隔: elapsed=%v", elapsed)
	}
	waitState(t, hs.e, StateFailed)
}
