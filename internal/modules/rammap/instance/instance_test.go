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
	"syscall"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	"hanxi/packages/go/externalquit"
	sup "hanxi/packages/go/supervisor"
)

// 说明：进程治理主流程由内核 supervisor 测试矩阵覆盖。本文件聚焦 rammap
// 适配层：探针兼容兜底契约、内核快照 → 本包 Snapshot 词表映射、Quit 分层
// 归因决策表、QuitExternal 的 confirm-force 档门控，以及真 Job 冒烟。

// ---------- 测试用替身 ----------

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

type fakeJobAPI struct{ job *fakeJob }

func (a *fakeJobAPI) Create() (platform.Job, error) {
	if a.job == nil {
		a.job = &fakeJob{}
	}
	return a.job, nil
}

// fakeProcessAPI 可控进程视图：mismatch 翻牌后 Query 报出陌生路径（PID 复用
// 模拟）；gone 翻牌后 Query 报"进程不存在"；killErr 定制 KillVerified 回执。
type fakeProcessAPI struct {
	mu       sync.Mutex
	info     platform.ProcInfo
	mismatch bool
	gone     bool
	killErr  error
}

func (p *fakeProcessAPI) Query(pid uint32) (platform.ProcInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gone {
		return platform.ProcInfo{}, platform.ErrProcessNotFound
	}
	info := p.info
	if p.mismatch {
		info.ExePath = `C:\Else\wherever.exe`
	}
	if info.PID == 0 {
		info.PID = pid
	}
	return info, nil
}

func (p *fakeProcessAPI) KillVerified(context.Context, platform.VerifyToken, bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killErr != nil {
		return p.killErr
	}
	p.gone = true
	return nil
}

func (*fakeProcessAPI) IsProtected(uint32, platform.ProcInfo) bool { return false }

func (p *fakeProcessAPI) diverge() {
	p.mu.Lock()
	p.mismatch = true
	p.mu.Unlock()
}

// fakeProbe 可编程探针：alive 在场判据默认由注入的 info 决定；绑定 proc 后
// 与进程替身联动（KillVerified 落 gone 即"探针看不见了"），如实回放
// "强杀成功 → 复探撤销 external → stopped"的收口链。
type fakeProbe struct {
	mu      sync.Mutex
	running bool
	info    *platform.ProcInfo
	proc    *fakeProcessAPI
}

func (fp *fakeProbe) set(running bool, info *platform.ProcInfo) {
	fp.mu.Lock()
	fp.running, fp.info = running, info
	fp.mu.Unlock()
}

func (fp *fakeProbe) Inspect(context.Context, uint32) (bool, *platform.ProcInfo, error) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if fp.proc != nil {
		fp.proc.mu.Lock()
		gone := fp.proc.gone
		fp.proc.mu.Unlock()
		if gone {
			return false, nil, nil
		}
	}
	return fp.running, fp.info, nil
}
func (fp *fakeProbe) WaitForReady(time.Duration) bool { return true }
func (fp *fakeProbe) FocusMainWindow(uint32) bool     { return false }
func (fp *fakeProbe) FocusAnyWindow() bool            { return false }

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

// mustCmdExe 真进程冒烟用 cmd.exe（PATH 不含 System32 的受限环境回退
// COMSPEC，皆不可得如实 skip——snipaste 同族环境守卫先例）。
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

func aliveCmdProc(exe string) *fakeProcessAPI {
	return &fakeProcessAPI{info: platform.ProcInfo{ExePath: exe, StartedAt: time.Now()}}
}

// ---------- 提权特判（#17 三重契约之①） ----------

func TestElevateHintContract(t *testing.T) {
	// 740 → 指引文案；文案必须含"管理员"关键词（前端 ElevateRestart 组件据此
	// 挂载一键提权重启，跨层契约家族同锁）。
	raw := error(syscall.Errno(740))
	hint := elevateHint(raw)
	if !strings.Contains(hint, "管理员") || !strings.Contains(hint, "RAMMap") {
		t.Fatalf("740 必须改写为含管理员关键词的指引: %q", hint)
	}
	if h := elevateHint(errors.New("其它错误")); h != "" {
		t.Fatalf("非 740 错误不得套提权指引: %q", h)
	}
	// 包装链（%w 多层）必须穿透
	if h := elevateHint(fmt.Errorf("启动失败: %w", fmt.Errorf("spawn: %w", raw))); h == "" {
		t.Fatal("errors.As 必须穿透包装链")
	}
}

func TestIsSpawnFailurePrefixes(t *testing.T) {
	if !isSpawnFailure(sup.Snapshot{State: sup.StateFailed, Error: "进程启动失败: x"}) ||
		!isSpawnFailure(sup.Snapshot{State: sup.StateFailed, Error: "进程创建失败: x"}) {
		t.Fatal("spawn 失败前缀必须命中暂扣")
	}
	if isSpawnFailure(sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 1）"}) {
		t.Fatal("运行期崩溃不是 spawn 失败，不得暂扣")
	}
}

// ---------- 探针兼容兜底契约 ----------

func TestNoExternalProbeAlwaysAbsent(t *testing.T) {
	running, info, err := noExternalProbe{}.Inspect(context.Background(), 0)
	if running || info != nil || err != nil {
		t.Fatalf("探针必须恒报不在场: running=%v info=%#v err=%v", running, info, err)
	}
}

// ---------- 词表与形状映射 ----------

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
		{"external", sup.Snapshot{State: sup.StateExternal}, StateExternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.onSupState(c.in)
			if got := rec.last().State; got != c.want {
				t.Fatalf("state = %s, want %s", got, c.want)
			}
		})
	}

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
	// 窗口外异常退出：RAMMap 文案（引导回版本管理重装）
	e.onSupState(sup.Snapshot{State: sup.StateFailed, Error: "托管进程异常退出（退出码 2）"})
	if got := rec.last(); got.State != StateFailed || !strings.Contains(got.Error, "RAMMap 异常退出（退出码 2）") {
		t.Fatalf("窗口外异常退出应还原既有文案: %+v", got)
	}
}

func TestStoppedMessageAndPidShape(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, rec.cb())

	e.onSupState(sup.Snapshot{State: sup.StateRunning, Version: "v2.7.0", PID: 4242, Exe: `C:\v\RAMMap.exe`, Since: time.Now()})
	run := rec.last()
	if run.PID != 4242 || run.ExePath != `C:\v\RAMMap.exe` || run.Version != "v2.7.0" {
		t.Fatalf("running 快照形状异常: %+v", run)
	}
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Version: "v2.7.0", PID: 4242, Exe: `C:\v\RAMMap.exe`, Error: "已手动停止"})
	stop := rec.last()
	if stop.State != StateStopped || stop.Error != "" || stop.PID != 0 {
		t.Fatalf("stopped 映射异常: %+v", stop)
	}
	if stop.StoppedAt.IsZero() || stop.StoppedAt.Before(run.StartedAt) {
		t.Fatalf("StoppedAt 应落账: %+v", stop)
	}
}

func TestStartupSelfCheckLatchMapsFailed(t *testing.T) {
	rec := &eventRecorder{}
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, rec.cb())
	e.mu.Lock()
	e.latchedErr = "启动进程路径不匹配: C:\\Else\\other.exe"
	e.mu.Unlock()
	e.onSupState(sup.Snapshot{State: sup.StateStopped, Error: "已手动停止"})
	snap := rec.last()
	if snap.State != StateFailed || snap.Error != "启动进程路径不匹配: C:\\Else\\other.exe" || !snap.StoppedAt.IsZero() {
		t.Fatalf("锁存文案应改写为 failed 且不落停止时刻: %+v", snap)
	}
}

// ---------- Quit 分层归因决策表（seam 注入） ----------

func TestFinishQuitAttributionTable(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, Callbacks{})
	snap := sup.Snapshot{State: sup.StateRunning, PID: 42, Exe: `C:\v\RAMMap.exe`}

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
				verify = alive
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
		})
	}
}

func TestQuitNotManagedGate(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, nil, Callbacks{})
	if res, err := e.Quit(); err != nil || res.Method != "not-managed" || res.Stopped {
		t.Fatalf("静止态 Quit 应回 not-managed: %#v err=%v", res, err)
	}
}

func TestVerifyTokenRejectsPathMismatch(t *testing.T) {
	dir := t.TempDir()
	proc := &fakeProcessAPI{info: platform.ProcInfo{PID: 10, ExePath: filepath.Join(dir, "other.exe"), StartedAt: time.Now()}}
	e := NewEngine(&fakeJobAPI{}, proc, nil, Callbacks{})
	err := e.verifyToken(platform.VerifyToken{PID: 10, ExePath: filepath.Join(dir, "RAMMap.exe"), StartedAt: proc.info.StartedAt})
	if err != platform.ErrTokenMismatch {
		t.Fatalf("err=%v", err)
	}
}

// ---------- QuitExternal confirm-force 档门控 ----------

// enterExternal 用可控探针把引擎推进 external 态（RefreshExternal 静止态探测）。
func enterExternal(t *testing.T, e *Engine, info *platform.ProcInfo) {
	t.Helper()
	fp := e.probe.(*fakeProbe)
	fp.set(true, info)
	e.RefreshExternal()
	if !e.ExternalRunning() {
		t.Fatalf("探针在场应建立 external 态, got %s", e.Snapshot().State)
	}
}

func TestQuitExternalRequiresExternalState(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, &fakeProbe{}, Callbacks{})
	res, err := e.QuitExternal(context.Background(), externalquit.PolicyConfirmForce, "risk", func(string) bool { return true })
	if err != nil || res.Method != "not-external" {
		t.Fatalf("静止态应拒: %#v err=%v", res, err)
	}
}

func TestQuitExternalProbeMissingPidRefuses(t *testing.T) {
	proc := &fakeProcessAPI{}
	e := NewEngine(&fakeJobAPI{}, proc, &fakeProbe{}, Callbacks{})
	enterExternal(t, e, nil) // 在场但身份不明
	killed := false
	proc.killErr = fmt.Errorf("boom")
	res, err := e.QuitExternal(context.Background(), externalquit.PolicyConfirmForce, "risk", func(string) bool { killed = true; return true })
	if err != nil || res.Method != "probe-missing-pid" {
		t.Fatalf("无身份必须拒执行: %#v err=%v", res, err)
	}
	if killed {
		t.Fatal("拒执行路径不得触达确认与终止")
	}
}

func TestQuitExternalDeclinedKeepsExternal(t *testing.T) {
	old := externalGrace
	externalGrace = 100 * time.Millisecond
	defer func() { externalGrace = old }()

	info := platform.ProcInfo{PID: 9001, ExePath: `C:\ext\RAMMap.exe`, StartedAt: time.Now()}
	proc := &fakeProcessAPI{info: info}
	e := NewEngine(&fakeJobAPI{}, proc, &fakeProbe{}, Callbacks{})
	e.closeByPID = func(uint32) int { return 0 } // 无可投窗口：优雅段空转后进入确认
	enterExternal(t, e, &info)

	res, err := e.QuitExternal(context.Background(), externalquit.PolicyConfirmForce, "风险文案", func(string) bool { return false })
	if err != nil || res.Method != externalquit.MethodDeclined || res.Stopped || res.Forced {
		t.Fatalf("拒绝确认应 declined 且不动手: %#v err=%v", res, err)
	}
	if proc.gone {
		t.Fatal("declined 路径不得终止进程")
	}
}

func TestQuitExternalForcedAfterConsent(t *testing.T) {
	old := externalGrace
	externalGrace = 100 * time.Millisecond
	defer func() { externalGrace = old }()

	info := platform.ProcInfo{PID: 9002, ExePath: `C:\ext\RAMMap.exe`, StartedAt: time.Now()}
	proc := &fakeProcessAPI{info: info}
	fp := &fakeProbe{proc: proc} // 探针与进程替身联动：杀成即"看不见"
	e := NewEngine(&fakeJobAPI{}, proc, fp, Callbacks{})
	e.closeByPID = func(uint32) int { return 0 }
	enterExternal(t, e, &info)

	res, err := e.QuitExternal(context.Background(), externalquit.PolicyConfirmForce, "风险", func(string) bool { return true })
	if err != nil || res.Method != externalquit.MethodForced || !res.Stopped || !res.Forced {
		t.Fatalf("同意后应强杀收口: %#v err=%v", res, err)
	}
	// 收口后复探：探针已随 KillVerified 报 gone → external 撤销落 stopped
	if e.Snapshot().State != StateStopped {
		t.Fatalf("强杀收口后应回到 stopped: %s", e.Snapshot().State)
	}
}

func TestQuitExternalBlockedIsHonest(t *testing.T) {
	old := externalGrace
	externalGrace = 100 * time.Millisecond
	defer func() { externalGrace = old }()

	info := platform.ProcInfo{PID: 9003, ExePath: `C:\ext\RAMMap.exe`, StartedAt: time.Now()}
	proc := &fakeProcessAPI{info: info, killErr: platform.ErrAccessDenied} // UIPI 拦截
	e := NewEngine(&fakeJobAPI{}, proc, &fakeProbe{}, Callbacks{})
	e.closeByPID = func(uint32) int { return 0 }
	enterExternal(t, e, &info)

	res, err := e.QuitExternal(context.Background(), externalquit.PolicyConfirmForce, "风险", func(string) bool { return true })
	if err != nil || res.Method != externalquit.MethodBlocked || res.Stopped {
		t.Fatalf("提权目标应如实 blocked: %#v err=%v", res, err)
	}
}

// ---------- 生命周期冒烟（真 Job Object） ----------

func TestStartDetachedFlagWiresJob(t *testing.T) {
	cmdExe := mustCmdExe(t)
	jobs := &fakeJobAPI{}
	proc := windows.NewProcessAPI()
	engine := NewEngine(jobs, proc, &fakeProbe{}, Callbacks{})
	if err := engine.Start(StartOptions{Version: "test", Exe: cmdExe, Detached: true}); err != nil {
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
		t.Fatal("Detached=true 必须解除退出联动")
	}
}

func TestStartKeepsKillOnCloseWhenFollow(t *testing.T) {
	cmdExe := mustCmdExe(t)
	jobs := &fakeJobAPI{}
	proc := windows.NewProcessAPI()
	engine := NewEngine(jobs, proc, &fakeProbe{}, Callbacks{})
	if err := engine.Start(StartOptions{Version: "test", Exe: cmdExe, Detached: false}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		outer := engine.sup.Snapshot()
		if outer.State == sup.StateRunning || outer.State == sup.StateStarting {
			_ = proc.KillVerified(context.Background(), quitToken(outer), true)
		}
	})
	// 联动开启：内核不得解除退出联动（allow 保持默认 true 或不调用）
	if jobs.job.allow != nil && !*jobs.job.allow {
		t.Fatal("Detached=false 不得解除退出联动")
	}
}

func TestStartRejectsEmptyExe(t *testing.T) {
	e := NewEngine(&fakeJobAPI{}, &fakeProcessAPI{}, &fakeProbe{}, Callbacks{})
	if err := e.Start(StartOptions{Version: "2026-03-26"}); err == nil || err.Error() != payloadImageName()+" 路径不能为空" {
		t.Fatalf("空路径应报既有文案, got %v", err)
	}
}

func TestStartIdentityMismatchAborts(t *testing.T) {
	cmdExe := mustCmdExe(t)
	proc := &fakeProcessAPI{info: platform.ProcInfo{ExePath: `C:\Else\other.exe`, StartedAt: time.Now()}}
	rec := &eventRecorder{}
	e := NewEngine(newWindowsJobAPI(), proc, &fakeProbe{}, rec.cb())
	err := e.Start(StartOptions{Version: "v2.7.0", Exe: cmdExe})
	if err == nil {
		t.Skipf("cmd 即时退出未走自检，环境不适合本测试: %+v", e.Snapshot())
	}
	if !strings.Contains(err.Error(), "启动进程路径不匹配") {
		t.Fatalf("路径不符应拒绝启动: %v", err)
	}
	waitState(t, e, StateFailed)
}

func TestStartRejectsWhenActive(t *testing.T) {
	cmdExe := mustCmdExe(t)
	e := NewEngine(newWindowsJobAPI(), aliveCmdProc(cmdExe), &fakeProbe{}, Callbacks{})
	e.closeByPID = func(uint32) int { return 0 }
	startCmdLoop(t, e, 30)
	if err := e.Start(StartOptions{Version: "v2.7.0", Exe: cmdExe, Detached: true}); err == nil ||
		err.Error() != "本会话启动的 RAMMap 已在运行" {
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

// TestQuitCloseRequestSmoke 真进程冒烟：宽限窗口内自然收口归因 close-request。
func TestQuitCloseRequestSmoke(t *testing.T) {
	cmdExe := mustCmdExe(t)
	engine := NewEngine(newWindowsJobAPI(), aliveCmdProc(cmdExe), &fakeProbe{}, Callbacks{})
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

// TestQuitForcedJobSmoke 真进程冒烟：宽限期不收口 → 复核在世 → 整树强杀。
func TestQuitForcedJobSmoke(t *testing.T) {
	cmdExe := mustCmdExe(t)
	rec := &eventRecorder{}
	engine := NewEngine(newWindowsJobAPI(), aliveCmdProc(cmdExe), &fakeProbe{}, rec.cb())
	engine.closeByPID = func(uint32) int { return 0 }
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

// startCmdLoop 以真 Job 拉起"存活 N 秒"的 cmd 实例并等 running（直接走内核：
// 绕过 e.Start 的模块侧身份自检）。PING.EXE 绝对路径：与宿主机 PATH 无关。
func startCmdLoop(t *testing.T, e *Engine, seconds int) {
	t.Helper()
	exe := mustCmdExe(t)
	ping := filepath.Join(filepath.Dir(exe), "PING.EXE")
	if _, err := os.Stat(ping); err != nil {
		t.Skip("PING.EXE 不可用，真进程冒烟在当前环境无法执行")
	}
	args := []string{"/C", fmt.Sprintf("%s -n %d 127.0.0.1", ping, seconds)}
	if err := e.sup.Start(context.Background(), sup.Spec{Version: "v2.7.0", Exe: exe, Args: args, DetachFromJob: true}); err != nil {
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
