package instance

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
)

// 本文件全部走假缝（假 spawner/假 stdin 管道/假 JobAPI/假探针）：零真网、
// 零真 piik、零真进程。进程治理主流程的家族纪律（四分类顺序/收口窗口/
// external 甄别）在本包自带实现上逐条钉死。

// ---------- 假进程（含假 stdin 管道） ----------

type fakeProc struct {
	pid  uint32
	spec procSpec

	startedCh chan struct{} // Start 调用即关
	closeOnce sync.Once
	exitCh    chan struct{}

	mu         sync.Mutex
	code       int
	exitErr    bool
	exited     bool
	stdin      *fakeStdin
	outW, errW *io.PipeWriter
	outR, errR *io.PipeReader
}

type fakeStdin struct {
	mu          sync.Mutex
	writes      []byte
	closed      bool
	exitOnWrite bool // 模拟上游"stdin 收到任意字节优雅停机"
	proc        *fakeProc
	blockErr    error // 非 nil 时 Write 直接失败（管道半断场）
}

func (w *fakeStdin) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.blockErr != nil {
		err := w.blockErr
		w.mu.Unlock()
		return 0, err
	}
	w.writes = append(w.writes, p...)
	shouldExit := w.exitOnWrite && !w.proc.isExited()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	if shouldExit {
		w.proc.exit(0) // 上游语义：任意字节 → 内部预算后 code 0 收口
	}
	return len(p), nil
}

func (w *fakeStdin) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return nil
}

func (w *fakeStdin) writeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.writes)
}

func newFakeProc(pid uint32) *fakeProc {
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	p := &fakeProc{
		pid:       pid,
		startedCh: make(chan struct{}),
		exitCh:    make(chan struct{}),
		outR:      outR, outW: outW,
		errR: errR, errW: errW,
	}
	p.stdin = &fakeStdin{exitOnWrite: true, proc: p}
	return p
}

func (p *fakeProc) isExited() bool {
	select {
	case <-p.exitCh:
		return true
	default:
		return false
	}
}

func (p *fakeProc) exit(code int) {
	p.mu.Lock()
	if !p.exited {
		p.exited = true
		p.code = code
	}
	p.mu.Unlock()
	p.closeOnce.Do(func() { close(p.exitCh) })
}

func (p *fakeProc) setStdinSpec(fn func(*fakeStdin)) { fn(p.stdin) }

// Start 由引擎在 spawn 后调用；假进程"启动即活"，退出全由测试驱动。
func (p *fakeProc) Start() error {
	close(p.startedCh)
	return nil
}

func (p *fakeProc) Wait() error {
	<-p.exitCh
	p.mu.Lock()
	code, exitErr := p.code, p.exitErr
	p.mu.Unlock()
	if code == 0 && !exitErr {
		return nil
	}
	return errors.New("exit status " + strconv.Itoa(code))
}

func (p *fakeProc) Kill() error {
	p.exit(1)
	return nil
}

func (p *fakeProc) PID() uint32 { return p.pid }

func (p *fakeProc) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

func (p *fakeProc) Stdin() io.WriteCloser { return p.stdin }
func (p *fakeProc) Stdout() io.Reader     { return p.outR }
func (p *fakeProc) Stderr() io.Reader     { return p.errR }

// feedStdout/feedStderr 测试侧灌输出（泵侧 EOF 由 Close 触发）。
func (p *fakeProc) feedStdout(lines ...string) {
	for _, l := range lines {
		_, _ = p.outW.Write([]byte(l + "\n"))
	}
}
func (p *fakeProc) feedStderr(lines ...string) {
	for _, l := range lines {
		_, _ = p.errW.Write([]byte(l + "\n"))
	}
}
func (p *fakeProc) closePipes() {
	_ = p.outW.Close()
	_ = p.errW.Close()
}

// ---------- 假 Job ----------

type fakeJob struct {
	mu           sync.Mutex
	assignedPID  uint32
	terminations int
	allowFalse   int // SetAllowKillOnClose(false) 次数
	closes       int
	assignErr    error
	detachErr    error
	onTerminate  func(code uint32)
}

func (j *fakeJob) Assign(pid uint32) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.assignErr != nil {
		return j.assignErr
	}
	j.assignedPID = pid
	return nil
}
func (j *fakeJob) Close() error {
	j.mu.Lock()
	j.closes++
	j.mu.Unlock()
	return nil
}
func (j *fakeJob) Terminate(code uint32) error {
	j.mu.Lock()
	j.terminations++
	fn := j.onTerminate
	j.mu.Unlock()
	if fn != nil {
		fn(code)
	}
	return nil
}
func (j *fakeJob) SetAllowKillOnClose(enabled bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !enabled {
		if j.detachErr != nil {
			return j.detachErr
		}
		j.allowFalse++
	}
	return nil
}

type fakeJobAPI struct {
	mu        sync.Mutex
	job       *fakeJob
	createErr error
	onCreate  func(*fakeJob) // 装配点（harness 接默认整树语义）
}

func (f *fakeJobAPI) Create() (platform.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.job == nil {
		f.job = &fakeJob{}
		if f.onCreate != nil {
			f.onCreate(f.job)
		}
	}
	return f.job, nil
}

// ---------- 假探针 ----------

type fakeProbe struct {
	mu      sync.Mutex
	running bool
	port    map[int]bool
	pids    []uint32
}

func newFakeProbe() *fakeProbe { return &fakeProbe{port: map[int]bool{}} }

func (p *fakeProbe) FindPIDs() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint32(nil), p.pids...)
}
func (p *fakeProbe) ProcessRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}
func (p *fakeProbe) PortListening(port int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.port[port]
}
func (p *fakeProbe) set(running bool, listenPorts ...int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = running
	p.port = map[int]bool{}
	for _, lp := range listenPorts {
		p.port[lp] = true
	}
}

// ---------- 事件记录 ----------

type eventRecorder struct {
	mu     sync.Mutex
	states []Snapshot
	logs   []LogEntry
}

func (r *eventRecorder) cb() Callbacks {
	return Callbacks{
		OnState: func(s Snapshot) {
			r.mu.Lock()
			r.states = append(r.states, s)
			r.mu.Unlock()
		},
		OnLog: func(e LogEntry) {
			r.mu.Lock()
			r.logs = append(r.logs, e)
			r.mu.Unlock()
		},
	}
}

func (r *eventRecorder) logLines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.logs))
	for _, l := range r.logs {
		out = append(out, l.Line)
	}
	return out
}

// ---------- 测试 harness ----------

type harness struct {
	e     *Engine
	probe *fakeProbe
	jobs  *fakeJobAPI
	rec   *eventRecorder
	mu    sync.Mutex
	procs []*fakeProc
	specs []procSpec
	errOn error // 非 nil 时 spawner 失败（进程创建失败注入）
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{probe: newFakeProbe(), rec: &eventRecorder{}}
	h.jobs = &fakeJobAPI{onCreate: func(j *fakeJob) {
		// 默认真实 JobObject 语义：Terminate(code) 即整树收口 → 当前代假进程退场
		j.onTerminate = func(code uint32) {
			if cp := h.lastProc(); cp != nil {
				cp.exit(int(code))
			}
		}
	}}
	e := NewEngine(h.jobs, h.probe, h.rec.cb())
	e.spawn = func(spec procSpec) (procHandle, error) {
		if h.errOn != nil {
			return nil, h.errOn
		}
		p := newFakeProc(uint32(4000 + len(h.procs)))
		p.spec = spec
		h.mu.Lock()
		h.procs = append(h.procs, p)
		h.specs = append(h.specs, spec)
		h.mu.Unlock()
		return p, nil
	}
	h.e = e
	return h
}

func (h *harness) lastProc() *fakeProc {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.procs) == 0 {
		return nil
	}
	return h.procs[len(h.procs)-1]
}

func (h *harness) lastSpec() procSpec {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.specs) == 0 {
		return procSpec{}
	}
	return h.specs[len(h.specs)-1]
}

func testOptions() Options {
	return Options{
		Version:    "v1.2.0",
		Exe:        `C:\hanxi\versions\piik_1.2.0\piik-app.exe`,
		Port:       8791,
		ConfigPath: `C:\hanxi\data\piik\client.json`,
		LogDir:     `C:\hanxi\data\piik\logs`,
	}
}

// startRunning 冷启动推进到 running 并让探针认账（进程在 + 端口 LISTEN）。
func (h *harness) startRunning(t *testing.T, opts Options) *fakeProc {
	t.Helper()
	if err := h.e.Start(opts); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p := h.lastProc()
	if p == nil {
		t.Fatal("未拉起假进程")
	}
	h.probe.set(true, opts.Port)
	h.probe.mu.Lock()
	h.probe.pids = []uint32{p.pid}
	h.probe.mu.Unlock()
	return p
}

func waitState(t *testing.T, e *Engine, want State, timeout time.Duration) Snapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := e.Snapshot()
		if s.State == want {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state = %s（期望 %s）超时", e.Snapshot().State, want)
	return e.Snapshot()
}

func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待条件超时: %s", desc)
}

// withTimings 临时压缩包级时长变量（单测不真等 6s/10s 窗口）。
func withTimings(t *testing.T, grace, settle time.Duration) {
	t.Helper()
	oldG, oldS, oldP := quitGrace, settleTimeout, probePollInterval
	quitGrace, settleTimeout, probePollInterval = grace, settle, 5*time.Millisecond
	t.Cleanup(func() { quitGrace, settleTimeout, probePollInterval = oldG, oldS, oldP })
}

// ---------- 组参/URL/解析纯函数 ----------

func TestBuildArgsAndEnv(t *testing.T) {
	args := buildArgs(testOptions())
	want := []string{"--local", "--port", "8791", "--config", `C:\hanxi\data\piik\client.json`, "--log-dir", `C:\hanxi\data\piik\logs`}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Errorf("组参漂移: %v", args)
	}
	env := buildEnv()
	if len(env) != 1 || env[0] != "PIIK_CLIENT_GATE_NO_BROWSER=true" {
		t.Errorf("机读模式 env 漂移: %v", env)
	}
}

func TestURLForPort(t *testing.T) {
	if got := URLForPort(8787); got != "http://127.0.0.1:8787/" {
		t.Errorf("URLForPort = %q", got)
	}
}

func TestParseMachineLine(t *testing.T) {
	cases := []struct {
		line string
		chk  func(machineUpdate) bool
		desc string
	}{
		{"Local access: open", func(u machineUpdate) bool { return u.changed && u.localOpen }, "开放本地访问"},
		{"Local access: password", func(u machineUpdate) bool { return u.changed && !u.localOpen }, "非 open 值不置开放"},
		{"Local access password: hunter2", func(u machineUpdate) bool { return u.changed && u.passwordSet && !u.localOpen }, "口令置位（明文丢弃）"},
		{"Local access password: ", func(u machineUpdate) bool { return u.changed && !u.passwordSet }, "空值不置位"},
		{"LAN invitation origin: http://192.168.1.7:8791", func(u machineUpdate) bool { return u.changed && u.lanInvitation == "http://192.168.1.7:8791" }, "LAN 来源"},
		{"Public invitation origin: https://abc.trycloudflare.com", func(u machineUpdate) bool { return u.changed && u.publicInvitation == "https://abc.trycloudflare.com" }, "公网来源"},
		{"GATE_NO_BROWSER: open manually", func(u machineUpdate) bool { return u.changed && u.noBrowser }, "拉浏览器失败位"},
		{"2026/09/26 10:00:00 server ready", func(u machineUpdate) bool { return !u.changed }, "普通行不入机读"},
		{"", func(u machineUpdate) bool { return !u.changed }, "空行"},
	}
	for _, c := range cases {
		u := parseMachineLine(c.line)
		if !c.chk(u) {
			t.Errorf("%s: line=%q update=%+v", c.desc, c.line, u)
		}
	}
	// CRLF 残留剥除后仍可解析
	if u := parseMachineLine("Local access: open\r"); !u.localOpen {
		t.Error("CRLF 行解析漂移")
	}
}

func TestMaskSecretLine(t *testing.T) {
	got := maskSecretLine("Local access password: hunter2")
	if !strings.HasPrefix(got, machinePasswordPrefix) || strings.Contains(got, "hunter2") {
		t.Errorf("口令行脱敏失败: %q", got)
	}
	if keep := maskSecretLine("Local access: open"); keep != "Local access: open" {
		t.Errorf("普通行被改写: %q", keep)
	}
}

// ---------- 生命周期 ----------

func TestStartBuildsInvocationAndReachesRunning(t *testing.T) {
	h := newHarness(t)
	p := h.startRunning(t, testOptions())

	spec := h.lastSpec()
	if spec.Exe != testOptions().Exe {
		t.Errorf("Exe 漂移: %s", spec.Exe)
	}
	if spec.WorkingDir != `C:\hanxi\versions\piik_1.2.0` {
		t.Errorf("工作目录未锁定 exe 所在目录: %s", spec.WorkingDir)
	}
	if !spec.HideWindow {
		t.Error("控制台子系统必须无窗拉起")
	}
	if strings.Join(spec.Args, "|") != strings.Join(buildArgs(testOptions()), "|") {
		t.Errorf("Args 漂移: %v", spec.Args)
	}
	if len(spec.Env) != 1 || spec.Env[0] != "PIIK_CLIENT_GATE_NO_BROWSER=true" {
		t.Errorf("Env 漂移: %v", spec.Env)
	}

	snap := h.e.Snapshot()
	if snap.State != StateRunning || snap.Version != "v1.2.0" || snap.PID != p.pid || snap.Port != 8791 {
		t.Errorf("running 快照漂移: %+v", snap)
	}
	if got := h.e.LocalURL(); got != "http://127.0.0.1:8791/" {
		t.Errorf("LocalURL = %q", got)
	}
	// 事件序列：starting → running
	var seq []State
	for _, s := range h.rec.states {
		seq = append(seq, s.State)
	}
	if len(seq) < 2 || seq[0] != StateStarting || seq[len(seq)-1] != StateRunning {
		t.Errorf("状态广播序列漂移: %v", seq)
	}
}

func TestStartValidationAndBusy(t *testing.T) {
	h := newHarness(t)
	bad := testOptions()
	bad.Port = 0
	if err := h.e.Start(bad); err == nil {
		t.Error("端口 0 应拒")
	}
	bad = testOptions()
	bad.ConfigPath = ""
	if err := h.e.Start(bad); err == nil {
		t.Error("空配置路径应拒")
	}
	bad = testOptions()
	bad.Exe = ""
	if err := h.e.Start(bad); err == nil {
		t.Error("空 exe 应拒")
	}

	h.startRunning(t, testOptions())
	if err := h.e.Start(testOptions()); !errors.Is(err, errBusy) {
		t.Errorf("在途再启动应 errBusy，得 %v", err)
	}
}

func TestStartSpawnFailureLandsFailed(t *testing.T) {
	h := newHarness(t)
	h.errOn = errors.New("fork boom")
	err := h.e.Start(testOptions())
	if err == nil || !strings.Contains(err.Error(), "进程创建失败") {
		t.Fatalf("创建失败应上抛: %v", err)
	}
	if s := h.e.Snapshot(); s.State != StateFailed || s.Error == "" {
		t.Errorf("failed 落账漂移: %+v", s)
	}
}

func TestJobCreateFailureKillsProcess(t *testing.T) {
	h := newHarness(t)
	h.jobs.createErr = errors.New("job create denied")
	err := h.e.Start(testOptions())
	if err == nil || !strings.Contains(err.Error(), "创建 Job Object 失败") {
		t.Fatalf("Job 创建失败应上抛: %v", err)
	}
	p := h.lastProc()
	waitFor(t, "假进程已被兜底强杀", func() bool { return p.isExited() })
	if s := h.e.Snapshot(); s.State != StateFailed {
		t.Errorf("failed 落账漂移: %+v", s)
	}
}

func TestDetachUnlinksKillOnClose(t *testing.T) {
	h := newHarness(t)
	opts := testOptions()
	opts.Detached = true
	h.startRunning(t, opts)
	h.jobs.mu.Lock()
	job := h.jobs.job
	h.jobs.mu.Unlock()
	if job == nil {
		t.Fatal("未建 Job")
	}
	job.mu.Lock()
	allowFalse := job.allowFalse
	job.mu.Unlock()
	if allowFalse != 1 {
		t.Errorf("Detached 必须 SetAllowKillOnClose(false) 恰一次: %d", allowFalse)
	}
}

// ---------- Quit / Stop ----------

func TestQuitGracefulViaStdin(t *testing.T) {
	withTimings(t, 2*time.Second, 2*time.Second)
	h := newHarness(t)
	p := h.startRunning(t, testOptions())
	// 假 stdin：exitOnWrite=true（收到任意字节 → code 0 优雅收口）

	if err := h.e.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if p.stdin.writeCount() == 0 {
		t.Fatal("必须向 stdin 投递停机字节")
	}
	h.jobs.mu.Lock()
	job := h.jobs.job
	h.jobs.mu.Unlock()
	if job == nil {
		t.Fatal("未建 Job")
	}
	job.mu.Lock()
	terms := job.terminations
	job.mu.Unlock()
	if terms != 0 {
		t.Errorf("优雅收口不得动用 JobObject 强杀: %d", terms)
	}
	snap := waitState(t, h.e, StateStopped, time.Second)
	if snap.Error != "" || snap.ExitCode != 0 || snap.PID != 0 {
		t.Errorf("stopped 终态快照漂移: %+v", snap)
	}
	if snap.StoppedAt.IsZero() {
		t.Error("stoppedAt 未落账")
	}
}

func TestQuitFallsBackToTerminate(t *testing.T) {
	// 上游 5s 预算赌头不收口（假进程无视 stdin 信令）→ 宽限到期 JobObject 兜底
	withTimings(t, 30*time.Millisecond, 2*time.Second)
	h := newHarness(t)
	p := h.startRunning(t, testOptions())
	p.setStdinSpec(func(w *fakeStdin) { w.exitOnWrite = false }) // 信令不灵
	h.jobs.mu.Lock()
	job := h.jobs.job
	h.jobs.mu.Unlock()
	if job == nil {
		t.Fatal("未建 Job")
	}
	job.mu.Lock()
	job.onTerminate = func(uint32) { p.exit(1) } // 强杀整树（自有代）
	job.mu.Unlock()

	if err := h.e.Quit(); err != nil {
		t.Fatalf("Quit（兜底路径）: %v", err)
	}
	job.mu.Lock()
	terms := job.terminations
	job.mu.Unlock()
	if terms != 1 {
		t.Errorf("宽限到期必须 JobObject 整树终止恰一次: %d", terms)
	}
	waitState(t, h.e, StateStopped, time.Second) // stopping 类恒先落 stopped，不误 external
}

func TestStopTerminatesWithoutStdin(t *testing.T) {
	withTimings(t, time.Second, 2*time.Second)
	h := newHarness(t)
	p := h.startRunning(t, testOptions())
	h.jobs.mu.Lock()
	job := h.jobs.job
	h.jobs.mu.Unlock()
	job.mu.Lock()
	job.onTerminate = func(uint32) { p.exit(1) }
	job.mu.Unlock()

	if err := h.e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if p.stdin.writeCount() != 0 {
		t.Error("Stop 不投 stdin 信令（与 Quit 的语义差）")
	}
	job.mu.Lock()
	terms := job.terminations
	job.mu.Unlock()
	if terms != 1 {
		t.Errorf("Stop 直接强杀: %d", terms)
	}
	waitState(t, h.e, StateStopped, time.Second)
}

func TestQuitAndStopIdempotent(t *testing.T) {
	h := newHarness(t)
	if err := h.e.Quit(); err != nil {
		t.Errorf("静止态 Quit 应幂等成功: %v", err)
	}
	if err := h.e.Stop(); err != nil {
		t.Errorf("静止态 Stop 应幂等成功: %v", err)
	}
	h.startRunning(t, testOptions())
	if err := h.e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := h.e.Stop(); err != nil {
		t.Errorf("二次 Stop 应幂等: %v", err)
	}
}

// ---------- wait 四分类 ----------

func TestWaitCategoriesZeroExitCleanStopped(t *testing.T) {
	h := newHarness(t)
	p := h.startRunning(t, testOptions())
	h.probe.set(false) // 我方退出后系统内再无 piik
	p.exit(0)
	snap := waitState(t, h.e, StateStopped, time.Second)
	if snap.Error != "" || snap.External {
		t.Errorf("类别 3（code 0）→ stopped 漂移: %+v", snap)
	}
}

func TestWaitCategoriesExternalTakeover(t *testing.T) {
	h := newHarness(t)
	p := h.startRunning(t, testOptions())
	// 我方进程退场，但探针仍见 piik 在世（外部接管）——类别 2 先于类别 3/4
	h.probe.mu.Lock()
	h.probe.running = true
	h.probe.pids = []uint32{7777} // 非我方 pid
	h.probe.mu.Unlock()
	p.mu.Lock()
	p.exitErr = true
	p.code = 1
	p.mu.Unlock()
	p.exit(1)
	snap := waitState(t, h.e, StateExternal, time.Second)
	if snap.PID != 7777 || !snap.External {
		t.Errorf("external 落账漂移: %+v", snap)
	}
	if got := h.e.Exe(); got != "" {
		t.Error("external 态 Exe 必须清空")
	}
}

func TestWaitCategoriesAbnormalFailed(t *testing.T) {
	h := newHarness(t)
	p := h.startRunning(t, testOptions())
	p.mu.Lock()
	p.exitErr = true
	p.code = 3
	p.mu.Unlock()
	h.probe.set(false)
	p.exit(3)
	snap := waitState(t, h.e, StateFailed, time.Second)
	if snap.ExitCode != 3 || !strings.Contains(snap.Error, "退出码 3") {
		t.Errorf("failed 落账漂移: %+v", snap)
	}
}

// ---------- RefreshExternal ----------

func TestRefreshExternalCycle(t *testing.T) {
	h := newHarness(t)
	// stopped → 探针见外部实例 → external
	h.probe.mu.Lock()
	h.probe.running = true
	h.probe.pids = []uint32{5555}
	h.probe.mu.Unlock()
	h.e.RefreshExternal()
	snap := h.e.Snapshot()
	if snap.State != StateExternal || snap.PID != 5555 {
		t.Fatalf("external 入账漂移: %+v", snap)
	}
	// external → 探针确认消失 → stopped
	h.probe.set(false)
	h.e.RefreshExternal()
	if s := h.e.Snapshot(); s.State != StateStopped || s.PID != 0 {
		t.Fatalf("external 撤销漂移: %+v", s)
	}
	// running 态守卫：不重分诊（探测到的正是自己）
	h.startRunning(t, testOptions())
	h.probe.set(false)
	h.e.RefreshExternal()
	if s := h.e.Snapshot(); s.State != StateRunning {
		t.Errorf("running 态 RefreshExternal 必须 no-op: %+v", s)
	}
}

// ---------- WaitReady 双因子 ----------

func TestWaitReadyRequiresBothFactors(t *testing.T) {
	withTimings(t, time.Second, time.Second)
	h := newHarness(t)
	h.startRunning(t, testOptions())
	// 进程在、端口未 LISTEN → 不成立
	h.probe.mu.Lock()
	h.probe.running = true
	h.probe.port = map[int]bool{}
	h.probe.mu.Unlock()
	if h.e.WaitReady(60 * time.Millisecond) {
		t.Fatal("仅进程在场不得判就绪")
	}
	// 端口 LISTEN、进程不在 → 不成立
	h.probe.mu.Lock()
	h.probe.running = false
	h.probe.port = map[int]bool{8791: true}
	h.probe.mu.Unlock()
	if h.e.WaitReady(60 * time.Millisecond) {
		t.Fatal("仅端口在场不得判就绪")
	}
	// 双因子合取 → 成立
	h.probe.set(true, 8791)
	if !h.e.WaitReady(2 * time.Second) {
		t.Fatal("双因子在场应就绪")
	}
}

// ---------- 机读字段 → 快照 → 日志 ----------

func TestMachineFieldsToSnapshotAndMaskedLogs(t *testing.T) {
	h := newHarness(t)
	p := h.startRunning(t, testOptions())

	p.feedStdout("2026/09/26 10:00:00 piik boot banner line")
	p.feedStdout("Local access: open")
	p.feedStdout("Local access password: s3cr3t-tok3n")
	p.feedStdout("LAN invitation origin: http://192.168.1.7:8791")
	p.feedStdout("Public invitation origin: https://abc.trycloudflare.com")
	p.feedStdout("GATE_NO_BROWSER: browser launch skipped")
	p.feedStderr("piik: upstream error sample")

	waitFor(t, "机读字段全部入快照", func() bool {
		s := h.e.Snapshot()
		return s.LocalAccessOpen && s.PasswordSet &&
			s.LanInvitation == "http://192.168.1.7:8791" &&
			s.PublicInvitation == "https://abc.trycloudflare.com" &&
			s.NoBrowser
	})

	// 快照 JSON 面零明文（事件载荷即前端的联编硬纪律）
	b, _ := json.Marshal(h.e.Snapshot())
	if strings.Contains(string(b), "s3cr3t-tok3n") {
		t.Fatal("快照泄漏口令明文")
	}

	waitFor(t, "stderr 与脱敏后的 stdout 进日志链", func() bool {
		lines := strings.Join(h.rec.logLines(), "\n")
		return strings.Contains(lines, "upstream error sample") &&
			strings.Contains(lines, machinePasswordPrefix+"***")
	})
	for _, l := range h.rec.logLines() {
		if strings.Contains(l, "s3cr3t-tok3n") {
			t.Fatal("日志事件链泄漏口令明文")
		}
	}
	if len(h.e.Logs(10)) == 0 {
		t.Error("环形缓冲未记账")
	}

	// 新一轮 Start 前必须收口（busy 纪律）：进程自然退场（探针同步失活）
	h.probe.set(false)
	p.exit(0)
	waitState(t, h.e, StateStopped, time.Second)

	// 机读账目新一代清零：重启后旧值不残留
	h.startRunning(t, testOptions())
	s := h.e.Snapshot()
	if s.LanInvitation != "" || s.NoBrowser || s.PasswordSet {
		t.Errorf("新一代机读账未清零: %+v", s)
	}
}

// ---------- 代际与收口纪律 ----------

func TestOldGenerationLinesDiscarded(t *testing.T) {
	h := newHarness(t)
	withTimings(t, time.Second, 2*time.Second)
	p1 := h.startRunning(t, testOptions())

	// 收口第一代（模拟竞态：quit 后旧管道仍可能尾随吐行）
	h.probe.set(false)
	p1.exit(0)
	waitState(t, h.e, StateStopped, time.Second)

	// 开新一代，旧管道迟到吐机读行
	h.startRunning(t, testOptions())
	p2 := h.lastProc()
	if p1 == p2 {
		t.Fatal("假 spawner 未发新代")
	}
	p1.feedStdout("LAN invitation origin: http://ghost:1")
	time.Sleep(80 * time.Millisecond)
	if got := h.e.Snapshot().LanInvitation; got == "http://ghost:1" {
		t.Error("上一代残留输出污染了新一代快照")
	}
	// 新一代正常吐行
	p2.feedStdout("Local access: open")
	waitFor(t, "新一代行入账", func() bool { return h.e.Snapshot().LocalAccessOpen })
}
