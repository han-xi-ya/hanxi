package paseo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/paseo/instance"
	"hanxi/internal/modules/paseo/version"
	"hanxi/internal/platform"
)

// TestDownloadDoneSettlesActiveBeforeBroadcast 锁死 termora 2ac9b3b 同构竞态：
// "首装自动设使用"必须在 done 事件广播的同一收口步（emit 闭包内）先落账，
// 而非等 DownloadContext 返回之后——否则前端共享 store 收到 done 即复刷版本区
// 读 GetActiveVersion，瞬时读到空值。以 download 函数接缝注入假实现（无网络），
// 判据取在 done 送达后、DownloadContext 尚未返回的时点（channel 建立同步）。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	newSvc := func(t *testing.T) (*PaseoService, chan string) {
		t.Helper()
		activeAtDone := make(chan string, 1)
		svc := &PaseoService{
			manager:   version.NewManager(t.TempDir()),
			store:     newPaseoStore(t.TempDir()),
			holder:    extapi.NewLeaseHolder(ID),
			downloads: map[string]struct{}{},
		}
		svc.download = func(_ context.Context, _, ver string, emit func(version.DownloadProgress)) error {
			emit(version.DownloadProgress{Version: ver, Stage: "downloading", Done: 10, Total: 100})
			emit(version.DownloadProgress{Version: ver, Stage: "verify"})
			emit(version.DownloadProgress{Version: ver, Stage: "extract"})
			emit(version.DownloadProgress{Version: ver, Stage: "done", Done: 100, Total: 100})
			activeAtDone <- svc.store.GetActive()
			return nil
		}
		return svc, activeAtDone
	}
	receive := func(t *testing.T, ch chan string) string {
		t.Helper()
		select {
		case got := <-ch:
			return got
		case <-time.After(3 * time.Second):
			t.Fatal("后台下载收口未在时限内送达 done")
			return ""
		}
	}

	t.Run("首装done即落账", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if _, err := svc.DownloadVersion("v0.1.20"); err != nil {
			t.Fatal(err)
		}
		// paseo 版本口径去 v 前缀
		if got := receive(t, activeAtDone); got != "0.1.20" {
			t.Fatalf("done 送达时（DownloadContext 内）active 应已落账为 0.1.20，实得 %q", got)
		}
	})

	t.Run("已手选active不被done覆盖", func(t *testing.T) {
		svc, activeAtDone := newSvc(t)
		if err := svc.store.SetActive("0.1.19"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.DownloadVersion("v0.1.20"); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, activeAtDone); got != "0.1.19" {
			t.Fatalf("done 送达时 active 应保持用户手选的 0.1.19，实得 %q", got)
		}
	})
}

// ---------- OpenWindow external 态治理锁测 ----------

// externalStubProbe PaseoProbe 桩：running=true 让静止态校正把引擎判为
// external；focus 控制直唤成败，focusCalls 记录直唤尝试次数。
type externalStubProbe struct {
	running    bool
	focus      bool
	focusCalls int
	waitReady  bool
}

func (p *externalStubProbe) IsRunning() bool                 { return p.running }
func (p *externalStubProbe) WaitForReady(time.Duration) bool { return p.waitReady }
func (p *externalStubProbe) IsWindowOpen() bool              { return p.focus }
func (p *externalStubProbe) FocusWindow() bool {
	p.focusCalls++
	return p.focus
}

// externalStubJobAPI Job Object 桩：external 态绝不允许触达 Create——
// 任何"再拉进程"退化路径（Start）都会先在这里炸出显式错误。
type externalStubJobAPI struct{}

func (externalStubJobAPI) Create() (platform.Job, error) {
	return nil, errors.New("external 态不应创建 Job Object（禁止再拉进程）")
}

// newExternalOpenWindowSvc 组装"外部实例在场"的 service：fake probe 判
// external；versionsDir 里放一个可列装但必然执行失败的假版本目录（垃圾字节
// exe）——若代码退回报废的"external→信使开新窗"路径，exec.Command 拉起假
// exe 会以系统错误收口成 OpenWindow 报错，被本测试的 err==nil 断言当场抓获；
// Job 桩再把退到 Start 的路径堵死。
func newExternalOpenWindowSvc(t *testing.T, focus bool) (*PaseoService, *externalStubProbe) {
	t.Helper()
	versionsDir := t.TempDir()
	stateDir := t.TempDir()
	vdir := filepath.Join(versionsDir, "paseo_9.9.9")
	if err := os.MkdirAll(filepath.Join(vdir, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "Paseo.exe"), []byte("not-a-real-exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "resources", "app.asar"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	probe := &externalStubProbe{running: true, focus: focus}
	svc := &PaseoService{
		manager:   version.NewManager(versionsDir),
		store:     newPaseoStore(stateDir),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}
	svc.engine = instance.NewEngine(externalStubJobAPI{}, probe, instance.Callbacks{})
	return svc, probe
}

// TestOpenWindowExternalFocusOnly 机主实证缺陷的收口锁测（横幅"可唤起其窗口"
// 与"点一次开一扇新窗"行为矛盾）：external 态 OpenWindow 只允许 Win32 直唤，
// 零拉起进程——
//  1. 直唤成功 → external-focused；
//  2. 直唤失败（外部实例无可聚焦窗）→ external-unreachable 如实指引，
//     不得报"请求窗口失败"错（那意味着走了信使），绝不再 Start/OpenMessenger。
//
// 归属收敛纪律同测钉死：唤起成功不改变归属，状态保持 external——hanxi 只
// 托管自己拉起的进程，绝不"接管"用户自启实例。
func TestOpenWindowExternalFocusOnly(t *testing.T) {
	t.Run("直唤成功仅聚焦且不转移归属", func(t *testing.T) {
		svc, probe := newExternalOpenWindowSvc(t, true)
		out, err := svc.OpenWindow()
		if err != nil {
			t.Fatalf("external 直唤成功不应报错（报错=走了信使/Start 退化路）: %v", err)
		}
		if out.Action != "external-focused" || !out.External {
			t.Fatalf("action = %s external = %v, want external-focused/true", out.Action, out.External)
		}
		if probe.focusCalls != 1 {
			t.Fatalf("直唤应恰好尝试一次，focusCalls = %d", probe.focusCalls)
		}
		if snap := svc.engine.Snapshot(); snap.State != instance.StateExternal {
			t.Fatalf("唤起成功后归属必须保持 external（不接管），state = %s", snap.State)
		}
	})

	t.Run("无可聚焦窗如实报unreachable零拉起", func(t *testing.T) {
		svc, probe := newExternalOpenWindowSvc(t, false)
		out, err := svc.OpenWindow()
		if err != nil {
			t.Fatalf("external 唤不到窗必须回如实指引而非报错/信使退化: %v", err)
		}
		if out.Action != "external-unreachable" || !out.External {
			t.Fatalf("action = %s external = %v, want external-unreachable/true", out.Action, out.External)
		}
		if probe.focusCalls != 1 {
			t.Fatalf("直唤应恰好尝试一次，focusCalls = %d", probe.focusCalls)
		}
		if snap := svc.engine.Snapshot(); snap.State != instance.StateExternal {
			t.Fatalf("唤不动不得改变状态机归属，state = %s", snap.State)
		}
	})
}

// TestSortInstalledOrdering 冷启动"自动最新已装"回退依赖此排序：
// stable 恒大于同名预发布，beta 间按序号，imported 时间戳退化字典序排最后。
func TestSortInstalledOrdering(t *testing.T) {
	list := []version.PaseoVersionInfo{
		{Version: "imported-20260915-010203"},
		{Version: "0.8.0-beta.2"},
		{Version: "0.7.2"},
		{Version: "0.8.0"},
		{Version: "0.8.0-beta.1"},
	}
	sortInstalled(list)
	want := []string{"0.8.0", "0.8.0-beta.2", "0.8.0-beta.1", "0.7.2", "imported-20260915-010203"}
	for i := range want {
		if list[i].Version != want[i] {
			t.Fatalf("排序异常: %d = %s, want %s (%v)", i, list[i].Version, want[i], versionsOf(list))
		}
	}
}

func versionsOf(list []version.PaseoVersionInfo) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v.Version
	}
	return out
}
