package webapp

// defaultopen_e2e_test.go 钉死"网页应用默认打开形态"的端到端语义与双账链路
// （三路收口之 W3；实现本体在 W1 后端批次，本文件只锁命令供给侧行为契约）。
//
// ---------------------------------------------------------------------------
// 端到端链路注记（轮盘/托盘双账共用候选命令 ref，DefaultOpen 对两账同时生效）
// ---------------------------------------------------------------------------
//
// 自 5964984/ade2338 双账架构起，托盘右键账（settings TrayMenu）与轮盘账
// （settings WheelMenu）是两本独立配置、同一套条目类型（settings.TrayMenuItem，
// Type="command" 时 Ref 恒为 "moduleId/commandId" 形态的稳定引用）。两本账
// 对网页应用只引用同一个候选命令键 webapp/open:<entryID>，消费链完全汇拢：
//
//	轮盘扇区点击（quickmenu/service.go wheelView 取轮盘账 → s.disp.Dispatch）
//	托盘菜单点击（app/tray.go Rebuild 装配 → b.disp.Dispatch）
//	        └───────────────┬───────────────┘
//		launcher.Dispatcher.Dispatch（Type=command → registry.RunTrayCommand(Ref)）
//		extapi.Registry.RunTrayCommand：splitTrayKey 拆出 webapp / open:<entryID>
//		        → Acquire("webapp") 持 operation lease（未启用/未安装在此挡下）
//		        → provider.TrayCommands() 现取候选 → cmd.ID 命中 → cmd.Run(ctx)
//		webapp.Module.TrayCommands 闭包（Run = svc.openByDefault，点击时现读 Store，改设定即时生效）→ 派发形态
//		        → "browser"：OpenExternal → openURL 通道（生产为
//		          windows.OpenURL，rundll32 FileProtocolHandler 系统默认浏览器）
//		        → "window" / ""（空=缺省）/ 盘上坏值：Open 建/置顶独立 WebviewWindow
//
// 结论（本文件两个测试分别从命令供给侧与真实 registry 派发侧钉住）：
// DefaultOpen 只是 webapp/open:<id> 这一个命令 Run 闭包内部分派的形态选择，
// 托盘账与轮盘账各自拿到的都是同一个闭包，不存在任何宿主侧的"打开"语义第二
// 实现——默认设定改一处，两本账的点击行为同时变，无须（也不得）在两账配置面
// 各别开关。删除条目时双账死引用剔除已由 settings 层既有测试锁定，不在本文范围。
//
// W1 落盘契约（本文件写成于 W1 在途期，落盘后已逐点对齐复核）：
//   - settings.WebAppEntry.DefaultOpen string（json "defaultOpen,omitempty"），
//     取值域常量 DefaultOpenWindow/DefaultOpenBrowser，空=window 零迁移；
//   - 写侧闸门 SetEntryDefaultOpen（非法值拒），读侧分派 openByDefault 宽松
//     回落 window；GUI 行内两颗显式钮不受本设定约束，仅命令腿经分派。
//   本文件只观测命令供给侧行为（浏览器通道是否被调用），不锁内部接线形状。

import (
	"context"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// defaultOpenProbe 记录"系统浏览器通道"被调用的地址序列：测试据此判定
// webapp/open:<id> 命令实际走了哪条形态路径。
type defaultOpenProbe struct {
	urls []string
}

func (p *defaultOpenProbe) openURL(rawURL string) error {
	p.urls = append(p.urls, rawURL)
	return nil
}

// newDefaultOpenTestModule 构造带浏览器通道探针的模块实例（openURL 注入点
// 即 W1 分派的"系统浏览器"腿；窗口腿在 headless 下由 Open 的 application
// 判空挡下，与 service_test 同法）。预置 filehelper 条目恒存在（DefaultOpen
// 为空），测试条目以显式 ID 追加，互不干扰。
func newDefaultOpenTestModule(t *testing.T, probe *defaultOpenProbe) (*Module, *settings.Store) {
	t.Helper()
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	entries := []settings.WebAppEntry{
		{ID: "e_browser", Name: "浏览器形态", URL: "https://browser.example.com/", DefaultOpen: "browser"},
		{ID: "e_window", Name: "窗口形态", URL: "https://window.example.com/", DefaultOpen: "window"},
		{ID: "e_blank", Name: "缺省形态", URL: "https://blank.example.com/"}, // DefaultOpen 空=窗口
	}
	for _, e := range entries {
		if err := store.UpsertWebAppEntry(e); err != nil {
			t.Fatalf("预置条目 %s: %v", e.ID, err)
		}
	}
	svc := NewWebAppService(store, probe.openURL, extapi.NewLeaseHolder(ID))
	return &Module{svc: svc}, store
}

// lookupCommand 按候选命令 ID（webapp/open:<entryID> 的去前缀部分）取回命令。
func lookupCommand(t *testing.T, mod *Module, cmdID string) extapi.TrayCommand {
	t.Helper()
	for _, c := range mod.TrayCommands() {
		if c.ID == cmdID {
			if c.Run == nil {
				t.Fatalf("候选命令 %s 无 Run", cmdID)
			}
			return c
		}
	}
	t.Fatalf("TrayCommands 未供给候选命令 %s（命令供给侧断链）", cmdID)
	return extapi.TrayCommand{}
}

// assertWindowPathHeadless 断言窗口形态命令在 headless 环境的行为：绝不触碰
// 浏览器通道、不静默成功、占位句柄不得泄露（application 意外存在的宿主机跳过）。
func assertWindowPathHeadless(t *testing.T, mod *Module, probe *defaultOpenProbe, entryID, cmdID string) {
	t.Helper()
	svc := mod.svc
	err := lookupCommand(t, mod, cmdID).Run(context.Background())
	if len(probe.urls) != 0 {
		t.Errorf("%s（窗口形态）误走系统浏览器通道: %v", cmdID, probe.urls)
	}
	if err == nil {
		t.Skipf("此环境意外存在 application 实例，headless 窗口路径断言不适用（%s）", cmdID)
	}
	svc.mu.Lock()
	_, leaked := svc.wins[entryID]
	svc.mu.Unlock()
	if leaked {
		t.Errorf("%s 建窗失败路径泄漏占位句柄 %s", cmdID, entryID)
	}
}

// TestDefaultOpenAtCommandSupply 从命令供给侧钉三态分派：DefaultOpen="browser"
// 的条目经 webapp/open:<id> 直连系统默认浏览器（拿到条目 URL、干净成功、不开窗）；
// "window" 与 ""（缺省）走独立窗口路径（headless 下即 Wails application 判空
// 报错腿），浏览器通道一根手指都不碰。
func TestDefaultOpenAtCommandSupply(t *testing.T) {
	probe := &defaultOpenProbe{}
	mod, store := newDefaultOpenTestModule(t, probe)

	// 1) browser 形态：浏览器通道收到条目 URL，无窗口副作用。
	cmd := lookupCommand(t, mod, "open:e_browser")
	if err := cmd.Run(context.Background()); err != nil {
		t.Fatalf("browser 形态命令应干净成功，实际: %v", err)
	}
	if len(probe.urls) != 1 || probe.urls[0] != "https://browser.example.com/" {
		t.Errorf("browser 形态应恰好调用一次浏览器通道且地址为条目 URL，实际 %v", probe.urls)
	}
	mod.svc.mu.Lock()
	windowLeak := len(mod.svc.wins)
	mod.svc.mu.Unlock()
	if windowLeak != 0 {
		t.Errorf("browser 形态不得登记/残留窗口句柄，实际 %d", windowLeak)
	}

	// 2) window 形态与 3) 空（缺省）形态：独立窗口路径，浏览器通道静默。
	probe.urls = nil
	assertWindowPathHeadless(t, mod, probe, "e_window", "open:e_window")
	if len(probe.urls) != 0 {
		t.Fatalf("window 形态污染了浏览器通道，后续空形态断言不可信: %v", probe.urls)
	}
	probe.urls = nil
	assertWindowPathHeadless(t, mod, probe, "e_blank", "open:e_blank")

	// 预置条目（DefaultOpen 空）同样落窗口腿：缺省即窗口，向后兼容旧配置零迁移。
	if _, ok := store.GetWebAppEntryByID("webapp_filehelper"); ok {
		probe.urls = nil
		assertWindowPathHeadless(t, mod, probe, "webapp_filehelper", "open:webapp_filehelper")
	}
}

// TestDefaultOpenViaRegistryDispatch 把链路推进到真实 extapi.Registry：双账
// （托盘 TrayMenu / 轮盘 WheelMenu）条目 Type=command 的 Ref 恒为
// "webapp/open:<entryID>"，两宿主最终都汇到 RunTrayCommand 这同一条腿。本测试
// 以该 Ref 原文为输入，证明默认形态在派发链末端生效——Ref 只有一份，两账同效
// 是链路形状的直接推论，而非两账各自实现的巧合同步。
func TestDefaultOpenViaRegistryDispatch(t *testing.T) {
	probe := &defaultOpenProbe{}
	mod, _ := newDefaultOpenTestModule(t, probe)

	// store 传 nil：注册表内建模块默认启用、无 receipts 即安装恒真，
	// 与生产装配只差持久化的启停账，不影响命令派发语义。
	registry := extapi.NewRegistry(nil)
	if err := registry.Register(mod); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// 候选目录键面：设置页两账条目编辑器按 Key 挂载，键形必须稳定。
	var sawBrowser bool
	for _, info := range registry.ListTrayCommands() {
		if info.Key == "webapp/open:e_browser" {
			sawBrowser = true
		}
	}
	if !sawBrowser {
		t.Fatal("候选目录缺 webapp/open:e_browser——两账编辑器将挂不上该条目")
	}

	// browser 形态：以两账存储的 Ref 原文派发，落浏览器通道。
	if err := registry.RunTrayCommand(context.Background(), "webapp/open:e_browser"); err != nil {
		t.Fatalf("Ref 派发 browser 形态: %v", err)
	}
	if len(probe.urls) != 1 || probe.urls[0] != "https://browser.example.com/" {
		t.Errorf("Ref 派发未落浏览器通道，实际 %v", probe.urls)
	}

	// window 形态：同一派发腿落到窗口路径（headless 判定同上）。
	probe.urls = nil
	if err := registry.RunTrayCommand(context.Background(), "webapp/open:e_window"); err == nil {
		t.Skip("此环境意外存在 application 实例，Ref 派发的 headless 窗口断言不适用")
	}
	if len(probe.urls) != 0 {
		t.Errorf("Ref 派发的 window 形态污染浏览器通道: %v", probe.urls)
	}
}
