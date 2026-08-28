package instance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hubkit/internal/platform"
)

type fakeJob struct {
	assigned   uint32
	allow      *bool
	terminated bool
}

func (j *fakeJob) Assign(pid uint32) error                { j.assigned = pid; return nil }
func (j *fakeJob) Close() error                           { return nil }
func (j *fakeJob) Terminate(uint32) error                 { j.terminated = true; return nil }
func (j *fakeJob) SetAllowKillOnClose(enabled bool) error { j.allow = &enabled; return nil }

type fakeJobAPI struct{ job *fakeJob }

func (a fakeJobAPI) Create() (platform.Job, error) { return a.job, nil }

type fakeProcessAPI struct{ info platform.ProcInfo }

func (p *fakeProcessAPI) Query(pid uint32) (platform.ProcInfo, error) {
	if p.info.PID == 0 {
		p.info.PID = pid
	}
	return p.info, nil
}
func (*fakeProcessAPI) KillVerified(context.Context, platform.VerifyToken, bool) error { return nil }
func (*fakeProcessAPI) IsProtected(uint32, platform.ProcInfo) bool                     { return false }

func TestStartKeepsIndependentJobAndTracksProcess(t *testing.T) {
	cmdExe := os.Getenv("COMSPEC")
	if cmdExe == "" {
		t.Skip("COMSPEC unavailable")
	}
	job := &fakeJob{}
	proc := &fakeProcessAPI{info: platform.ProcInfo{ExePath: cmdExe, StartedAt: time.Now()}}
	engine := NewEngine(fakeJobAPI{job: job}, proc, Callbacks{})
	// cmd.exe 无参数会立即退出；本测试只锁定启动期 Job 契约。
	_ = engine.Start(StartOptions{Version: "test", Exe: cmdExe})
	if job.assigned == 0 {
		t.Fatal("job was not assigned")
	}
	if job.allow == nil || *job.allow {
		t.Fatal("kill-on-close must be disabled")
	}
}

func TestQuitNotManagedIsNoop(t *testing.T) {
	engine := NewEngine(fakeJobAPI{job: &fakeJob{}}, &fakeProcessAPI{}, Callbacks{})
	result, err := engine.Quit()
	if err != nil || result.Method != "not-managed" || result.Stopped {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestVerifyTokenRejectsPathMismatch(t *testing.T) {
	proc := &fakeProcessAPI{info: platform.ProcInfo{PID: 10, ExePath: filepath.Join(t.TempDir(), "other.exe"), StartedAt: time.Now()}}
	engine := NewEngine(fakeJobAPI{job: &fakeJob{}}, proc, Callbacks{})
	err := engine.verifyToken(platform.VerifyToken{PID: 10, ExePath: filepath.Join(t.TempDir(), "Snipaste.exe"), StartedAt: proc.info.StartedAt})
	if err != platform.ErrTokenMismatch {
		t.Fatalf("err=%v", err)
	}
}

func TestQuitForcesOwnedJobAfterGrace(t *testing.T) {
	job := &fakeJob{}
	started := time.Now()
	proc := &fakeProcessAPI{info: platform.ProcInfo{PID: 42, ExePath: `C:\HubKit\Snipaste.exe`, StartedAt: started}}
	engine := NewEngine(fakeJobAPI{job: job}, proc, Callbacks{})
	engine.state = StateRunning
	engine.pid = 42
	engine.job = job
	engine.token = platform.VerifyToken{PID: 42, ExePath: proc.info.ExePath, StartedAt: started}
	engine.waitDone = make(chan struct{})
	engine.closeByPID = func(pid uint32) int {
		if pid != 42 {
			panic(fmt.Sprintf("pid=%d", pid))
		}
		return 1
	}
	old := closeGracePeriod
	closeGracePeriod = time.Millisecond
	defer func() { closeGracePeriod = old }()
	result, err := engine.Quit()
	if err != nil || !result.Forced || result.Method != "forced-job" || !job.terminated {
		t.Fatalf("result=%#v err=%v job=%#v", result, err, job)
	}
}
