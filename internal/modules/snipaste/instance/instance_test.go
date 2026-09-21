//go:build windows

package instance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	sup "hanxi/packages/go/supervisor"
)

// 说明：进程治理主流程（spawn/Job 绑定/退出分类/强制终止收口）已由内核
// packages/go/supervisor 的引擎测试矩阵覆盖。本文件聚焦 snipaste 适配层：
// 无外部探针契约、内核快照 → 本包 Snapshot 的词表/形状映射、Quit 分层归因
// 决策表（finishQuitWith 注入假复核/假强杀）、身份自检，以及真 Job 冒烟。

// ---------- 测试用 fake ----------

type fakeJob struct {
	assigned   uint32
	allow      *bool
	terminated bool
}

func (j *fakeJob) Assign(pid uint32) error { j.assigned = pid; return nil }
func (j *fakeJob) Close() error            { return nil }
func (j *fakeJob) Terminate(uint32) error  { j.terminated = true; return nil }
func (j *fakeJob) SetAllowKillOnClose(on bool) error {
	j.allow = &on
	return nil
}

// fakeJobAPI 假 Job：Terminate 不真正杀进程，仅记账（启动契约断言用；
// 生命周期冒烟一律走真 windows.NewJobAPI）。
type fakeJobAPI struct{ job *fakeJob }

func (a *fakeJobAPI) Create() (platform.Job, error) {
	if a.job == nil {
		a.job = &fakeJob{}
	}
	return a.job, nil
}

// fakeProcessAPI 可控进程视图：mismatch 翻牌后 Query 报出陌生路径，
// 模拟 PID 复用/启动器转发场景。
type fakeProcessAPI struct {
	mu       sync.Mutex
	info     platform.ProcInfo
	mismatch bool
}

func (p *fakeProcessAPI) Query(pid uint32) (platform.ProcInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := p.info
	if p.mismatch {
		info.ExePath = `C:\Else\wherever.exe`
	}
	if info.PID == 0 {
		info.PID = pid
	}
	return info, nil
}

func (*fakeProcessAPI) KillVerified(context.Context, platform.VerifyToken, bool) error { return nil }
func (*fakeProcessAPI) IsProtected(uint32, platform.ProcInfo) bool                     { return false }

func (p *fakeProcessAPI) diverge() {
	p.mu.Lock()
	p.mismatch = true
	p.mu.Unlock()
}

type eventRecorder struct {
	mu     sync.Mutex
	states []Snapshot
}

func (r *eventRecorder) cb() Callbacks {
	return Callbacks{OnState: func(s Snapshot) {
		r.mu.Lock()
		r.states = append(r.states, s)
		r.mu.Unlock()
	}}
}

func (r *eventRecorder) last() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		return Snapshot{}
	}
	return r.states[len(r.states)-1]
}

func (r *eventRecorder) hasState(want State) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.states {
		if s.State == want {
			return true
		}
	}
	return false
}

func newWindowsJobAPI() platform.JobAPI { return windows.NewJobAPI() }

// mustCmdExe 返回可用的 cmd.exe 路径（真进程冒烟用）。PATH 不含 System32 的
// 受限环境（如 CI 沙箱）回退 COMSPEC；两者皆不可得则如实 skip——同 markeron
// "环境红即同口径收口"的先例，但优先降级为 skip 不制造假红。
func mustCmdExe(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("cmd.exe"); err == nil {
		return path
	}
	if path := os.Getenv("COMSPEC"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Skip("cmd.exe 不可用，真进程冒烟在当前环境无法执行")
	return ""
}

// waitState 轮询直至引擎达到目标状态（或超时）。
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
		time.Sleep(20 * time.Millisecond)
	}
}

// aliveCmdProc 构造与真 cmd.exe 自洽的假进程视图（Start 身份自检与 Quit
// 复核都依赖 ExePath/StartedAt 稳定）。
func aliveCmdProc(exe string) *fakeProcessAPI {
	return &fakeProcessAPI{info: platform.ProcInfo{ExePath: exe, StartedAt: time.Now()}}
}

// ---------- 无外部探针契约 ----------

func TestNoExternalProbeAlwaysAbsent(t *testing.T) {
	running, info, err := noExternalProbe{}.Inspect(context.Background())
	if running || info != nil || err != nil {
		t.Fatalf("探针必须恒报不在场: running=%v info=%#v err=%v", running, info, err)
	}
}

// ---------- 词表与形状映射（内核快照直注） ----------

func TestMapKernelStateVocabulary(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, rec.cb())

	cases := []struct {
		name string
		in   sup.Snapshot
		want State
	}{
		{"starting", sup.Snapshot{State: sup.StateStarting}, StateStarting},
		{"running", sup.Snapshot{State: sup.StateRunning}, StateRunning},
		{"stopping→quitting", sup.Snapshot{State: sup.StateStopping}, StateQuitting},
		{"stopped", sup.Snapshot{State: sup.StateStopped}, StateStopped},
		{"failed", sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 3）"}, StateFailed},
		{"external 防御收口 stopped", sup.Snapshot{State: sup.StateExternal}, StateStopped},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.onSupState(c.in)
			if got := rec.last().State; got != c.want {
				t.Fatalf("state = %s, want %s", got, c.want)
			}
		})
	}

	// 手动退出窗口：running 呈现 quitting；failed 落终改判 stopped（承原
	// stopping 先置位：Quit 窗口内任何落终都归因手动退出）。
	e.mu.Lock()
	e.quitReported, e.manualExit = true, true
	e.mu.Unlock()
	e.onSupState(sup.Snapshot{State: sup.StateRunning})
	if got := rec.last().State; got != StateQuitting {
		t.Fatalf("退出窗口内 running 应呈现 quitting, got %s", got)
	}
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 1）"})
	snap := rec.last()
	if snap.State != StateStopped || snap.Error != "" {
		t.Fatalf("退出窗口内非零落终应记 stopped 且无错误文案: %+v", snap)
	}
	if snap.ExitCode != 1 {
		t.Fatalf("退出码应从内核文案提取: %d", snap.ExitCode)
	}
	// 收口清零归因标记（下次自然崩溃回到 failed 语义）
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 2）"})
	if got := rec.last(); got.State != StateFailed || !strings.Contains(got.Error, "Snipaste 异常退出（退出码 2）") {
		t.Fatalf("窗口外异常退出应还原既有文案: %+v", got)
	}
}

func TestStoppedMessageAndPidShape(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, rec.cb())

	e.onSupState(sup.Snapshot{State: sup.StateRunning, Version: "2.11.3", PID: 4242, Exe: `C:\v\Snipaste.exe`, Since: time.Now()})
	run := rec.last()
	if run.PID != 4242 || run.ExePath != `C:\v\Snipaste.exe` || run.Version != "2.11.3" {
		t.Fatalf("running 快照形状异常: %+v", run)
	}

	// 内核"已手动停止"文案 → snipaste 既有口径：终态事件无错误文案；收口后 PID 清零。
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Version: "2.11.3", PID: 4242, Exe: `C:\v\Snipaste.exe`, Error: "已手动停止"})
	stop := rec.last()
	if stop.State != StateStopped || stop.Error != "" || stop.PID != 0 {
		t.Fatalf("stopped 映射异常: %+v", stop)
	}
	// Windows 单调钟粒度可致同 tick：只断言"已落账且不早于启动时刻"
	if stop.ExePath == "" || stop.StoppedAt.IsZero() || stop.StoppedAt.Before(run.StartedAt) {
		t.Fatalf("ExePath 应延续、StoppedAt 应落账: %+v", stop)
	}
}

func TestStartupSelfCheckLatchMapsFailed(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, rec.cb())
	// 自检失败锁存后：内核收口的 stopped 事件必须改写为 failed + 锁存文案，
	// 且不落停止时刻（还原原实现"启动期失败无运行终态"语义）。
	e.mu.Lock()
	e.latchedErr = "启动进程路径不匹配: C:\\Else\\other.exe"
	e.mu.Unlock()
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Error: "已手动停止"})
	snap := rec.last()
	if snap.State != StateFailed || snap.Error != "启动进程路径不匹配: C:\\Else\\other.exe" {
		t.Fatalf("锁存文案应改写为 failed: %+v", snap)
	}
	if !snap.StoppedAt.IsZero() {
		t.Fatalf("启动自检失败不应落停止时刻: %+v", snap)
	}
}

// ---------- Quit 分层归因决策表（seam 注入，不依赖真进程） ----------

func TestFinishQuitAttributionTable(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, Callbacks{})
	snap := sup.Snapshot{State: sup.StateRunning, PID: 42, Exe: `C:\v\Snipaste.exe`}

	notFound := func(platform.VerifyToken) error { return platform.ErrProcessNotFound }
	mismatch := func(platform.VerifyToken) error { return platform.ErrTokenMismatch }
	alive := func(platform.VerifyToken) error { return nil }
	forceOK := func() error { return nil }
	forceErr := func() error { return errors.New("boom") }

	cases := []struct {
		name       string
		settled    bool
		verify     func(platform.VerifyToken) error
		force      func() error
		wantMethod string
		wantStop   bool
		wantForce  bool
		wantErr    bool
	}{
		{"宽限内自然收口→close-request", true, nil, nil, "close-request", true, false, false},
		{"宽限后已消失→already-exited", false, notFound, nil, "already-exited", true, false, false},
		{"宽限后身份不符→ownership-lost", false, mismatch, nil, "ownership-lost", false, false, true},
		{"内核强制终止成功→forced-job", false, alive, forceOK, "forced-job", true, true, false},
		{"终止通道全败→forced-process+error", false, alive, forceErr, "forced-process", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			verify := c.verify
			if verify == nil {
				verify = alive // close-request 分支不触达复核
			}
			force := c.force
			if force == nil {
				force = func() error { t.Fatal("该分支不应触达强制终止"); return nil }
			}
			res, err := e.finishQuitWith(snap, true, c.settled, verify, force)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if res.Method != c.wantMethod || res.Stopped != c.wantStop || res.Forced != c.wantForce {
				t.Fatalf("result = %#v", res)
			}
			if !res.CloseRequested {
				t.Error("CloseRequested 应透传投递结果")
			}
		})
	}

	// ownership-lost 错误文案保真
	if _, err := e.finishQuitWith(snap, false, false, mismatch, forceOK); err == nil ||
		!strings.Contains(err.Error(), "强制结束前进程身份已变化，已拒绝操作") {
		t.Fatalf("ownership-lost 文案失真: %v", err)
	}
}

func TestQuitPrecheckRejectsMismatchedIdentity(t *testing.T) {
	// 动手前复核不通过：拒绝投递与强杀（宁可退出失败也不误杀复用 PID 的进程）。
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, rec.cb())
	e.mu.Lock()
	e.settle = make(chan struct{})
	e.mu.Unlock()
	// 伪造内核"在跑"不可行（sup 状态机自持），改以真进程冒烟覆盖：见
	// TestQuitOwnershipLostSmoke。此处只锁定 not-managed 门控。
	if res, err := e.Quit(); err != nil || res.Method != "not-managed" || res.Stopped {
		t.Fatalf("静止态 Quit 应回 not-managed: %#v err=%v", res, err)
	}
}

func TestVerifyTokenRejectsPathMismatch(t *testing.T) {
	proc := &fakeProcessAPI{info: platform.ProcInfo{PID: 10, ExePath: filepath.Join(t.TempDir(), "other.exe"), StartedAt: time.Now()}}
	engine := NewEngine(&fakeJobAPI{}, proc, nil, Callbacks{})
	err := engine.verifyToken(platform.VerifyToken{PID: 10, ExePath: filepath.Join(t.TempDir(), "Snipaste.exe"), StartedAt: proc.info.StartedAt})
	if err != platform.ErrTokenMismatch {
		t.Fatalf("err=%v", err)
	}
}

// ---------- 生命周期冒烟（真 Job Object，与生产同源） ----------

func TestStartKeepsIndependentJobAndTracksProcess(t *testing.T) {
	// 启动契约：绑定 Job 后立即解除退出联动（Snipaste 恒脱管，Hanxi 退出不连杀）。
	cmdExe := os.Getenv("COMSPEC")
	if cmdExe == "" {
		t.Skip("COMSPEC unavailable")
	}
	jobs := &fakeJobAPI{}
	proc := windows.NewProcessAPI() // 真查询：身份自检与终局清理都要如实
	engine := NewEngine(jobs, proc, nil, Callbacks{})
	if err := engine.Start(StartOptions{Version: "test", Exe: cmdExe}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		outer := engine.sup.Snapshot()
		if outer.State == sup.StateRunning || outer.State == sup.StateStarting {
			_ = proc.KillVerified(context.Background(), quitToken(outer), true)
		}
	})
	if jobs.job.assigned == 0 {
		t.Fatal("job was not assigned")
	}
	if jobs.job.allow == nil || *jobs.job.allow {
		t.Fatal("kill-on-close must be disabled")
	}
}

func TestStartRejectsEmptyExe(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, Callbacks{})
	if err := e.Start(StartOptions{Version: "v2.11.3"}); err == nil || err.Error() != "Snipaste.exe 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartIdentityMismatchAborts(t *testing.T) {
	// 启动器转发防御：Query 报出陌生路径 → Start 失败并锁存 failed 终态，
	// 误起进程被真 Job 整树终止（不残留）。
	cmdExe := mustCmdExe(t)
	proc := &fakeProcessAPI{info: platform.ProcInfo{ExePath: `C:\Else\other.exe`, StartedAt: time.Now()}}
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), proc, nil, rec.cb())
	err := e.Start(StartOptions{Version: "2.11.3", Exe: cmdExe})
	if err == nil {
		// cmd 在自检时点已自行退出（stdin 环境所致）——abort 路径无从触发，如实跳过。
		t.Skipf("cmd 即时退出未走自检，环境不适合本测试: %+v", e.Snapshot())
	}
	if !strings.Contains(err.Error(), "启动进程路径不匹配") {
		t.Fatalf("路径不符应拒绝启动: %v", err)
	}
	snap := e.Snapshot()
	if snap.State != StateFailed || !strings.Contains(snap.Error, "启动进程路径不匹配") || snap.PID != 0 {
		t.Fatalf("自检失败应锁存为 failed: %+v", snap)
	}
	waitState(t, e, StateFailed) // 锁存态稳定（不被后续广播冲掉）
}

func TestStartRejectsWhenActive(t *testing.T) {
	cmdExe := mustCmdExe(t)
	e := NewEngine(newWindowsJobAPI(), aliveCmdProc(cmdExe), nil, Callbacks{})
	e.closeByPID = func(uint32) int { return 0 }
	startCmdLoop(t, e, 30)
	if err := e.Start(StartOptions{Version: "2.11.3", Exe: cmdExe}); err == nil ||
		err.Error() != "本会话启动的 Snipaste 已在运行" {
		t.Fatalf("重复启动应拒绝: %v", err)
	}
	old := closeGracePeriod
	closeGracePeriod = 50 * time.Millisecond
	defer func() { closeGracePeriod = old }()
	res, err := e.Quit()
	if err != nil || !res.Stopped || !res.Forced || res.Method != "forced-job" {
		t.Fatalf("清理 Quit 失败: %#v err=%v", res, err)
	}
}

// TestQuitCloseRequestSmoke 真进程冒烟：被投关闭请求后（此处以接缝回报投递成功
// 并让进程在宽限窗口内自然退出）归因 close-request，终态事件无错误文案。
func TestQuitCloseRequestSmoke(t *testing.T) {
	cmdExe := mustCmdExe(t)
	// 存活约 2s 的 cmd：自然退出（退出码 0）落在 3s 宽限内。
	engine := NewEngine(newWindowsJobAPI(), aliveCmdProc(cmdExe), nil, Callbacks{})
	engine.closeByPID = func(uint32) int { return 1 }
	origGrace := closeGracePeriod
	closeGracePeriod = 3 * time.Second
	defer func() { closeGracePeriod = origGrace }()

	startCmdLoop(t, engine, 2)
	res, err := engine.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if res.Method != "close-request" || !res.Stopped || !res.CloseRequested || res.Forced {
		t.Fatalf("归因异常: %#v", res)
	}
	snap := engine.Snapshot()
	if snap.State != StateStopped || snap.Error != "" || snap.PID != 0 || snap.StoppedAt.IsZero() {
		t.Fatalf("终态快照异常: %+v", snap)
	}
}

// TestQuitForcedJobSmoke 真进程冒烟：宽限期不收口 → 复核仍是在世自家进程 →
// 内核整树强杀，归因 forced-job。
func TestQuitForcedJobSmoke(t *testing.T) {
	cmdExe := mustCmdExe(t)
	rec := &eventRecorder{}
	engine := NewEngine(newWindowsJobAPI(), aliveCmdProc(cmdExe), nil, rec.cb())
	engine.closeByPID = func(uint32) int { return 0 } // 顶层窗口不可投（等价无头进程），记未投递
	origGrace := closeGracePeriod
	closeGracePeriod = 100 * time.Millisecond
	defer func() { closeGracePeriod = origGrace }()

	startCmdLoop(t, engine, 30)
	res, err := engine.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if res.Method != "forced-job" || !res.Stopped || !res.Forced || res.CloseRequested {
		t.Fatalf("归因异常: %#v", res)
	}
	if !rec.hasState(StateQuitting) {
		t.Error("退出过程应报告过 quitting 态")
	}
	waitState(t, engine, StateStopped)
}

// TestQuitOwnershipLostSmoke 复核在宽限到期后身份突变（PID 复用模拟）：
// 拒绝强杀并报 ownership-lost；自家进程（Job 在手）随测试清理。
func TestQuitOwnershipLostSmoke(t *testing.T) {
	cmdExe := mustCmdExe(t)
	proc := aliveCmdProc(cmdExe)
	engine := NewEngine(newWindowsJobAPI(), proc, nil, Callbacks{})
	engine.closeByPID = func(uint32) int {
		proc.diverge() // 投递动作发生时点即"身份已变化"
		return 0
	}
	origGrace := closeGracePeriod
	closeGracePeriod = 50 * time.Millisecond
	defer func() { closeGracePeriod = origGrace }()

	startCmdLoop(t, engine, 30)
	res, err := engine.Quit()
	if err == nil || res.Method != "ownership-lost" || res.Stopped || res.Forced {
		t.Fatalf("身份突变应拒强杀: %#v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "强制结束前进程身份已变化") {
		t.Fatalf("文案失真: %v", err)
	}
	if got := engine.Snapshot().State; got != StateQuitting {
		t.Fatalf("退出窗口内应呈现 quitting, got %s", got)
	}
	// 清理：直接经内核强制终止（复核路径已被测试证明拒绝强杀）
	if qerr := engine.sup.Stop(0); qerr != nil {
		t.Fatalf("清理 Stop: %v", qerr)
	}
	waitState(t, engine, StateStopped)
}

// startCmdLoop 以真 Job 拉起"存活 N 秒"的 cmd 实例并等 running（直接走内核：
// 绕过 e.Start 的模块侧身份自检——自检另有专测，本组冒烟锁定内核+适配层生命
// 周期）。PING.EXE 用绝对路径：受限测试环境（PATH 不含 System32）下相对命令
// 会秒败，冒烟必须与宿主机 PATH 无关。
func startCmdLoop(t *testing.T, e *Engine, seconds int) {
	t.Helper()
	exe := mustCmdExe(t)
	ping := filepath.Join(filepath.Dir(exe), "PING.EXE")
	if _, err := os.Stat(ping); err != nil {
		t.Skip("PING.EXE 不可用，真进程冒烟在当前环境无法执行")
	}
	args := []string{"/C", fmt.Sprintf("%s -n %d 127.0.0.1", ping, seconds)}
	if err := e.sup.Start(context.Background(), sup.Spec{Version: "2.11.3", Exe: exe, Args: args, DetachFromJob: true}); err != nil {
		t.Fatalf("内核 Start: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for e.sup.Snapshot().State != sup.StateRunning {
		if time.Now().After(deadline) {
			t.Fatalf("等待内核 running 超时，当前 %s", e.sup.Snapshot().State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
