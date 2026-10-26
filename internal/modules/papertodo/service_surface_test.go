// service_surface_test.go 钉死 papertodo service 层的同步可观察面：
// DownloadVersion 入口归一与拦截（不启动后台下载的路径）、卸载"只删程序
// 保留便签数据"的盘上契约、导入本地全链、窗口控制在静止/外部态的如实回执、
// OpenDir 校验、StateFailed → 通知 Hub 的收口。全部用临时目录 + 假探针/假
// JobAPI 构造，不触碰真实网络、真实进程与真实互斥体。
package papertodo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/papertodo/instance"
	"hanxi/internal/modules/papertodo/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
)

// ---------- 测试夹具 ----------

type fakeProbe struct{ running bool }

func (p *fakeProbe) IsRunning() bool                 { return p.running }
func (p *fakeProbe) WaitForReady(time.Duration) bool { return p.running }

type fakeJob struct{}

func (fakeJob) Assign(uint32) error            { return nil }
func (fakeJob) Close() error                   { return nil }
func (fakeJob) Terminate(uint32) error         { return nil }
func (fakeJob) SetAllowKillOnClose(bool) error { return nil }

type fakeJobAPI struct{}

func (fakeJobAPI) Create() (platform.Job, error) { return fakeJob{}, nil }

type countGate struct {
	deny     error
	acquired atomic.Int64
	released atomic.Int64
}

func (g *countGate) Acquire(string) (func(), error) {
	g.acquired.Add(1)
	if g.deny != nil {
		return nil, g.deny
	}
	return func() { g.released.Add(1) }, nil
}

type svcFixture struct {
	svc      *PaperTodoService
	probe    *fakeProbe
	install  string // 托管目录（versions/papertodo）
	versions string
	gate     *countGate
}

func newSvcFixture(t *testing.T) *svcFixture {
	t.Helper()
	root := t.TempDir()
	versions := filepath.Join(root, "versions")
	probe := &fakeProbe{}
	gate := &countGate{}
	holder := extapi.NewLeaseHolder(ID)
	holder.SetGate(gate)
	svc := &PaperTodoService{
		manager: version.NewManager(versions),
		store:   newPapertodoStore(filepath.Join(root, "state")),
		engine:  instance.NewEngine(fakeJobAPI{}, probe, instance.Callbacks{}),
		holder:  holder,
	}
	return &svcFixture{svc: svc, probe: probe, install: filepath.Join(versions, "papertodo"), versions: versions, gate: gate}
}

// simInstall 伪造一份托管安装：exe 占位文件 + hanxi-meta + 便签数据。
func (f *svcFixture) simInstall(t *testing.T, tag, variant string) {
	t.Helper()
	if err := os.MkdirAll(f.install, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.install, "PaperTodo.exe"), []byte("not-a-real-pe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.install, "data.json"), []byte(`{"notes":["mine"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{"tag": tag, "installedAt": "2026-01-01 00:00:00"}
	if variant != "" {
		meta["variant"] = variant
	}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(f.install, "hanxi-meta.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *svcFixture) toExternal(t *testing.T) {
	t.Helper()
	f.probe.running = true
	f.svc.engine.RefreshExternal()
	if snap := f.svc.engine.Snapshot(); snap.State != instance.StateExternal {
		t.Fatalf("夹具未进入 external 态: %s", snap.State)
	}
}

// ---------- DownloadVersion：同步入口面 ----------

func TestDownloadVersionNormalizesAndGates(t *testing.T) {
	f := newSvcFixture(t)
	f.simInstall(t, "v9.9.9", version.VariantSelfContained)

	cases := []struct {
		name    string
		target  string
		variant string
		want    string // 返回值（"" 表示只查错误）
		wantErr string
	}{
		{"tag 归一：无前缀补 v", "9.9.9", "", "already-installed", ""},
		{"tag 归一：带前缀不重复", "v9.9.9", "", "already-installed", ""},
		{"tag 归一：去首尾空白", "  v9.9.9  ", "", "already-installed", ""},
		{"variant 传空回退 store 默认", "9.9.9", version.VariantSelfContained, "already-installed", ""},
		{"显式 variant 与账一致", "9.9.9", version.VariantSelfContained, "already-installed", ""},
		{"未知变体如实拒", "9.9.9", "win7", "", "未知运行库变体"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := f.svc.DownloadVersion(c.target, c.variant)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err=%v, want 含 %q", err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got=%q err=%v, want %q", got, err, c.want)
			}
		})
	}

}

func TestDownloadVersionExternalNotBlockedButGateDenies(t *testing.T) {
	f := newSvcFixture(t)
	f.simInstall(t, "v9.9.9", version.VariantSelfContained)

	sentinel := errors.New("gate: blocked")
	f.gate.deny = sentinel
	if _, err := f.svc.DownloadVersion("9.9.9", "win7"); !errors.Is(err, sentinel) {
		t.Fatalf("门检查必须先于参数校验且原样上抛: %v", err)
	}
	if f.gate.released.Load() != 0 {
		t.Error("拒绝路径不应有 release")
	}
}

// ---------- RemoveInstalled：卸载只删程序、便签数据原地保留 ----------

func TestRemoveInstalledKeepsNoteData(t *testing.T) {
	f := newSvcFixture(t)
	if err := f.svc.RemoveInstalled(); err == nil || !strings.Contains(err.Error(), "尚无托管安装") {
		t.Fatalf("空目录卸载应报尚无安装: %v", err)
	}

	f.simInstall(t, "v9.9.9", version.VariantSelfContained)
	// 追加残留与用户数据，验证清理名单与保留名单
	exe := filepath.Join(f.install, "PaperTodo.exe")
	if err := os.WriteFile(exe+".bak", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".download-tmp", []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugins := filepath.Join(f.install, "plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "x.js"), []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RemoveInstalled(); err != nil {
		t.Fatalf("RemoveInstalled: %v", err)
	}
	for _, gone := range []string{exe, exe + ".bak", exe + ".download-tmp", filepath.Join(f.install, "hanxi-meta.json")} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("卸载后残留程序侧文件: %s", gone)
		}
	}
	for _, kept := range []string{filepath.Join(f.install, "data.json"), filepath.Join(plugins, "x.js")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("便签数据被误删: %s (%v)", kept, err)
		}
	}
}

// ---------- ImportLocal：服务层 TrimSpace + 运行中拒绝 + 落盘实况 ----------

func TestImportLocalSurface(t *testing.T) {
	f := newSvcFixture(t)

	if _, err := f.svc.ImportLocal("   "); err == nil || !strings.Contains(err.Error(), "源目录未找到") {
		t.Fatalf("空源目录应报未找到: %v", err)
	}
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ImportLocal(src); err == nil {
		t.Fatal("无 PaperTodo.exe 的源目录应拒绝")
	}
	if err := os.WriteFile(filepath.Join(src, "PaperTodo.exe"), []byte("dummy-pe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "data.json"), []byte(`{"keep":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 外部实例在跑（互斥体在）→ 先拒导入
	f.toExternal(t)
	if _, err := f.svc.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "正在运行") {
		t.Fatalf("external 态应拒绝导入: %v", err)
	}
	f.probe.running = false
	f.svc.engine.RefreshExternal()

	// 带空白入参也必须成（service 层 TrimSpace 契约）
	info, err := f.svc.ImportLocal("  " + src + "  ")
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !info.IsImport || !info.HasData || info.Source != filepath.Clean(src) {
		t.Errorf("导入回执失真: %+v", info)
	}
	if !strings.HasPrefix(info.Version, "vunknown-") {
		t.Errorf("假 PE 应走日期兜底版本，got %q", info.Version)
	}
	if b, err := os.ReadFile(filepath.Join(f.install, "hanxi-meta.json")); err != nil {
		t.Errorf("导入应写 hanxi-meta: %v", err)
	} else {
		var mm map[string]any
		if json.Unmarshal(b, &mm) != nil || mm["isImport"] != true {
			t.Errorf("meta 未记录导入凭据: %s", b)
		}
	}
	if _, err := os.Stat(filepath.Join(f.install, "data.json")); err != nil {
		t.Errorf("便签数据未随导入迁移: %v", err)
	}

	// 已有托管安装时二次导入拒绝（防覆盖数据）
	if _, err := f.svc.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "已有 PaperTodo") {
		t.Fatalf("重复导入应拒绝: %v", err)
	}
}

// ---------- 窗口控制回执（静止/外部态，全部无进程副作用） ----------

func TestControlOutcomeStates(t *testing.T) {
	f := newSvcFixture(t)

	// 静止 + 未安装：开窗如实报"尚未安装"，收拢报"未在运行"，退出幂等成功
	if _, err := f.svc.OpenWindow(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("OpenWindow(未安装): %v", err)
	}
	if _, err := f.svc.HidePapers(); err == nil || !strings.Contains(err.Error(), "未在运行") {
		t.Fatalf("HidePapers(静止): %v", err)
	}
	out, err := f.svc.Quit()
	if err != nil || !out.Stopped {
		t.Fatalf("静止态 Quit 应幂等成功: out=%+v err=%v", out, err)
	}

	// 外部实例态：不越权强杀，只给指引；收拢/唤窗走 ResolveExe
	f.toExternal(t)
	q, err := f.svc.Quit()
	if err != nil {
		t.Fatalf("外部态 Quit 不应报错: %v", err)
	}
	if q.Stopped || !q.External || !strings.Contains(q.Message, "外部") {
		t.Errorf("外部态 Quit 回执失真: %+v", q)
	}
	if _, err := f.svc.OpenWindow(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("外部态无托管 exe 应报未安装: %v", err)
	}

	// 外部态 + 有托管安装（假 exe 文件）：信使拉起必失败（无效 PE），
	// 错误必须如实包装上抛，不得谎报"已唤起"
	f.simInstall(t, "v9.9.9", version.VariantSelfContained)
	if _, err := f.svc.OpenWindow(); err == nil || !strings.Contains(err.Error(), "唤起纸片失败") {
		t.Fatalf("假 exe 信使应失败: %v", err)
	}
	if _, err := f.svc.HidePapers(); err == nil {
		t.Fatal("假 exe 收拢信使应失败")
	}
}

func TestGetStatusReflectsProbe(t *testing.T) {
	f := newSvcFixture(t)
	snap, err := f.svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != instance.StateStopped || snap.External {
		t.Errorf("无互斥体应 stopped: %+v", snap)
	}
	f.probe.running = true
	snap, err = f.svc.GetStatus() // GetStatus 自带一次静止态校正
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != instance.StateExternal || !snap.External {
		t.Errorf("互斥体在场应 external: %+v", snap)
	}
}

// ---------- StateFailed → 通知 Hub（外部可观察回执） ----------

func TestEmitInstanceStateFailureReachesHub(t *testing.T) {
	hub := notify.GetHub()
	hub.ClearHistory()
	svc := &PaperTodoService{} // 直调：application 缺席不得 panic

	svc.emitInstanceState(instance.Snapshot{State: instance.StateRunning, PID: 1})
	if len(hub.GetHistory()) != 0 {
		t.Fatal("正常态迁移不应发通知")
	}
	svc.emitInstanceState(instance.Snapshot{State: instance.StateFailed, Error: ""})
	if len(hub.GetHistory()) != 0 {
		t.Fatal("无因由的 failed 不应发通知（既有契约：Error 非空才报）")
	}

	svc.emitInstanceState(instance.Snapshot{State: instance.StateFailed, Error: "boom-42"})
	hist := hub.GetHistory()
	if len(hist) != 1 {
		t.Fatalf("failed+因由必须恰好发一条通知，history=%d", len(hist))
	}
	n := hist[0]
	if n.ModuleID != ID || n.Level != notify.LevelError || n.Title != "PaperTodo 实例异常" ||
		n.Message != "boom-42" || n.Route != "/ext/papertodo" {
		t.Errorf("通知要素失真: %+v", n)
	}
	hub.ClearHistory()
}

// ---------- OpenDir 校验面（成功分支会拉起 explorer，不在默认档验证） ----------

func TestOpenDirValidation(t *testing.T) {
	f := newSvcFixture(t)
	if err := f.svc.OpenDir("  "); err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Errorf("空目录应拒: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "nope")
	if err := f.svc.OpenDir(missing); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("不存在目录应拒: %v", err)
	}
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.OpenDir(file); err == nil || !strings.Contains(err.Error(), "不是目录") {
		t.Errorf("文件应拒: %v", err)
	}
}

// ---------- 门矩阵：全 RPC 拒即无副作用（含 nil plat 的越界防护） ----------

func TestGateDenyAcrossRPCSurface(t *testing.T) {
	f := newSvcFixture(t)
	sentinel := errors.New("gate: disabled")
	f.gate.deny = sentinel

	calls := []struct {
		name string
		fn   func() error
	}{
		{"ListReleases", func() error { _, e := f.svc.ListReleases(); return e }}, // 拒时不得触网
		{"GetInstalledVersion", func() error { _, e := f.svc.GetInstalledVersion(); return e }},
		{"DownloadVersion", func() error { _, e := f.svc.DownloadVersion("1.0", ""); return e }},
		{"RemoveInstalled", f.svc.RemoveInstalled},
		{"ImportLocal", func() error { _, e := f.svc.ImportLocal("C:\\"); return e }},
		{"GetVariant", func() error { _, e := f.svc.GetVariant(); return e }},
		{"SetVariant", func() error { return f.svc.SetVariant(version.VariantNoRuntime) }},
		{"GetRuntimeStatus", func() error { _, e := f.svc.GetRuntimeStatus(); return e }},
		{"GetFollowOnExit", func() error { _, e := f.svc.GetFollowOnExit(); return e }},
		{"SetFollowOnExit", func() error { return f.svc.SetFollowOnExit(true) }},
		{"GetStatus", func() error { _, e := f.svc.GetStatus(); return e }},
		{"OpenWindow", func() error { _, e := f.svc.OpenWindow(); return e }},
		{"HidePapers", func() error { _, e := f.svc.HidePapers(); return e }},
		{"Quit", func() error { _, e := f.svc.Quit(); return e }},
		{"OpenDir", func() error { return f.svc.OpenDir(t.TempDir()) }},
		{"CreateDesktopShortcut", f.svc.CreateDesktopShortcut}, // plat==nil：放行会 panic，拒必须早于使用
		{"RepositoryURL", func() error { _, e := f.svc.RepositoryURL(); return e }},
		{"OpenRepository", f.svc.OpenRepository}, // 同上：拒不得触发浏览器
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			if err := c.fn(); !errors.Is(err, sentinel) {
				t.Errorf("%s 门拒绝须原样上抛，got %v", c.name, err)
			}
		})
	}
	// 拒后盘上无痕
	if got := f.svc.store.GetVariant(); got != defaultVariant {
		t.Errorf("SetVariant 被拒后仍落盘: %s", got)
	}
	if f.svc.store.GetFollowOnExit() {
		t.Error("SetFollowOnExit 被拒后仍落盘")
	}
	if got := f.gate.acquired.Load(); got != int64(len(calls)) {
		t.Errorf("每方法敲门一次: %d vs %d", got, len(calls))
	}

	// 纯 void 版 Shutdown：拒即早退，不得停轮询/杀实例（watching 账目不动）
	f.svc.activate()
	defer f.svc.shutdown()
	f.svc.Shutdown()
	f.svc.watchMu.Lock()
	watching := f.svc.watching
	f.svc.watchMu.Unlock()
	if !watching {
		t.Error("Shutdown 被门拒后不得产生停用副作用")
	}
	if got := f.gate.acquired.Load(); got != int64(len(calls))+1 {
		t.Errorf("Shutdown 应额外敲门一次: %d", got)
	}
}
