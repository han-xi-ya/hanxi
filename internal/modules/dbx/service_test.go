package dbx

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/dbx/instance"
	"hanxi/internal/modules/dbx/version"
	"hanxi/internal/platform"
)

// ---------- 测试用假件（版本引擎 / 探针 / Job 注入；零真实 DBX、零真实网络） ----------

type fakeManager struct {
	mu          sync.Mutex
	installed   []version.DBXVersionInfo
	resolveErr  map[string]error
	removeCalls []string
	drifted     bool
	driftNote   string
}

func (f *fakeManager) ListRemote() ([]version.DBXRelease, error) { return nil, nil }

func (f *fakeManager) ListInstalled() ([]version.DBXVersionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.installed, nil
}

func (f *fakeManager) DownloadContext(ctx context.Context, _, targetVersion string, progress func(version.DownloadProgress)) error {
	return errors.New("fakeManager 不承载真实下载（测试必须注入 downloadDriver）")
}

func (f *fakeManager) ImportLocal(string) (version.DBXVersionInfo, error) {
	return version.DBXVersionInfo{}, errors.New("fake import")
}

func (f *fakeManager) Remove(targetVersion string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeCalls = append(f.removeCalls, targetVersion)
	return nil
}

func (f *fakeManager) ResolveExe(targetVersion string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.resolveErr[targetVersion]; err != nil {
		return "", err
	}
	for _, v := range f.installed {
		if v.Version == targetVersion {
			return v.ExePath, nil
		}
	}
	return "", errors.New("版本未安装: " + targetVersion)
}

func (f *fakeManager) VerifyLedger(string) (bool, string) { return f.drifted, f.driftNote }

func (f *fakeManager) CheckUpdate(context.Context) (string, string, bool, error) {
	return "", "", false, nil
}

type fakeProbe struct {
	running bool
}

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.running }
func (p *fakeProbe) IsMainWindowOpen() bool          { return p.running }
func (p *fakeProbe) FindPIDs() []uint32              { return nil }

type fakeJobAPI struct{}

func (f *fakeJobAPI) Create() (platform.Job, error) { return fakeJob{}, nil }

type fakeJob struct{}

func (fakeJob) Assign(pid uint32) error          { return nil }
func (fakeJob) Close() error                     { return nil }
func (fakeJob) Terminate(code uint32) error      { return nil }
func (fakeJob) SetAllowKillOnClose(b bool) error { return nil }

// newTestDBXService 装配单测最小 service（termora harness 同法：结构体字面量
// 注入；调用门未挂 = Enter/EnterBackground 直通，ops 账本未注入 = 事务降级
// no-op；engine 挂假 Job/假探针，绝不开真进程——仅信使路径刻意保留真 spawn，
// 以 cmd.exe 短命替身承载）。
func newTestDBXService(t *testing.T, probe *fakeProbe) *DBXService {
	t.Helper()
	if probe == nil {
		probe = &fakeProbe{}
	}
	return &DBXService{
		manager:   &fakeManager{},
		store:     newDBXStore(t.TempDir()),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
		dataDir:   filepath.Join(t.TempDir(), "dbx"),
		engine:    instance.NewEngine(&fakeJobAPI{}, probe, instance.Callbacks{}),
	}
}

// ---------- 下载事件时序（termora 2ac9b3b 同型病灶回归护栏） ----------

// downloadReceipt 下载事件票据：stage/version 为回执载荷，activeAtBroadcast
// 由探针在广播位（downloadProbe 与生产 Wails Emit 同一点位）于下载 goroutine
// 内同步快照——即"前端此刻复刷 GetActiveVersion 会读到什么"的实况。
type downloadReceipt struct {
	stage             string
	version           string
	activeAtBroadcast string
}

// 锁死 termora 2ac9b3b 同型病灶的修复：前端共享 store 收到 done 成功回执即
// 复刷 GetActiveVersion，"首装自动设使用"的落账必须先于事件广播完成（断言取
// 广播位快照，非事后补读——事后读会与被测代码赛跑，负序回归可能漏网）。假驱动
// 经 seam 隔离网络（绝不真下载），事件时序与 journal/租约全走生产闭包实现。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	svc := newTestDBXService(t, nil)
	seen := make(chan downloadReceipt, 32)
	svc.downloadProbe = func(p version.DownloadProgress) {
		seen <- downloadReceipt{stage: p.Stage, version: p.Version, activeAtBroadcast: svc.store.GetActive()}
	}
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(version.DownloadProgress)) error {
		emit(version.DownloadProgress{Version: targetVersion, Stage: "downloading", Done: 1, Total: 2})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "extract"})
		emit(version.DownloadProgress{Version: targetVersion, Stage: "done", Done: 100, Total: 100})
		return nil
	}

	// DownloadVersion 归一化 v 前缀，落账与回执都应按归一化后的口径核对。
	if state, err := svc.DownloadVersion("1.5.3"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v1.5.3" {
		t.Fatalf("done 回执版本应为归一化的 v1.5.3，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v1.5.3" {
		t.Fatalf("done 成功回执广播位上 active 应已落账为 v1.5.3，实得 %q", rr.activeAtBroadcast)
	}
	// 已有使用版本时不得被新下载覆盖（落账条件仅"未设使用"，语义随迁移保持不变）。
	if state, err := svc.DownloadVersion("1.6.0"); err != nil || state != "started" {
		t.Fatalf("第二次 DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitDoneReceipt(t, seen); rr.version != "v1.6.0" {
		t.Fatalf("第二次下载应广播 v1.6.0 的 done 回执，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "v1.5.3" {
		t.Fatalf("active 已有值时广播位不应被后续下载改写，实得 %q", rr.activeAtBroadcast)
	}
}

func waitDoneReceipt(t *testing.T, seen <-chan downloadReceipt) downloadReceipt {
	t.Helper()
	for {
		select {
		case r := <-seen:
			if r.stage == "done" {
				return r
			}
		case <-time.After(5 * time.Second):
			t.Fatal("等待下载 done 事件超时")
			return downloadReceipt{}
		}
	}
}

// ---------- 版本解析与卸载清账 ----------

func TestResolveActiveVersionPrefersAndSelfHeals(t *testing.T) {
	svc := newTestDBXService(t, nil)
	fm := svc.manager.(*fakeManager)
	fm.installed = []version.DBXVersionInfo{
		{Version: "v1.4.0", ExePath: `X:\dbx_1.4.0\DBX.exe`},
		{Version: "v1.10.0", ExePath: `X:\dbx_1.10.0\DBX.exe`},
		{Version: "v1.9.0", ExePath: `X:\dbx_1.9.0\DBX.exe`},
	}

	// 设定版本有效 → 直取
	if err := svc.store.SetActive("v1.4.0"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	v, exe, err := svc.resolveActiveVersion()
	if err != nil || v != "v1.4.0" || exe != `X:\dbx_1.4.0\DBX.exe` {
		t.Fatalf("active 优先失败: %v %v %v", v, exe, err)
	}

	// 设定版本失效（被卸载/损坏）→ 清空自愈回退最新已装（数值段比较：1.10.0 > 1.9.0）
	if err := svc.store.SetActive("v9.9.9"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	v, _, err = svc.resolveActiveVersion()
	if err != nil || v != "v1.10.0" {
		t.Fatalf("应回退数值最新 v1.10.0: %v %v", v, err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("失效设定应被清空自愈, got %q", got)
	}
}

func TestRemoveVersionClearsActiveAndRunningRejectedShape(t *testing.T) {
	svc := newTestDBXService(t, nil)
	fm := svc.manager.(*fakeManager)
	if err := svc.store.SetActive("v1.5.3"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := svc.RemoveVersion("1.5.3"); err != nil {
		t.Fatalf("RemoveVersion: %v", err)
	}
	if len(fm.removeCalls) != 1 || fm.removeCalls[0] != "v1.5.3" {
		t.Fatalf("应归一化 v 前缀后委托 manager: %v", fm.removeCalls)
	}
	if svc.store.GetActive() != "" {
		t.Fatal("卸掉设定版本后 activeVersion 应清空")
	}
}

// ---------- 退出编排（external 治理口径 + 托管备份 worker 残留如实呈现） ----------

func TestQuitExternalInstanceNotKilled(t *testing.T) {
	svc := newTestDBXService(t, &fakeProbe{running: true})
	// 探针判在场 → RefreshExternal 后快照 external：Quit 不越权、给指引
	svc.engine.RefreshExternal()
	out, err := svc.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if out.Stopped || !out.External {
		t.Fatalf("external 场应零越权: %+v", out)
	}
	if !strings.Contains(out.Message, "外部") {
		t.Fatalf("external 指引缺失: %q", out.Message)
	}
}

// TestQuitResidualWorkerHint 自有实例退出后，"托管备份"计划任务残留的同名
// 进程（探针翻真模拟）必须让 Quit 回执翻出 external + 设置内关闭指引——
// 外部实例治理口径的回归位。
func TestQuitResidualWorkerHint(t *testing.T) {
	probe := &fakeProbe{running: false}
	svc := newTestDBXService(t, probe)
	// 时序造景：Quit 入口读内核快照（stopped，不经探针）→ 自有实例收口 →
	// 复检位 RefreshExternal 读探针——此刻同名残留（worker/外部实例）翻真。
	probe.running = true
	out, err := svc.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if !out.Stopped || !out.External {
		t.Fatalf("残留 worker 场应 stopped+external 双真: %+v", out)
	}
	if !strings.Contains(out.Message, "托管备份") || !strings.Contains(out.Message, "不强杀") {
		t.Fatalf("退出回执缺 worker 指引: %q", out.Message)
	}
}

func TestQuitCleanOutcome(t *testing.T) {
	svc := newTestDBXService(t, &fakeProbe{running: false})
	out, err := svc.Quit()
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if !out.Stopped || out.External || out.Message != "DBX 已退出" {
		t.Fatalf("干净退出回执异常: %+v", out)
	}
}

// ---------- GetStatus 复合投影 ----------

func TestGetStatusCompositesDriftAndDataDir(t *testing.T) {
	svc := newTestDBXService(t, &fakeProbe{running: false})
	fm := svc.manager.(*fakeManager)
	fm.drifted, fm.driftNote = true, "exe mtime 新于落位账"
	if err := svc.store.SetActive("v1.5.3"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := svc.store.SetDataDirInjected(true); err != nil {
		t.Fatalf("SetDataDirInjected: %v", err)
	}

	st, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !st.Drifted || st.DriftNote != "exe mtime 新于落位账" {
		t.Fatalf("漂移投影失败: %+v", st)
	}
	if st.DataDir != svc.dataDir || !st.DataDirInjected {
		t.Fatalf("数据改道投影失败: %+v", st)
	}
}

// ---------- OpenWindow external 信使编排 ----------

func TestOpenWindowExternalSpawnsMessenger(t *testing.T) {
	svc := newTestDBXService(t, &fakeProbe{running: true})
	fm := svc.manager.(*fakeManager)
	// 信使载体：真实 cmd.exe 短命进程（ccswitch TestOpenWindowMessenger 同口径，
	// 裸启读 EOF 即退、不阻塞不遗留）
	cmdExe, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Skipf("cmd.exe 不可用: %v", err)
	}
	fm.installed = []version.DBXVersionInfo{{Version: "v1.5.3", ExePath: cmdExe}}

	out, err := svc.OpenWindow()
	if err != nil {
		t.Fatalf("OpenWindow external: %v", err)
	}
	if out.Action != "external-opened" || !out.External {
		t.Fatalf("external 唤窗回执异常: %+v", out)
	}
}

// ---------- 披露与上游直达 ----------

func TestMetaHintsDisclosures(t *testing.T) {
	svc := newTestDBXService(t, nil)
	hints, err := svc.MetaHints()
	if err != nil {
		t.Fatalf("MetaHints: %v", err)
	}
	joined := strings.Join(hints, "\n")
	for _, needle := range []string{svc.dataDir, "不删数据，除非您明示", "portable.dbx", "minisign", "托管备份", "WebView2"} {
		if !strings.Contains(joined, needle) {
			t.Errorf("metaHints 缺披露位 %q:\n%s", needle, joined)
		}
	}
}

func TestRepositoryURLIsUpstream(t *testing.T) {
	svc := newTestDBXService(t, nil)
	url, err := svc.RepositoryURL()
	if err != nil {
		t.Fatalf("RepositoryURL: %v", err)
	}
	if url != "https://github.com/t8y2/dbx" {
		t.Fatalf("上游地址异常: %s", url)
	}
}

// ---------- 联动开关 ----------

func TestFollowOnExitRoundTrip(t *testing.T) {
	svc := newTestDBXService(t, nil)
	if v, err := svc.GetFollowOnExit(); err != nil || v {
		t.Fatalf("默认应 false: %v %v", v, err)
	}
	if err := svc.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	if v, err := svc.GetFollowOnExit(); err != nil || !v {
		t.Fatalf("回转失败: %v %v", v, err)
	}
}

// ---------- Shutdown 纪律 ----------

// TestShutdownRespectsFollowOnExitSwitch 联动关闭（默认）时 shutdown 绝不动
// 引擎（工具实例存活跨 Hanxi 生命周期）；开启后 shutdown 走 Stop 通道。
// 假 JobAPI 下 Terminate 不真杀，断言收口不报错、轮询协程退出。
func TestShutdownRespectsFollowOnExitSwitch(t *testing.T) {
	svc := newTestDBXService(t, nil)
	svc.activate()
	svc.activate() // 幂等
	svc.shutdown() // followOnExit=false：仅停 watcher
	svc.shutdown() // 重复收口不 panic

	s2 := newTestDBXService(t, nil)
	if err := s2.store.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2.activate()
	s2.shutdown() // 联动开启：Stop 自有实例（静止态幂等）
}
