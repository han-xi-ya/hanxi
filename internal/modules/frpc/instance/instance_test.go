//go:build windows

package instance

import (
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
)

// eventRecorder 记录状态迁移与日志回调，便于断言事件序列。
type eventRecorder struct {
	mu     sync.Mutex
	states []Snapshot
	lines  []string
}

func (r *eventRecorder) cb() Callbacks {
	return Callbacks{
		OnState: func(s Snapshot) {
			r.mu.Lock()
			r.states = append(r.states, s)
			r.mu.Unlock()
		},
		OnLog: func(_ string, line string) {
			r.mu.Lock()
			r.lines = append(r.lines, line)
			r.mu.Unlock()
		},
	}
}

func (r *eventRecorder) snapshotStates() []Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Snapshot(nil), r.states...)
}

func (r *eventRecorder) logLines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// newWindowsJobAPI 使用真实 Windows Job Object（与生产同源）。
func newWindowsJobAPI() platform.JobAPI {
	return windows.NewJobAPI()
}

// ---------- 测试用 fake（仅用于 Job 失败分支注入） ----------

type fakeJob struct{ assignErr error }

func (f *fakeJob) Assign(pid uint32) error        { return f.assignErr }
func (f *fakeJob) Close() error                   { return nil }
func (f *fakeJob) Terminate(code uint32) error    { return nil }
func (f *fakeJob) SetAllowKillOnClose(bool) error { return nil }

// fakeJobAPI 通过 err 控制 Create 失败，通过 job.assignErr 控制 Assign 失败。
type fakeJobAPI struct {
	createErr  error
	assignErr  error
	createdJob bool
}

func (f *fakeJobAPI) Create() (platform.Job, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.createdJob = true
	return &fakeJob{assignErr: f.assignErr}, nil
}

// waitState 轮询等待实例进入目标状态（带超时）。
func waitState(t *testing.T, in *Instance, want State, timeout time.Duration) Snapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := in.Snapshot()
		if s.State == want {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	cur := in.Snapshot()
	t.Fatalf("state = %s (want %s): %+v", cur.State, want, cur)
	return cur
}

// mustInstance 按项目 ID 取实例。
func (m *Manager) mustInstance(id string) *Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	in, ok := m.instances[id]
	if !ok {
		panic(fmt.Sprintf("no instance for %s", id))
	}
	return in
}

// TestInstanceLifecycleEcho cmd /c echo 走完整生命周期：running → stopped，日志捕获。
func TestInstanceLifecycleEcho(t *testing.T) {
	rec := &eventRecorder{}
	m := NewManager(newWindowsJobAPI(), rec.cb())

	err := m.Start(StartOptions{
		ProjectID:   "p1",
		ProjectName: "echo-proj",
		Version:     "v0.0.0",
		FrpcExe:     "cmd",
		ConfigPath:  "/c echo hanxi-test-line",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	in := m.mustInstance("p1")
	snap := in.Snapshot()
	if snap.State != StateRunning || snap.PID == 0 {
		t.Fatalf("expect running with pid, got %+v", snap)
	}

	waitState(t, in, StateStopped, 5*time.Second)

	logs := in.Logs(0)
	var found bool
	for _, l := range logs {
		if strings.Contains(l, "hanxi-test-line") {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs %v missing echo output", logs)
	}

	s := in.Snapshot()
	if s.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", s.ExitCode)
	}
	if s.Error != "" {
		t.Fatalf("error = %q, want empty for clean exit", s.Error)
	}

	// AllSnapshots 应包含本实例且状态 stopped
	all := m.AllSnapshots()
	if len(all) != 1 || all[0].ProjectID != "p1" || all[0].State != StateStopped {
		t.Fatalf("AllSnapshots = %+v", all)
	}
}

// TestInstanceLifecycleExitCode 非零退出 → Failed（退出码入错误信息）。
func TestInstanceLifecycleExitCode(t *testing.T) {
	rec := &eventRecorder{}
	m := NewManager(newWindowsJobAPI(), rec.cb())
	if err := m.Start(StartOptions{
		ProjectID:   "p2",
		ProjectName: "exit-proj",
		Version:     "v0.0.0",
		FrpcExe:     "cmd",
		ConfigPath:  "/c exit 5",
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	in := m.mustInstance("p2")
	snap := waitState(t, in, StateFailed, 5*time.Second)
	if snap.ExitCode != 5 {
		t.Fatalf("exit code = %d, want 5", snap.ExitCode)
	}
	if !strings.Contains(snap.Error, "5") {
		t.Fatalf("error %q should mention exit code", snap.Error)
	}

	// 事件序列须包含 running 与 failed
	states := rec.snapshotStates()
	seen := map[State]bool{}
	for _, s := range states {
		seen[s.State] = true
	}
	if !seen[StateRunning] || !seen[StateFailed] {
		t.Fatalf("event sequence missing running/failed: %v", states)
	}
}

// TestInstanceStop 手动停止 → stopped + "已手动停止"，且幂等。
func TestInstanceStop(t *testing.T) {
	rec := &eventRecorder{}
	m := NewManager(newWindowsJobAPI(), rec.cb())
	if err := m.Start(StartOptions{
		ProjectID:   "p3",
		ProjectName: "stop-proj",
		Version:     "v0.0.0",
		FrpcExe:     "cmd",
		ConfigPath:  "/c ping 127.0.0.1 -n 6 > nul",
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	in := m.mustInstance("p3")
	waitState(t, in, StateRunning, 5*time.Second)

	if err := m.Stop("p3"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	snap := waitState(t, in, StateStopped, 5*time.Second)
	if !strings.Contains(snap.Error, "停止") {
		t.Fatalf("error %q should mention manual stop", snap.Error)
	}
	// 幂等
	if err := m.Stop("p3"); err != nil {
		t.Fatalf("second Stop should be idempotent: %v", err)
	}
}

// TestJobAssignFailure JobObject 绑定失败 → Failed（进程被回滚杀死，不留孤儿）。
func TestJobAssignFailure(t *testing.T) {
	rec := &eventRecorder{}
	fake := &fakeJobAPI{assignErr: fmt.Errorf("injected bind failure")}
	m := NewManager(fake, rec.cb())

	if err := m.Start(StartOptions{
		ProjectID:   "p4",
		ProjectName: "bind-fail-proj",
		Version:     "v0.0.0",
		FrpcExe:     "cmd",
		ConfigPath:  "/c echo nope",
	}); err == nil {
		t.Fatal("Start should fail when JobObject assign fails")
	}
	s := m.mustInstance("p4").Snapshot()
	if s.State != StateFailed {
		t.Fatalf("state = %s, want failed", s.State)
	}
	if !strings.Contains(s.Error, "JobObject") {
		t.Fatalf("error %q should mention JobObject", s.Error)
	}
}

// TestJobCreateFailure Job 创建失败同样回滚。
func TestJobCreateFailure(t *testing.T) {
	rec := &eventRecorder{}
	fake := &fakeJobAPI{createErr: fmt.Errorf("injected create failure")}
	m := NewManager(fake, rec.cb())
	if err := m.Start(StartOptions{
		ProjectID:   "p4b",
		ProjectName: "create-fail-proj",
		Version:     "v0.0.0",
		FrpcExe:     "cmd",
		ConfigPath:  "/c echo nope",
	}); err == nil {
		t.Fatal("Start should fail when JobObject create fails")
	}
	if s := m.mustInstance("p4b").Snapshot(); s.State != StateFailed {
		t.Fatalf("state = %s, want failed", s.State)
	}
}

// TestRedactToken 日志脱敏：启动前注入的 secret 不出现在任何日志出口。
func TestRedactToken(t *testing.T) {
	rec := &eventRecorder{}
	m := NewManager(newWindowsJobAPI(), rec.cb())
	if err := m.Start(StartOptions{
		ProjectID:   "p5",
		ProjectName: "redact-proj",
		Version:     "v0.0.0",
		FrpcExe:     "cmd",
		ConfigPath:  "/c echo top_secret_abc123",
		Redact:      []string{"top_secret_abc123"},
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	in := m.mustInstance("p5")
	waitState(t, in, StateStopped, 5*time.Second)

	for _, l := range rec.logLines() {
		if strings.Contains(l, "top_secret_abc123") {
			t.Fatalf("secret leaked in pushed log line: %q", l)
		}
	}
	// 回放日志同样脱敏
	for _, l := range in.Logs(0) {
		if strings.Contains(l, "top_secret_abc123") {
			t.Fatal("secret leaked in replayed logs")
		}
	}
}

// TestHideWindowFlag 子进程必须带 CREATE_NO_WINDOW + HideWindow（防黑窗口弹出）。
func TestHideWindowFlag(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit 0")
	hideWindow(cmd)
	want := &syscall.SysProcAttr{CreationFlags: 0x08000000, HideWindow: true}
	if !reflect.DeepEqual(cmd.SysProcAttr, want) {
		t.Fatalf("SysProcAttr = %+v, want %+v", cmd.SysProcAttr, want)
	}
}

// TestRemove 删除实例后状态与日志不再可查。
func TestRemove(t *testing.T) {
	rec := &eventRecorder{}
	m := NewManager(newWindowsJobAPI(), rec.cb())
	if err := m.Start(StartOptions{
		ProjectID: "gone", ProjectName: "gone-proj", Version: "v0.0.0",
		FrpcExe: "cmd", ConfigPath: "/c echo x",
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	in := m.mustInstance("gone")
	waitState(t, in, StateStopped, 5*time.Second)

	m.Remove("gone")
	if _, ok := m.Snapshot("gone"); ok {
		t.Fatal("Snapshot should miss after Remove")
	}
}
