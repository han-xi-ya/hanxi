package termora

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/termora/instance"
	"hanxi/internal/modules/termora/version"
	"hanxi/internal/platform"
)

// T2 测试加固批（Termora 根包）：既有 service_test.go 钉住了 RemoveVersion
// 守门三态；本文件补齐版本归一、下载同步回执与后台租约入账、冷启动解析
// 自愈、external 接管/confirm-force 归因、信使拉起失败的如实上浮、调用门
// 拒接矩阵。卸载家族语义在 windterm 侧同型核对（见该模块测试）。

// ---------- 测试替身 ----------

type denyGateTR struct{ sentinel error }

func (g denyGateTR) Acquire(string) (func(), error) { return nil, g.sentinel }

type firstAcceptTRGate struct {
	mu       sync.Mutex
	acquires int
	denied   chan struct{}
}

func (g *firstAcceptTRGate) Acquire(string) (func(), error) {
	g.mu.Lock()
	g.acquires++
	n := g.acquires
	g.mu.Unlock()
	if n == 1 {
		return func() {}, nil
	}
	select {
	case g.denied <- struct{}{}:
	default:
	}
	return nil, errors.New("模块已停用：调用门拒接")
}

type fakeProbeTR struct {
	mu      sync.Mutex
	running bool
	info    *platform.ProcInfo
	proc    *fakeProcTR
}

func (p *fakeProbeTR) set(running bool, info *platform.ProcInfo) {
	p.mu.Lock()
	p.running, p.info = running, info
	p.mu.Unlock()
}

func (p *fakeProbeTR) Inspect(context.Context, uint32) (bool, *platform.ProcInfo, error) {
	p.mu.Lock()
	running, info := p.running, p.info
	p.mu.Unlock()
	if p.proc != nil && p.proc.killed() {
		return false, nil, nil
	}
	return running, info, nil
}
func (p *fakeProbeTR) MutexHeld() bool                 { return false }
func (p *fakeProbeTR) WaitForReady(time.Duration) bool { return true }
func (p *fakeProbeTR) FocusMainWindow(uint32) bool     { return false }
func (p *fakeProbeTR) FocusAnyWindow() bool            { return false }

type fakeProcTR struct {
	mu    sync.Mutex
	info  platform.ProcInfo
	kills int
}

func (p *fakeProcTR) killed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kills > 0
}

func (p *fakeProcTR) Query(uint32) (platform.ProcInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info, nil
}

func (p *fakeProcTR) KillVerified(context.Context, platform.VerifyToken, bool) error {
	p.mu.Lock()
	p.kills++
	p.mu.Unlock()
	return nil
}

func (*fakeProcTR) IsProtected(uint32, platform.ProcInfo) bool { return false }

type fakeJobTR struct{}

func (fakeJobTR) Assign(uint32) error            { return nil }
func (fakeJobTR) Close() error                   { return nil }
func (fakeJobTR) Terminate(uint32) error         { return nil }
func (fakeJobTR) SetAllowKillOnClose(bool) error { return nil }

type fakeJobAPITR struct{}

func (fakeJobAPITR) Create() (platform.Job, error) { return fakeJobTR{}, nil }

func newTermoraServiceWithProbe(t *testing.T) (*TermoraService, string, *fakeProbeTR, *fakeProcTR) {
	t.Helper()
	versionsDir := t.TempDir()
	proc := &fakeProcTR{}
	probe := &fakeProbeTR{proc: proc}
	svc := &TermoraService{
		manager:   version.NewManager(versionsDir),
		store:     newTermoraStore(t.TempDir()),
		engine:    instance.NewEngine(fakeJobAPITR{}, proc, probe, instance.Callbacks{}),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}
	return svc, versionsDir, probe, proc
}

func waitTRRosterFree(t *testing.T, svc *TermoraService, v string, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		svc.downloadMu.Lock()
		_, busy := svc.downloads[v]
		svc.downloadMu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s 在飞名册 %s 未销账（残账粘闸）", v, budget)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---------- 版本归一 ----------

func TestTermoraNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.16.1", "v2.16.1"},
		{"v2.16.1", "v2.16.1"},
		{" 2.16.1 ", "v2.16.1"},
		{"", ""},
		{"imported-0123456789ab", "imported-0123456789ab"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// ---------- 下载同步回执与后台入账 ----------

func TestTermoraDownloadSyncReceipts(t *testing.T) {
	svc, versionsDir, _, _ := newTermoraServiceWithProbe(t)
	seedInstalled(t, versionsDir, "2.16.1")

	if state, err := svc.DownloadVersion("2.16.1"); err != nil || state != "already-installed" {
		t.Fatalf("已装版本应回 already-installed，实得 %q err=%v", state, err)
	}
	if len(svc.downloads) != 0 {
		t.Fatalf("already-installed 路径不得登记名册，实得 %v", svc.downloads)
	}

	svc.downloads["v9.9.9"] = struct{}{}
	if state, err := svc.DownloadVersion("9.9.9"); err != nil || state != "in-progress" {
		t.Fatalf("同版本连点应回 in-progress，实得 %q err=%v", state, err)
	}
}

func TestTermoraDownloadBackgroundLeaseBookkeeping(t *testing.T) {
	svc, versionsDir, _, _ := newTermoraServiceWithProbe(t)
	gate := &firstAcceptTRGate{denied: make(chan struct{}, 4)}
	svc.holder.SetGate(gate)

	state, err := svc.DownloadVersion("9.9.9")
	if err != nil || state != "started" {
		t.Fatalf("未装且不在飞应受理 started，实得 %q err=%v", state, err)
	}
	select {
	case <-gate.denied:
	case <-time.After(5 * time.Second):
		t.Fatal("后台下载 5s 内未自行入账（未申请后台租约，drain 数不到它）")
	}
	waitTRRosterFree(t, svc, "v9.9.9", 10*time.Second)

	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("租约拒早退不得落 active，实得 %q", got)
	}
	if installed, err := svc.manager.ListInstalled(); err != nil || len(installed) != 0 {
		t.Fatalf("早退路径不得留版本残骸，实得 %+v err=%v", installed, err)
	}
	ents, rerr := os.ReadDir(versionsDir)
	if rerr != nil || len(ents) != 0 {
		t.Fatalf("versions 根目录应零残骸，实得 %+v err=%v", ents, rerr)
	}
}

// ---------- 冷启动解析（回退取最新 / 损坏自愈落盘 / 空树报错） ----------

func TestTermoraResolveActiveVersion(t *testing.T) {
	svc, versionsDir, _, _ := newTermoraServiceWithProbe(t)
	if _, _, err := svc.resolveActiveVersion(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("空树应如实报错，实得 %v", err)
	}

	seedInstalled(t, versionsDir, "2.16.1")
	seedInstalled(t, versionsDir, "2.17.0")
	v, exe, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != "v2.17.0" {
		t.Fatalf("未指定 active 应取更高版本，实得 %q", v)
	}
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		t.Fatalf("返回路径须真实在场，实得 %q", exe)
	}

	if err := svc.store.SetActive("v9.9.9"); err != nil {
		t.Fatal(err)
	}
	v2, _, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v2 != "v2.17.0" {
		t.Fatalf("坏 active 应回退最新已装，实得 %q", v2)
	}
	reloaded := newTermoraStore(filepath.Dir(svc.store.filePath))
	if got := reloaded.GetActive(); got != "" {
		t.Fatalf("自愈清空未落盘，重启后读到 %q", got)
	}
}

// ---------- external 接管与退出归因 ----------

func TestTermoraQuitExternalRoundTrip(t *testing.T) {
	svc, _, probe, proc := newTermoraServiceWithProbe(t)
	info := &platform.ProcInfo{PID: 4294967290, ExePath: `X:\user\Termora.exe`, StartedAt: time.Now()}
	proc.info = *info
	probe.set(true, info)
	svc.engine.RefreshExternal()
	if !svc.engine.ExternalRunning() {
		t.Fatal("前置：应已接管为 external")
	}

	out, err := svc.Quit(false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "confirm-required" || out.Method != "confirm-required" || !out.External {
		t.Fatalf("首入应回 confirm-required，实得 %+v", out)
	}
	if out.Stopped || out.Forced || out.CloseRequested {
		t.Fatalf("未授权路径必须零动作，实得 %+v", out)
	}
	if out.Risk == "" || out.Risk != out.Message {
		t.Fatalf("风险文案须与消息同源非空，实得 Risk=%q Message=%q", out.Risk, out.Message)
	}
	if proc.kills != 0 {
		t.Fatalf("未授权不得触达终止，实得 kills=%d", proc.kills)
	}

	out2, err := svc.Quit(true)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Method != "forced" || !out2.Stopped || !out2.Forced || out2.CloseRequested {
		t.Fatalf("confirm-force 收口归因应 forced/Stopped/!CloseRequested，实得 %+v", out2)
	}
	if proc.kills != 1 {
		t.Fatalf("恰一次终止调用，实得 %d", proc.kills)
	}
	if got := svc.engine.Snapshot().State; got != instance.StateStopped {
		t.Fatalf("强杀收口后应回 stopped，实得 %s", got)
	}
}

func TestTermoraQuitExternalUnidentifiedRefuses(t *testing.T) {
	svc, _, probe, proc := newTermoraServiceWithProbe(t)
	probe.set(true, nil)
	out, err := svc.Quit(true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != "probe-missing-pid" || out.Stopped || out.Forced {
		t.Fatalf("身份不明必须拒执行，实得 %+v", out)
	}
	if proc.kills != 0 {
		t.Fatal("拒执行不得触达终止")
	}
}

func TestTermoraQuitNotManaged(t *testing.T) {
	svc, _, _, proc := newTermoraServiceWithProbe(t)
	out, err := svc.Quit(false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != "not-managed" || out.Stopped || out.External {
		t.Fatalf("静止会话应归因 not-managed，实得 %+v", out)
	}
	if proc.kills != 0 {
		t.Fatal("not-managed 不得触达终止")
	}
}

// ---------- OpenWindow 信使编排 ----------

func TestTermoraOpenWindowExternalWithoutExePath(t *testing.T) {
	svc, _, probe, _ := newTermoraServiceWithProbe(t)
	probe.set(true, nil) // external 在场但路径不可得
	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "external-unreachable" || !out.External {
		t.Fatalf("取不到程序路径应如实回指引，实得 %+v err=%v", out, err)
	}
}

func TestTermoraOpenWindowMessengerFailurePropagates(t *testing.T) {
	svc, _, probe, _ := newTermoraServiceWithProbe(t)
	probe.set(true, &platform.ProcInfo{
		PID: 4294967290, ExePath: `X:\no\such\Termora.exe`, StartedAt: time.Now(),
	})
	_, err := svc.OpenWindow()
	// 信使拉起不存在的 exe：错误必须如实上抛，不得谎报"已激活"。
	if err == nil {
		t.Fatal("信使拉起失败应如实报错")
	}
}

// ---------- 调用门拒接矩阵 ----------

func TestTermoraGateRefusesRPCSurface(t *testing.T) {
	svc, _, _, _ := newTermoraServiceWithProbe(t)
	sentinel := errors.New("模块已停用：调用门拒接")
	svc.holder.SetGate(denyGateTR{sentinel: sentinel})

	if _, err := svc.ListReleases(); !errors.Is(err, sentinel) {
		t.Errorf("ListReleases 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.ListInstalledVersions(); !errors.Is(err, sentinel) {
		t.Errorf("ListInstalledVersions 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.DownloadVersion("2.17.0"); !errors.Is(err, sentinel) {
		t.Errorf("DownloadVersion 门拒应上抛原错，实得 %v", err)
	}
	if len(svc.downloads) != 0 {
		t.Errorf("门拒路径不得登记名册，实得 %v", svc.downloads)
	}
	if _, err := svc.ImportLocal(`C:\nonexistent`); !errors.Is(err, sentinel) {
		t.Errorf("ImportLocal 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.RemoveVersion("2.17.0"); !errors.Is(err, sentinel) {
		t.Errorf("RemoveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.SetActiveVersion("2.17.0"); !errors.Is(err, sentinel) {
		t.Errorf("SetActiveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetActiveVersion(); !errors.Is(err, sentinel) {
		t.Errorf("GetActiveVersion 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetStatus(); !errors.Is(err, sentinel) {
		t.Errorf("GetStatus 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.OpenWindow(); !errors.Is(err, sentinel) {
		t.Errorf("OpenWindow 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.Quit(false); !errors.Is(err, sentinel) {
		t.Errorf("Quit 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.GetFollowOnExit(); !errors.Is(err, sentinel) {
		t.Errorf("GetFollowOnExit 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.SetFollowOnExit(true); !errors.Is(err, sentinel) {
		t.Errorf("SetFollowOnExit 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.Shutdown(); !errors.Is(err, sentinel) {
		t.Errorf("Shutdown(RPC) 门拒应上抛原错，实得 %v", err)
	}
	if _, err := svc.RepositoryURL(); !errors.Is(err, sentinel) {
		t.Errorf("RepositoryURL 门拒应上抛原错，实得 %v", err)
	}
	if err := svc.OpenRepository(); !errors.Is(err, sentinel) {
		t.Errorf("OpenRepository 门拒应上抛原错（plat 未接线，越过门即 panic），实得 %v", err)
	}
}
