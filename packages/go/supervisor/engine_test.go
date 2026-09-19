package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
)

// ---------- 测试替身：handle / JobAPI / Probe / ProcessAPI ----------
//
// 全部治理路径均走注入替身，不依赖真实进程（本仓 Git Bash 环境 cmd.exe 不可
// 解析，既有 */instance 测试同此约束；真机冒烟单独成例并 LookPath 守卫）。

type fakeHandle struct {
	mu       sync.Mutex
	startErr error
	pid      uint32
	waitErr  error
	code     int
	killed   bool
	waited   bool
	exitCh   chan struct{} // 关闭 = 进程退场（Wait 解除阻塞）
	// 日志泵管道注入（Start 前设置；nil = 该路不可得，模拟不转发输出的实现）
	stdout        io.Reader
	stderr        io.Reader
	closersOnExit []io.Closer // exitWith 时关闭，模拟 OS 随进程退出回收管道写端
}

func newFakeHandle(pid uint32) *fakeHandle {
	return &fakeHandle{pid: pid, exitCh: make(chan struct{})}
}

// exitWith 模拟进程退场（指定 Wait 错误与退出码），幂等。
// 先回收"随进程关闭"的管道写端再放行 Wait，令泵协程收口时序贴近真机。
func (f *fakeHandle) exitWith(err error, code int) {
	f.mu.Lock()
	f.waitErr = err
	f.code = code
	closers := f.closersOnExit
	f.mu.Unlock()
	for _, c := range closers {
		_ = c.Close()
	}
	select {
	case <-f.exitCh:
	default:
		close(f.exitCh)
	}
}

func (f *fakeHandle) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startErr
}

func (f *fakeHandle) Wait() error {
	f.mu.Lock()
	f.waited = true
	f.mu.Unlock()
	<-f.exitCh
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

func (f *fakeHandle) Kill() error {
	f.mu.Lock()
	f.killed = true
	f.mu.Unlock()
	f.exitWith(errors.New("signal: killed"), 1)
	return nil
}

func (f *fakeHandle) PID() uint32 { return f.pid }

func (f *fakeHandle) ExitCode() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.code
}

func (f *fakeHandle) wasKilled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.killed
}

func (f *fakeHandle) wasWaited() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waited
}

func (f *fakeHandle) StdoutPipe() (io.Reader, error) { return f.stdout, nil }

func (f *fakeHandle) StderrPipe() (io.Reader, error) { return f.stderr, nil }

// closesOnExit 登记退场时需关闭的管道写端（须在 Start 前调用）。
func (f *fakeHandle) closesOnExit(cs ...io.Closer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closersOnExit = append(f.closersOnExit, cs...)
}

type fakeJob struct {
	mu           sync.Mutex
	h            *fakeHandle // Terminate/Close 联动模拟
	assignErr    error
	terminateErr error
	detachErr    error
	killOnClose  bool // Create 默认 true；SetAllowKillOnClose(false) 解除
	assigned     []uint32
	terminated   int
	closed       int
	detachTo     *bool
}

func (j *fakeJob) Assign(pid uint32) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.assignErr != nil {
		return j.assignErr
	}
	j.assigned = append(j.assigned, pid)
	return nil
}

func (j *fakeJob) Close() error {
	j.mu.Lock()
	j.closed++
	kill := j.killOnClose
	h := j.h
	j.mu.Unlock()
	if kill && h != nil {
		h.exitWith(errors.New("exit status 1"), 1) // KILL_ON_JOB_CLOSE 语义模拟
	}
	return nil
}

func (j *fakeJob) Terminate(uint32) error {
	j.mu.Lock()
	if j.terminateErr != nil {
		err := j.terminateErr
		j.mu.Unlock()
		return err
	}
	j.terminated++
	h := j.h
	j.mu.Unlock()
	if h != nil {
		h.exitWith(errors.New("exit status 1"), 1)
	}
	return nil
}

func (j *fakeJob) SetAllowKillOnClose(enabled bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.detachErr != nil {
		return j.detachErr
	}
	j.killOnClose = enabled
	v := enabled
	j.detachTo = &v
	return nil
}

func (j *fakeJob) counts() (assigned, terminated, closed int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.assigned), j.terminated, j.closed
}

// fakeJobAPI 按模板字段孵化 fakeJob（Create 复制种子），便于负路径注入。
type fakeJobAPI struct {
	mu           sync.Mutex
	createErr    error
	assignErr    error
	terminateErr error
	detachErr    error
	h            *fakeHandle
	jobs         []*fakeJob
}

func (f *fakeJobAPI) Create() (platform.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	j := &fakeJob{
		killOnClose:  true,
		h:            f.h,
		assignErr:    f.assignErr,
		terminateErr: f.terminateErr,
		detachErr:    f.detachErr,
	}
	f.jobs = append(f.jobs, j)
	return j, nil
}

func (f *fakeJobAPI) last() *fakeJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.jobs) == 0 {
		return nil
	}
	return f.jobs[len(f.jobs)-1]
}

func (f *fakeJobAPI) created() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.jobs)
}

type fakeProbe struct {
	mu      sync.Mutex
	running bool
	info    *platform.ProcInfo
	err     error
	calls   int
}

func (p *fakeProbe) Inspect(context.Context) (bool, *platform.ProcInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.running, p.info, p.err
}

func (p *fakeProbe) set(running bool, info *platform.ProcInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = running
	p.info = info
	p.err = nil
}

func (p *fakeProbe) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
	p.running = false
}

func (p *fakeProbe) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type fakeProcAPI struct {
	mu        sync.Mutex
	queryInfo platform.ProcInfo
	queryErr  error
	killErr   error
	queries   int
	verified  []platform.VerifyToken // KillVerified 收到的令牌（复核证据）
	h         *fakeHandle            // 复核通过的杀进程联动
}

func (p *fakeProcAPI) Query(uint32) (platform.ProcInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queries++
	if p.queryErr != nil {
		return platform.ProcInfo{}, p.queryErr
	}
	return p.queryInfo, nil
}

func (p *fakeProcAPI) KillVerified(_ context.Context, token platform.VerifyToken, _ bool) error {
	p.mu.Lock()
	p.verified = append(p.verified, token)
	err := p.killErr
	h := p.h
	p.mu.Unlock()
	if err == nil && h != nil {
		h.exitWith(errors.New("exit status 1"), 1) // 复核通过才动手
	}
	return err
}

func (p *fakeProcAPI) IsProtected(uint32, platform.ProcInfo) bool { return false }

func (p *fakeProcAPI) queryCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.queries
}

func (p *fakeProcAPI) verifyCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.verified)
}

func (p *fakeProcAPI) lastToken() platform.VerifyToken {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.verified[len(p.verified)-1]
}

// eventRecorder 记录状态广播与日志泵转发行，便于断言事件形状。
type eventRecorder struct {
	mu    sync.Mutex
	snaps []Snapshot
	logs  []string
}

func (r *eventRecorder) cb() Callbacks {
	return Callbacks{
		OnState: func(s Snapshot) {
			r.mu.Lock()
			r.snaps = append(r.snaps, s)
			r.mu.Unlock()
		},
		OnLog: func(line string) {
			r.mu.Lock()
			r.logs = append(r.logs, line)
			r.mu.Unlock()
		},
	}
}

func (r *eventRecorder) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.logs...)
}

func (r *eventRecorder) logCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.logs)
}

func (r *eventRecorder) states() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]State, 0, len(r.snaps))
	for _, s := range r.snaps {
		out = append(out, s.State)
	}
	return out
}

func (r *eventRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.snaps)
}

// ---------- 测试脚手架 ----------

type harness struct {
	e      *Engine
	h      *fakeHandle
	jobs   *fakeJobAPI
	probe  *fakeProbe
	events *eventRecorder
}

// newHarness 构造全替身引擎：注入 fake opener（单代 handle）、fake JobAPI、fake Probe。
func newHarness(t *testing.T) *harness {
	t.Helper()
	oldPoll := readyPollInterval
	readyPollInterval = 5 * time.Millisecond // 压缩轮询窗口，纯 fake 用例毫秒级完成
	t.Cleanup(func() { readyPollInterval = oldPoll })

	h := newFakeHandle(4242)
	probe := &fakeProbe{}
	jobs := &fakeJobAPI{h: h}
	rec := &eventRecorder{}
	e := NewEngine(jobs, probe, rec.cb())
	e.open = func(Spec) (processHandle, error) { return h, nil }
	return &harness{e: e, h: h, jobs: jobs, probe: probe, events: rec}
}

func waitState(t *testing.T, e *Engine, want State) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := e.Snapshot().State; got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待状态 %s 超时，当前 %s", want, e.Snapshot().State)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---------- Start 主流程与失败分支 ----------

func TestStartSuccess(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(true, &platform.ProcInfo{PID: 4242, ExePath: `C:\tool\tool.exe`})
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`, ReadyTimeout: time.Second}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	snap := hs.e.Snapshot()
	if snap.State != StateRunning {
		t.Fatalf("state = %s, want running", snap.State)
	}
	if !snap.Managed || snap.PID != 4242 || snap.Exe != `C:\tool\tool.exe` {
		t.Fatalf("快照入账异常: %+v", snap)
	}
	job := hs.jobs.last()
	if job == nil {
		t.Fatal("应已创建 Job")
	}
	assigned, terminated, _ := job.counts()
	if assigned != 1 {
		t.Fatalf("Assign 次数 = %d, want 1", assigned)
	}
	if terminated != 0 {
		t.Fatalf("健康启动不应触发 Terminate，次数 = %d", terminated)
	}
	if detached := job.detachToSnapshot(); detached != nil {
		t.Fatalf("未请求 DetachFromJob 时不应触碰 SetAllowKillOnClose, got %v", *detached)
	}
	wantSeq := fmt.Sprint([]State{StateStarting, StateRunning})
	if got := fmt.Sprint(hs.events.states()); got != wantSeq {
		t.Fatalf("事件序列 = %s, want %s", got, wantSeq)
	}
}

func (j *fakeJob) detachToSnapshot() *bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.detachTo == nil {
		return nil
	}
	v := *j.detachTo
	return &v
}

func TestStartNoReadyWait(t *testing.T) {
	hs := newHarness(t) // probe.running 恒 false：ReadyTimeout==0 不等待就绪
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := hs.e.Snapshot().State; got != StateRunning {
		t.Fatalf("state = %s, want running（ReadyTimeout=0 仅 spawn+绑定）", got)
	}
	if calls := hs.probe.callCount(); calls != 0 {
		t.Fatalf("ReadyTimeout=0 不应调用探针，calls = %d", calls)
	}
}

func TestStartDetachFromJob(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`, DetachFromJob: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	detached := hs.jobs.last().detachToSnapshot()
	if detached == nil || *detached != false {
		t.Fatal("DetachFromJob=true 应调用 SetAllowKillOnClose(false)")
	}
}

func TestStartEmptyExeRejected(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Start(context.Background(), Spec{}); err == nil {
		t.Fatal("空 Exe 应报错")
	}
	if got := hs.events.count(); got != 0 {
		t.Fatalf("入参校验失败不应产生事件，count = %d", got)
	}
}

func TestStartOpenerFail(t *testing.T) {
	hs := newHarness(t)
	hs.e.open = func(Spec) (processHandle, error) { return nil, errors.New("boom") }
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err == nil {
		t.Fatal("opener 失败应返回错误")
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartSpawnFail(t *testing.T) {
	hs := newHarness(t)
	hs.h.startErr = errors.New("deny")
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err == nil {
		t.Fatal("handle.Start 失败应返回错误")
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartJobCreateFail(t *testing.T) {
	hs := newHarness(t)
	hs.jobs.createErr = errors.New("boom")
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err == nil {
		t.Fatal("Create 失败应返回错误")
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
	if !hs.h.wasKilled() {
		t.Fatal("绑定失败应终止已拉起进程")
	}
	waitState(t, hs.e, StateFailed) // 收口竞态终局稳定为 failed，不被 wait 回退
}

func TestStartAssignFail(t *testing.T) {
	hs := newHarness(t)
	hs.e.jobs = &fakeJobAPI{assignErr: errors.New("boom"), h: hs.h}
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err == nil {
		t.Fatal("Assign 失败应返回错误")
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
	if !hs.h.wasKilled() {
		t.Fatal("Assign 失败应终止进程")
	}
	waitState(t, hs.e, StateFailed)
}

func TestStartDetachFail(t *testing.T) {
	hs := newHarness(t)
	hs.e.jobs = &fakeJobAPI{detachErr: errors.New("boom"), h: hs.h}
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`, DetachFromJob: true}); err == nil {
		t.Fatal("SetAllowKillOnClose 失败应返回错误")
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
	waitState(t, hs.e, StateFailed)
}

func TestStartReadyTimeout(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(false, nil) // 端口/互斥体永不可用
	start := time.Now()
	err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`, ReadyTimeout: 150 * time.Millisecond})
	if err == nil {
		t.Fatal("就绪超时应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("不应远超 ReadyTimeout，耗时 %v", elapsed)
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
	_, terminated, _ := hs.jobs.last().counts()
	if terminated != 1 {
		t.Fatalf("就绪失败应经 job.Terminate 收口，次数 = %d", terminated)
	}
}

// TestStartAbortOnDeadProcess 就绪等待负路径：进程秒退（蓝本对应 cmd.exe 秒退场景，
// 此处 fake handle 20ms 退场），Start 应在死亡被观察后立即中止，不空等满超时。
func TestStartAbortOnDeadProcess(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(false, nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		hs.h.exitWith(nil, 0)
	}()
	start := time.Now()
	err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`, ReadyTimeout: 30 * time.Second})
	if err == nil {
		t.Fatal("进程死亡应中止启动并报错")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("AbortOnDeadProcess 失效，耗时 %v", elapsed)
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

func TestStartReadyCtxCancel(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := hs.e.Start(ctx, Spec{Exe: `C:\tool\tool.exe`, ReadyTimeout: 30 * time.Second})
	if err == nil {
		t.Fatal("上下文取消应中止启动")
	}
	_, terminated, _ := hs.jobs.last().counts()
	if terminated != 1 {
		t.Fatalf("取消后应终止进程，Terminate 次数 = %d", terminated)
	}
	if got := hs.e.Snapshot().State; got != StateFailed {
		t.Fatalf("state = %s, want failed", got)
	}
}

// ---------- Start 并发/收口纪律 ----------

func TestStartBusyUntilSettled(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); !errors.Is(err, ErrBusy) {
		t.Fatalf("未收口重复启动应 ErrBusy, got %v", err)
	}
	// 自然退场收口后可再次启动
	hs.probe.set(false, nil)
	hs.h.exitWith(nil, 0)
	waitState(t, hs.e, StateStopped)
	hs.e.open = func(Spec) (processHandle, error) { return newFakeHandle(4243), nil }
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("收口后应可重新启动: %v", err)
	}
}

func TestConcurrentStartSerialized(t *testing.T) {
	hs := newHarness(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`})
		}(i)
	}
	wg.Wait()
	ok, busy := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrBusy):
			busy++
		default:
			t.Fatalf("并发 Start 出现意外错误: %v", err)
		}
	}
	if ok != 1 || busy != 1 {
		t.Fatalf("并发 Start 应一成功一 ErrBusy，got ok=%d busy=%d", ok, busy)
	}
	if created := hs.jobs.created(); created != 1 {
		t.Fatalf("串行化失效：Job 创建 %d 次, want 1", created)
	}
}

// ---------- Stop 分类：managed / external / 无 ----------

func TestStopManagedViaJobTerminate(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := hs.e.Stop(0); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	snap := hs.e.Snapshot()
	if snap.State != StateStopped || snap.Managed {
		t.Fatalf("Stop 收口后快照不一致: %+v", snap)
	}
	_, terminated, closed := hs.jobs.last().counts()
	if terminated != 1 {
		t.Fatalf("应 job.Terminate 收口，次数 = %d", terminated)
	}
	if closed != 1 {
		t.Fatalf("wait 收口应 Close Job，次数 = %d", closed)
	}
	seq := fmt.Sprint(hs.events.states())
	if !strings.Contains(seq, string(StateStopping)) || !strings.Contains(seq, string(StateStopped)) {
		t.Fatalf("事件序列应含 stopping 与 stopped: %s", seq)
	}
}

func TestStopGracefulWithinQuitHook(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hs.e.SetQuitHook(func(context.Context) error {
		hs.h.exitWith(nil, 0) // 模拟命令通道投递成功、进程自然退出
		return nil
	})
	if err := hs.e.Stop(2 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	_, terminated, _ := hs.jobs.last().counts()
	if terminated != 0 {
		t.Fatalf("grace 内自然退出不应走强制终止，Terminate 次数 = %d", terminated)
	}
	if hs.h.wasKilled() {
		t.Fatal("grace 内自然退出不应 Kill")
	}
	if got := hs.e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}
}

func TestStopGraceTimeoutFallsBackToKill(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hs.e.SetQuitHook(func(context.Context) error { return nil }) // 投递成功但进程装死
	if err := hs.e.Stop(30 * time.Millisecond); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	_, terminated, _ := hs.jobs.last().counts()
	if terminated != 1 {
		t.Fatalf("grace 超时应兜底 job.Terminate，次数 = %d", terminated)
	}
	waitState(t, hs.e, StateStopped)
}

func TestStopHookErrorGoesStraightToForce(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hs.e.SetQuitHook(func(context.Context) error { return errors.New("管道断开") })
	start := time.Now()
	if err := hs.e.Stop(10 * time.Second); err != nil { // 钩子失败不得消耗 grace
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("钩子失败应立即转强制路径，耗时 %v", elapsed)
	}
	_, terminated, _ := hs.jobs.last().counts()
	if terminated != 1 {
		t.Fatalf("应兜底 job.Terminate，次数 = %d", terminated)
	}
}

func TestStopKillFallbackWithTokenVerify(t *testing.T) {
	hs := newHarness(t)
	proc := &fakeProcAPI{queryInfo: platform.ProcInfo{PID: 4242, ExePath: `C:\tool\tool.exe`}, h: hs.h}
	hs.e.WithProcessAPI(proc)
	hs.jobs.terminateErr = errors.New("terminate denied") // Job 兜底失败 → KillVerified 路径
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if proc.queryCalls() < 1 {
		t.Fatal("注入 ProcessAPI 后 Start 应 Query 建立身份令牌")
	}
	if err := hs.e.Stop(0); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if proc.verifyCalls() != 1 {
		t.Fatalf("兜底强杀应经 KillVerified 复核，calls = %d", proc.verifyCalls())
	}
	if tok := proc.lastToken(); tok.PID != 4242 || tok.ExePath != `C:\tool\tool.exe` {
		t.Fatalf("令牌身份入账错误: %+v", tok)
	}
	if hs.h.wasKilled() {
		t.Fatal("KillVerified 成功路径不应再走句柄 Kill")
	}
	waitState(t, hs.e, StateStopped)
}

func TestStopTokenMismatchRefusesKill(t *testing.T) {
	hs := newHarness(t)
	oldSettle := settleTimeout
	settleTimeout = 300 * time.Millisecond // 拒杀路径进程仍在世，收口等待必然超时
	t.Cleanup(func() { settleTimeout = oldSettle })

	proc := &fakeProcAPI{queryInfo: platform.ProcInfo{PID: 4242, ExePath: `C:\tool\tool.exe`}, h: hs.h}
	hs.e.WithProcessAPI(proc)
	hs.jobs.terminateErr = errors.New("terminate denied")
	proc.killErr = platform.ErrTokenMismatch // 复核认定 PID 已复用
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := hs.e.Stop(0)
	if !errors.Is(err, platform.ErrTokenMismatch) {
		t.Fatalf("身份不符应拒杀上报, got %v", err)
	}
	if hs.h.wasKilled() {
		t.Fatal("复核不通过不得回退句柄 Kill（防误杀陌生进程）")
	}
}

func TestStopExternalRefuses(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(true, &platform.ProcInfo{PID: 99, ExePath: `D:\elsewhere\tool.exe`})
	hs.e.RefreshExternal() // 静止态入账 external
	if got := hs.e.Snapshot().State; got != StateExternal {
		t.Fatalf("state = %s, want external", got)
	}
	err := hs.e.Stop(0)
	if !errors.Is(err, ErrExternal) {
		t.Fatalf("外部实例 Stop 应返回 ErrExternal, got %v", err)
	}
	if hs.h.wasKilled() || hs.h.wasWaited() {
		t.Fatal("外部实例不得被本引擎触碰")
	}
	if hs.jobs.created() != 0 {
		t.Fatal("静止态 Stop 不应创建任何 Job")
	}
	// 探针改口"外部已退出"：Stop 幂等回落 stopped
	hs.probe.set(false, nil)
	if err := hs.e.Stop(0); err != nil {
		t.Fatalf("外部实例消失后 Stop 应幂等, got %v", err)
	}
	if got := hs.e.Snapshot().State; got != StateStopped {
		t.Fatalf("state = %s, want stopped", got)
	}
}

func TestStopIdempotent(t *testing.T) {
	hs := newHarness(t)
	if err := hs.e.Stop(0); err != nil {
		t.Fatalf("Stop(stopped) = %v, want nil", err)
	}
	hs.probe.setErr(errors.New("probe down")) // 探针故障：保守判无，幂等返回
	if err := hs.e.Stop(0); err != nil {
		t.Fatalf("探针失败时 Stop 应保守幂等, got %v", err)
	}
}

// ---------- wait 退出分类（表驱动） ----------

func TestWaitClassification(t *testing.T) {
	cases := []struct {
		name      string
		stopping  bool
		waitErr   error
		code      int
		probeRun  bool
		wantState State
		wantErrIn string
	}{
		{"正常退出码0→stopped", false, nil, 0, false, StateStopped, ""},
		{"非零退出→failed带码", false, errors.New("exit status 3"), 3, false, StateFailed, "退出码 3"},
		{"被第三方终止→failed（蓝本语义：非零码且探针未见存活）", false, errors.New("exit status 1"), 1, false, StateFailed, "退出码 1"},
		{"外部接管：自有退场但探针仍见目标→external", false, nil, 0, true, StateExternal, ""},
		{"手动停止优先：stopping 下非零码且探针见目标也归 stopped", true, errors.New("exit status 1"), 1, true, StateStopped, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := newHarness(t)
			hs.probe.set(false, nil)
			if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
				t.Fatalf("Start: %v", err)
			}
			hs.probe.set(tc.probeRun, nil) // 退出分类探针在 Wait 返回后才读取
			if tc.stopping {
				hs.e.mu.Lock()
				hs.e.stopping = true
				hs.e.mu.Unlock()
			}
			hs.h.exitWith(tc.waitErr, tc.code)
			waitState(t, hs.e, tc.wantState)
			snap := hs.e.Snapshot()
			if tc.wantErrIn != "" && !strings.Contains(snap.Error, tc.wantErrIn) {
				t.Fatalf("failed 应携带原因 %q, got %q", tc.wantErrIn, snap.Error)
			}
			if snap.Managed {
				t.Fatalf("收口后 Managed 应为 false: %+v", snap)
			}
			if tc.wantState == StateExternal && snap.PID != 0 {
				t.Fatalf("external 无探针 PID 时应为 0: %+v", snap)
			}
			if tc.wantState == StateStopped && !settledNow(hs.e) {
				t.Fatal("收口后应可再次启动（done 已清）")
			}
		})
	}
}

func settledNow(e *Engine) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.done == nil
}

// ---------- RefreshExternal 校正 ----------

func TestRefreshExternalTwoWay(t *testing.T) {
	hs := newHarness(t)

	// 静止且目标不在：无变化不广播
	hs.e.RefreshExternal()
	if got := hs.events.count(); got != 0 {
		t.Fatalf("无变化时不应广播，事件数 = %d", got)
	}

	hs.probe.set(true, &platform.ProcInfo{PID: 77, ExePath: `D:\x\tool.exe`, StartedAt: time.Now()})
	hs.e.RefreshExternal()
	snap := hs.e.Snapshot()
	if snap.State != StateExternal || snap.PID != 77 || snap.Managed {
		t.Fatalf("应入账 external 并采纳探针 PID: %+v", snap)
	}

	hs.probe.set(false, nil)
	hs.e.RefreshExternal()
	snap = hs.e.Snapshot()
	if snap.State != StateStopped || snap.PID != 0 {
		t.Fatalf("external 退场应回落 stopped 并清零 PID: %+v", snap)
	}

	// 运行态不探测（探测到的正是自己）
	hs.probe.set(true, nil)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	before := hs.events.count()
	hs.e.RefreshExternal()
	if got := hs.e.Snapshot().State; got != StateRunning {
		t.Fatalf("running 不应被 RefreshExternal 改写，state = %s", got)
	}
	if hs.events.count() != before {
		t.Fatal("running 守卫下不应产生事件")
	}
}

func TestRefreshExternalProbeErrorKeepsState(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(true, nil)
	hs.e.RefreshExternal()
	hs.probe.setErr(errors.New("probe boom"))
	hs.e.RefreshExternal()
	if got := hs.e.Snapshot().State; got != StateExternal {
		t.Fatalf("探针瞬时失败不应改变状态, got %s", got)
	}
}

// ---------- Ownership ----------

func TestOwnership(t *testing.T) {
	hs := newHarness(t)
	if got := hs.e.Ownership(context.Background()); got != OwnNone {
		t.Fatalf("静止无实例应 OwnNone, got %s", got)
	}
	hs.probe.set(true, nil)
	if got := hs.e.Ownership(context.Background()); got != OwnExternal {
		t.Fatalf("探针命中应 OwnExternal, got %s", got)
	}
	hs.probe.set(false, nil)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hs.probe.set(true, nil) // 探针同时命中也不改判：自有句柄优先
	if got := hs.e.Ownership(context.Background()); got != OwnManaged {
		t.Fatalf("受管进程在世应 OwnManaged, got %s", got)
	}
	hs.probe.setErr(errors.New("boom")) // 探针失败 + 自有进程在世：仍 OwnManaged
	if got := hs.e.Ownership(context.Background()); got != OwnManaged {
		t.Fatalf("OwnManaged 应无视探针故障, got %s", got)
	}
}

// ---------- 收口后快照一致性 ----------

func TestSnapshotConsistentAfterRelease(t *testing.T) {
	hs := newHarness(t)
	hs.probe.set(true, &platform.ProcInfo{PID: 4242})
	if err := hs.e.Start(context.Background(), Spec{Version: "v1.2.3", Exe: `C:\tool\tool.exe`, ReadyTimeout: time.Second}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hs.probe.set(false, nil) // 自然退出后目标消失
	hs.h.exitWith(nil, 0)
	waitState(t, hs.e, StateStopped)
	snap := hs.e.Snapshot()
	if snap.Version != "v1.2.3" {
		t.Fatalf("Version 应保留最近启动绑定: %+v", snap)
	}
	if snap.PID != 4242 || snap.Exe != `C:\tool\tool.exe` {
		t.Fatalf("stopped 应保留最后受管身份供诊断: %+v", snap)
	}
	if snap.Managed {
		t.Fatal("收口后 Managed 必为 false")
	}
	if snap.Since.IsZero() || snap.Since.After(time.Now()) {
		t.Fatalf("Since 应为已发生的启动时刻: %v", snap.Since)
	}
	// 收口后可再次 Start（release 语义）
	hs.e.open = func(Spec) (processHandle, error) { return newFakeHandle(4243), nil }
	if err := hs.e.Start(context.Background(), Spec{Version: "v2", Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("收口后重新启动: %v", err)
	}
	if v := hs.e.Snapshot().Version; v != "v2" {
		t.Fatalf("Version 未随新 Start 更新: %s", v)
	}
}

// ---------- Spec 扩展字段透传 / 日志泵（fake 管道注入） ----------

// TestStartPassesExtFieldsToOpener HideWindow/Env 在 fake 链路只断言原样透传给
// opener（真实注入行为由 proc_test.go / child_windows_test.go 断言）。
func TestStartPassesExtFieldsToOpener(t *testing.T) {
	hs := newHarness(t)
	var got Spec
	hs.e.open = func(s Spec) (processHandle, error) { got = s; return hs.h, nil }
	if err := hs.e.Start(context.Background(), Spec{
		Exe:        `C:\tool\tool.exe`,
		HideWindow: true,
		Env:        []string{"DDNS_GO_DAEMON=1"},
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !got.HideWindow {
		t.Fatal("HideWindow 应透传给 opener")
	}
	if len(got.Env) != 1 || got.Env[0] != "DDNS_GO_DAEMON=1" {
		t.Fatalf("Env 应原样透传给 opener, got %v", got.Env)
	}
	if got.WorkingDir == "" {
		t.Fatal("WorkingDir 缺省锁定应在传给 opener 前完成")
	}
}

func TestOnLogPumpsAggregateAndSettle(t *testing.T) {
	hs := newHarness(t)
	hs.h.stdout = strings.NewReader("o1\no2\n")
	hs.h.stderr = strings.NewReader("e1\n")
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n := hs.e.pumpStarted.Load(); n != 2 {
		t.Fatalf("stdout/stderr 应各起一条泵协程, started = %d", n)
	}
	// 两路聚合断言（跨管道行序无关，按排序多重集比较），并等待泵收口
	want := []string{"e1", "o1", "o2"}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if hs.e.pumpFinished.Load() == 2 && reflect.DeepEqual(sortedLines(hs.events.lines()), want) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("聚合或收口未达成: finished=%d lines=%v", hs.e.pumpFinished.Load(), hs.events.lines())
		}
		time.Sleep(5 * time.Millisecond)
	}
	hs.h.exitWith(nil, 0)
	waitState(t, hs.e, StateStopped)
}

func TestOnLogPumpsSettleAfterProcessExit(t *testing.T) {
	hs := newHarness(t)
	// io.Pipe 写端不关则读端不 EOF：泵收口必须晚于进程退场（模拟真机句柄语义）
	pr, pw := io.Pipe()
	qr, qw := io.Pipe()
	hs.h.stdout, hs.h.stderr = pr, qr
	hs.h.closesOnExit(pw, qw)
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := pw.Write([]byte("alive-1\nalive-2\n")); err != nil {
		t.Fatalf("写 stdout 管道: %v", err)
	}
	if _, err := qw.Write([]byte("alive-e1\n")); err != nil {
		t.Fatalf("写 stderr 管道: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for hs.events.logCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := hs.events.logCount(); got != 3 {
		t.Fatalf("进程在世时应聚合到 3 行, got %d (%v)", got, hs.events.lines())
	}
	if n := hs.e.pumpFinished.Load(); n != 0 {
		t.Fatalf("进程在世时泵协程不应收口, finished = %d", n)
	}
	hs.h.exitWith(nil, 0)
	waitState(t, hs.e, StateStopped)
	if n := hs.e.pumpFinished.Load(); n != 2 {
		t.Fatalf("进程退出后两条泵协程应收口（不泄露）, finished = %d", n)
	}
}

func TestOnLogScannerErrSurfaces(t *testing.T) {
	hs := newHarness(t)
	pr, pw := io.Pipe()
	hs.h.stdout = pr // stderr 不注入：应只起一路泵
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n := hs.e.pumpStarted.Load(); n != 1 {
		t.Fatalf("仅 stdout 可得时应只起一路泵, started = %d", n)
	}
	if _, err := pw.Write([]byte("last-good\n")); err != nil {
		t.Fatalf("写管道: %v", err)
	}
	_ = pw.CloseWithError(io.ErrUnexpectedEOF) // 半路中断：scanner 扫完必查 Err 的口径落点
	waitPumpsSettled(t, hs.e, 1)
	joined := strings.Join(hs.events.lines(), "\n")
	if !strings.Contains(joined, "last-good") || !strings.Contains(joined, "读取中断") {
		t.Fatalf("读取中断应转为可见日志行, got %v", hs.events.lines())
	}
	hs.h.exitWith(nil, 0)
	waitState(t, hs.e, StateStopped)
}

func TestOnLogNilKeepsPipes(t *testing.T) {
	hs := newHarness(t)
	hs.e.cb = Callbacks{} // 未注册 OnLog：不获取管道、零泵协程
	hs.h.stdout = strings.NewReader("unused\n")
	hs.h.stderr = strings.NewReader("unused\n")
	if err := hs.e.Start(context.Background(), Spec{Exe: `C:\tool\tool.exe`}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n := hs.e.pumpStarted.Load(); n != 0 {
		t.Fatalf("OnLog==nil 不应起泵协程, started = %d", n)
	}
	hs.h.exitWith(nil, 0)
	waitState(t, hs.e, StateStopped)
}

func sortedLines(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// waitPumpsSettled 等待泵收口计数追平指定增量（基线由调用方先取）。
func waitPumpsSettled(t *testing.T, e *Engine, wantDelta int64) {
	t.Helper()
	base := e.pumpFinished.Load()
	deadline := time.Now().Add(10 * time.Second)
	for e.pumpFinished.Load() < base+wantDelta {
		if time.Now().After(deadline) {
			t.Fatalf("泵协程收口超时: finished=%d, want +%d", e.pumpFinished.Load(), wantDelta)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---------- 真机冒烟（LookPath + SystemRoot 兜底；环境不可得时按仓库惯例落环境红，CI 覆盖） ----------

// mustCmdExePath 返回系统 cmd.exe：PATH 精简的会话（如 Git Bash）不含 System32，
// 经 SystemRoot 定位兜底，仍是真实系统 cmd.exe，不改变冒烟口径。
func mustCmdExePath(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("cmd.exe"); err == nil {
		return path
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		path := filepath.Join(root, "System32", "cmd.exe")
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path
		}
	}
	t.Fatal("cmd.exe 不可用")
	return ""
}

// TestRealCmdExeSpawnSmoke 验证 defaultOpener 真实链路：cmd.exe 短命进程
// spawn→绑定（Job 仍为替身，聚焦 opener/wait）→running→真实退出→退出码 0 归 stopped。
func TestRealCmdExeSpawnSmoke(t *testing.T) {
	exe := mustCmdExePath(t)
	oldPoll, oldSettle := readyPollInterval, settleTimeout
	readyPollInterval, settleTimeout = 10*time.Millisecond, 15*time.Second
	t.Cleanup(func() { readyPollInterval, settleTimeout = oldPoll, oldSettle })

	probe := &fakeProbe{} // 探针保持 not-running：短命进程走退出码分类
	jobs := &fakeJobAPI{} // Job 保持替身：不引入真实 JobObject 联动，聚焦 opener 与 wait 分类
	var logMu sync.Mutex
	var logLines []string
	e := NewEngine(jobs, probe, Callbacks{OnLog: func(line string) {
		logMu.Lock()
		logLines = append(logLines, line)
		logMu.Unlock()
	}})
	if err := e.Start(context.Background(), Spec{
		Exe: exe,
		// echo 而非 ping：退出码恒 0 且有一行 stdout，不依赖网络/沙箱策略
		// （受限环境里 ping 非零退出会把 stopped 期望正确地分类成 failed，
		// 那是分类在工作，不是内核缺陷）。
		Args: []string{"/c", "echo", "supervisor-smoke"},
	}); err != nil {
		t.Fatalf("真机 Start: %v", err)
	}
	if got := e.Snapshot().State; got != StateRunning {
		t.Fatalf("真机启动后 state = %s, want running", got)
	}
	waitState(t, e, StateStopped) // echo 退出码 0 → stopped
	if got := e.Snapshot().PID; got == 0 {
		t.Fatal("真机 PID 应已入账")
	}
	if jobs.created() != 1 {
		t.Fatalf("应创建 1 个 Job，got %d", jobs.created())
	}
	// 日志泵真机口径：两路泵协程随进程退出收口（不泄露），ping 回显至少一行
	if n := e.pumpStarted.Load(); n != 2 {
		t.Fatalf("应起两路日志泵, started = %d", n)
	}
	// 短命进程秒退,泵可能已先于本行收口——按绝对值(追平 started)等待,
	// 不用 waitPumpsSettled 的"调用时取基线+增量"形态(基线会被竞态吃掉)。
	deadline := time.Now().Add(10 * time.Second)
	for e.pumpFinished.Load() < e.pumpStarted.Load() {
		if time.Now().After(deadline) {
			t.Fatalf("泵协程收口超时: finished=%d, started=%d", e.pumpFinished.Load(), e.pumpStarted.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	logMu.Lock()
	defer logMu.Unlock()
	if len(logLines) == 0 {
		t.Fatal("真机 stdout/stderr 应至少转发一行")
	}
}
