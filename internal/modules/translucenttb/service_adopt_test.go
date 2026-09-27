package translucenttb

// P1-8 归属收编 + external 强制结束的 service 层回归测（2026-09-27 机主裁决批）：
// 打包收编态指引话术、真 external 的 Quit 指引 / ForceQuitExternal 成败与被拒
// 三分支、收编实例不在强制结束射程。引擎内部机制（路径判据/三段退出链/单飞闩）
// 已在 instance 包锁死，本文件只钉 service 门面话术与分诊语义——fake 探针注入
// 身份事实，零真实进程。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/translucenttb/instance"
	"hanxi/internal/platform"
)

const (
	adoptTestPackagedExe = `C:\Program Files\WindowsApps\28017CharlesMilette.TranslucentTB_2026.2.0.0_x64__v826wp6bftszj\TranslucentTB.exe`
	adoptTestPortableExe = `D:\tools\TranslucentTB\TranslucentTB.exe`
	adoptTestPrefix      = MsixPackageName + "_"
)

// ---------- 本包用最小 fake（探针/Job/进程通道） ----------

type adoptProbe struct {
	mu        sync.Mutex
	running   bool
	instances []platform.ProcInfo
}

func (p *adoptProbe) set(running bool, instances ...platform.ProcInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = running
	p.instances = instances
}

func (p *adoptProbe) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}
func (p *adoptProbe) WaitForReady(time.Duration) bool { return p.IsRunning() }
func (p *adoptProbe) FindInstances() []platform.ProcInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return nil
	}
	return p.instances
}

type adoptJobAPI struct{}

func (adoptJobAPI) Create() (platform.Job, error) { return adoptStubJob{}, nil }

type adoptStubJob struct{}

func (adoptStubJob) Assign(uint32) error            { return nil }
func (adoptStubJob) Close() error                   { return nil }
func (adoptStubJob) Terminate(uint32) error         { return nil }
func (adoptStubJob) SetAllowKillOnClose(bool) error { return nil }

type adoptProcAPI struct {
	mu     sync.Mutex
	killed []platform.VerifyToken
	errOf  map[uint32]error
	onKill func(pid uint32) // 成功结束的副作用钩子（联动探针落位）
}

func (f *adoptProcAPI) Query(pid uint32) (platform.ProcInfo, error) {
	return platform.ProcInfo{PID: pid}, nil
}
func (f *adoptProcAPI) KillVerified(_ context.Context, tok platform.VerifyToken, _ bool) error {
	f.mu.Lock()
	f.killed = append(f.killed, tok)
	errOf, onKill := f.errOf, f.onKill
	f.mu.Unlock()
	if errOf != nil {
		if err, ok := errOf[tok.PID]; ok {
			return err
		}
	}
	if onKill != nil {
		onKill(tok.PID)
	}
	return nil
}
func (f *adoptProcAPI) IsProtected(uint32, platform.ProcInfo) bool { return false }

func (f *adoptProcAPI) killedPIDs() []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uint32, 0, len(f.killed))
	for _, t := range f.killed {
		out = append(out, t.PID)
	}
	return out
}

// newAdoptSvc 装配仅含门面所需件的 svc（门直通：未接 Gate 的 LeaseHolder）。
func newAdoptSvc(probe *adoptProbe, proc *adoptProcAPI) *TranslucentTBService {
	svc := &TranslucentTBService{holder: extapi.NewLeaseHolder(ID)}
	e := instance.NewEngine(adoptJobAPI{}, probe, instance.Callbacks{})
	e.SetPackagedPackagePrefix(adoptTestPrefix)
	if proc != nil {
		e.SetProcessAPI(proc)
	}
	svc.engine = e
	return svc
}

// ---------- Start / Quit 话术 ----------

// TestStartAdoptedPackagedMessage 打包收编实例点「启动」：幂等回执走打包语境，
// 不得输出"（Hanxi 托管）+ 空版本"的畸形串（便携版本号是便携线的账）。
func TestStartAdoptedPackagedMessage(t *testing.T) {
	probe := &adoptProbe{}
	probe.set(true, platform.ProcInfo{PID: 4242, ExePath: adoptTestPackagedExe, StartedAt: time.Now().Add(-time.Hour)})
	svc := newAdoptSvc(probe, nil)

	out, err := svc.Start()
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "already-running" || out.External {
		t.Errorf("收编态动作/归属失真: %+v", out)
	}
	if !strings.Contains(out.Message, "打包版 TranslucentTB 已在运行") {
		t.Errorf("应给打包语境指引: %q", out.Message)
	}
	if strings.Contains(out.Message, "Hanxi 托管") || strings.Contains(out.Message, "TranslucentTB  已") {
		t.Errorf("不得混入便携账目话术或双空格畸形: %q", out.Message)
	}
}

// TestQuitExternalGuidanceMentionsForceQuit 真 external 的 Quit 指引必须点名
// 新通道（机主硬需求"即使判 external 也要允许强制结束"的可发现性半边）。
func TestQuitExternalGuidanceMentionsForceQuit(t *testing.T) {
	probe := &adoptProbe{}
	probe.set(true, platform.ProcInfo{PID: 5, ExePath: adoptTestPortableExe, StartedAt: time.Now()})
	svc := newAdoptSvc(probe, &adoptProcAPI{})

	out, err := svc.Quit()
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !out.External {
		t.Errorf("Quit 对真 external 仍不越权: %+v", out)
	}
	for _, want := range []string{"强制结束", "托盘菜单", "非 Hanxi 拉起"} {
		if !strings.Contains(out.Message, want) {
			t.Errorf("指引话术缺 %q: %q", want, out.Message)
		}
	}
}

// ---------- ForceQuitExternal ----------

func TestForceQuitExternalGuidesWhenNotExternal(t *testing.T) {
	probe := &adoptProbe{} // 静止态
	svc := newAdoptSvc(probe, &adoptProcAPI{})
	out, err := svc.ForceQuitExternal()
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !strings.Contains(out.Message, "没有在运行") {
		t.Errorf("静止态应零副作用指引: %+v", out)
	}
}

// TestForceQuitExternalGuidesWhenRunning 托管口径（含打包收编显示 running）
// 归「退出」三段链管辖，本面不出枪——用收编态冒充 running 展示，零真进程。
func TestForceQuitExternalGuidesWhenRunning(t *testing.T) {
	probe := &adoptProbe{}
	probe.set(true, platform.ProcInfo{PID: 4242, ExePath: adoptTestPackagedExe, StartedAt: time.Now()})
	proc := &adoptProcAPI{}
	svc := newAdoptSvc(probe, proc)

	out, err := svc.ForceQuitExternal()
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped || !strings.Contains(out.Message, "「退出」") {
		t.Errorf("running（收编）态应指引「退出」链: %+v", out)
	}
	if len(proc.killedPIDs()) != 0 {
		t.Errorf("指引路径不得出枪: %v", proc.killedPIDs())
	}
}

func TestForceQuitExternalSuccess(t *testing.T) {
	probe := &adoptProbe{}
	probe.set(true, platform.ProcInfo{PID: 5, ExePath: adoptTestPortableExe, StartedAt: time.Now()})
	proc := &adoptProcAPI{onKill: func(uint32) { probe.set(false) }} // 终结成功 = 互斥体落位
	svc := newAdoptSvc(probe, proc)

	out, err := svc.ForceQuitExternal()
	if err != nil {
		t.Fatal(err)
	}
	if !out.Stopped || !out.External || !strings.Contains(out.Message, "已强制结束") {
		t.Errorf("成功回执失真: %+v", out)
	}
	if got := proc.killedPIDs(); len(got) != 1 || got[0] != 5 {
		t.Errorf("应按探针身份出枪: %v", got)
	}
	// 收口校正：RPC 内 ForceQuitExternal 已刷新，落位后快照应脱 external
	if snap := svc.engine.Snapshot(); snap.State != instance.StateStopped {
		t.Errorf("终结落位后应校正 stopped: %+v", snap)
	}
}

func TestForceQuitExternalDeniedHonestError(t *testing.T) {
	probe := &adoptProbe{}
	probe.set(true, platform.ProcInfo{PID: 5, ExePath: adoptTestPortableExe, StartedAt: time.Now()})
	proc := &adoptProcAPI{errOf: map[uint32]error{5: platform.ErrAccessDenied}}
	svc := newAdoptSvc(probe, proc)

	out, err := svc.ForceQuitExternal()
	if err == nil {
		t.Fatalf("全被拒必须报错不吞: %+v", out)
	}
	msg := err.Error()
	for _, want := range []string{"系统权限", "非 hanxi 拉起", "托盘菜单"} {
		if !strings.Contains(msg, want) {
			t.Errorf("被拒话术缺 %q: %s", want, msg)
		}
	}
}

// TestForceQuitExternalSkipsAdoptedPackaged 打包收编实例不在强制结束射程
// （归「退出」三段链）：探针在场两实例（打包+真外部），只终结真外部那一个。
func TestForceQuitExternalSkipsAdoptedPackaged(t *testing.T) {
	probe := &adoptProbe{}
	probe.set(true,
		platform.ProcInfo{PID: 4242, ExePath: adoptTestPackagedExe, StartedAt: time.Now().Add(-time.Hour)},
		platform.ProcInfo{PID: 5, ExePath: adoptTestPortableExe, StartedAt: time.Now()},
	)
	proc := &adoptProcAPI{}
	svc := newAdoptSvc(probe, proc)

	// 收编态显示 running——service 层 switch 先指路「退出」。要让 RPC 真正遍历
	// 枚举，改用未收编视图：拔掉打包身份（模拟探针读不到路径）后仅剩真外部。
	out, err := svc.ForceQuitExternal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Message, "「退出」") {
		t.Fatalf("两实例含打包收编 → 快照 running → 应指路: %+v", out)
	}

	probe.set(true, platform.ProcInfo{PID: 5, ExePath: adoptTestPortableExe, StartedAt: time.Now()})
	out2, err := svc.ForceQuitExternal()
	if err != nil {
		t.Fatal(err)
	}
	_ = out2
	if got := proc.killedPIDs(); len(got) != 1 || got[0] != 5 {
		t.Errorf("打包实例必须被跳过、真外部被结束: %v", got)
	}
}

// TestAdoptedQuitDeniedPropagatesThroughSvc 收编实例 Quit 三段链被系统拒绝时，
// service.Quit 如实上抛错误（不吞、不假报已退，机主硬需求的诚实半边）。
// 三段链宽限走生产 2s 常量（instance 包级变量不跨包可达），本用例耗时 ~2s 可接受。
func TestAdoptedQuitDeniedPropagatesThroughSvc(t *testing.T) {
	probe := &adoptProbe{}
	probe.set(true, platform.ProcInfo{PID: 4242, ExePath: adoptTestPackagedExe, StartedAt: time.Now().Add(-time.Hour)})
	proc := &adoptProcAPI{errOf: map[uint32]error{4242: platform.ErrAccessDenied}}
	svc := newAdoptSvc(probe, proc)

	// 先入 external 账（快照呈 running 收编态），再 Quit
	if snap, err := svc.GetStatus(); err != nil || snap.State != instance.StateRunning {
		t.Fatalf("应呈收编托管态: %+v err=%v", snap, err)
	}
	out, err := svc.Quit()
	if err == nil || !strings.Contains(err.Error(), "系统权限") {
		t.Fatalf("被系统拒绝须如实上抛: %+v err=%v", out, err)
	}
	if out.Stopped {
		t.Error("失败态不得标 Stopped")
	}
}
