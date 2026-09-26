package webapp

import (
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

func newTestService(t *testing.T) (*WebAppService, *settings.Store) {
	t.Helper()
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// gate 未注入的放行 holder：测试走同一套门代码路径。
	return NewWebAppService(store, func(string) error { return nil }, extapi.NewLeaseHolder(ID)), store
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"https://filehelper.weixin.qq.com/", false},
		{"http://127.0.0.1:9876/", false},
		{"  https://example.com/a?b=1#c  ", false},
		{"", true},
		{"weixin.qq.com", true},       // 缺协议前缀必须显式拒绝，不猜
		{"javascript:alert(1)", true}, // 危险 scheme 旁路
		{"file:///C:/Windows/x.html", true},
		{"data:text/html,<script>", true},
		{"ftp://example.com", true},
		{"https://", true}, // 有 scheme 无主机
	}
	for _, c := range cases {
		got, err := validateURL(c.in)
		if c.wantErr && err == nil {
			t.Errorf("validateURL(%q) 应拒绝，实际通过得 %q", c.in, got)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateURL(%q) 应通过，实际失败: %v", c.in, err)
		}
	}
}

func TestSaveEntryLifecycle(t *testing.T) {
	svc, store := newTestService(t)

	// 新建：entryID 留空，服务端定 ID（webapp_ 前缀）
	id, err := svc.SaveEntry("", "测试站", "https://example.com", "🧪")
	if err != nil {
		t.Fatalf("SaveEntry 新建: %v", err)
	}
	if !strings.HasPrefix(id, "webapp_") {
		t.Errorf("新建 ID 应带 webapp_ 前缀，实际 %q", id)
	}
	if views, _ := svc.ListEntries(); len(views) != 2 { // 预置 filehelper + 新建
		t.Fatalf("新建后应共 2 条，实际 %+v", views)
	}

	// 更新：命中现有 ID 改名，不新增条目
	if _, err := svc.SaveEntry(id, "改名后", "https://example.com/x", ""); err != nil {
		t.Fatalf("SaveEntry 更新: %v", err)
	}
	got, ok := store.GetWebAppEntryByID(id)
	if !ok || got.Name != "改名后" || got.URL != "https://example.com/x" {
		t.Errorf("更新未生效: %+v ok=%v", got, ok)
	}
	if views, _ := svc.ListEntries(); len(views) != 2 {
		t.Errorf("更新不应增加条目: %+v", views)
	}

	// 闸门与幽灵引用
	if _, err := svc.SaveEntry("", "空协议", "example.com", ""); err == nil {
		t.Error("缺协议网址应被拒绝")
	}
	if _, err := svc.SaveEntry("", "  ", "https://ok.example.com", ""); err == nil {
		t.Error("空白名称应被拒绝")
	}
	if _, err := svc.SaveEntry("webapp_deleted", "幽灵", "https://ok.example.com", ""); err == nil {
		t.Error("不存在的 entryID 不得静默新建（防复活幽灵条目）")
	}
}

// TestWindowStateHelpersHeadless 无 application 环境下的窗体层防御：
// 错误路径必须干净返回且不留占位残留；无窗时收起/销毁均幂等。
// （开窗/销毁主路径依赖真实 Wails 主循环，无法 headless 测，走手工冒烟清单。）
func TestWindowStateHelpersHeadless(t *testing.T) {
	svc, store := newTestService(t)

	if err := svc.Open("  "); err == nil {
		t.Error("空 ID 开窗应报错")
	}
	if err := svc.Open("webapp_missing"); err == nil {
		t.Error("不存在条目开窗应报错")
	}
	// 预置条目存在但无 application：应报错且不留占位句柄
	if err := svc.Open("webapp_filehelper"); err == nil {
		t.Skip("此环境意外存在 application 实例，占位回滚断言不适用")
	}
	svc.mu.Lock()
	_, leaked := svc.wins["webapp_filehelper"]
	svc.mu.Unlock()
	if leaked {
		t.Error("建窗失败路径泄漏了占位句柄")
	}

	if err := svc.Collapse("webapp_filehelper"); err != nil {
		t.Errorf("无窗收起应幂等无错: %v", err)
	}
	if err := svc.CollapseAll(); err != nil {
		t.Errorf("无窗收起全部应幂等无错: %v", err)
	}
	svc.shutdown() // 空表不应 panic

	views, _ := svc.ListEntries()
	if len(views) != 1 || views[0].WindowOpen || views[0].WindowHidden {
		t.Errorf("无窗时窗态徽标应全 false: %+v", views)
	}
	if _, ok := store.GetWebAppEntryByID("webapp_filehelper"); !ok {
		t.Error("预置条目意外丢失")
	}
}

// TestSetEntryDefaultOpenGate 默认打开形态写侧闸门：window/browser/""(重置)
// 放行，非法值拒绝且不落盘；幽灵 ID/空 ID 拒绝；同值重复设置幂等。
func TestSetEntryDefaultOpenGate(t *testing.T) {
	svc, store := newTestService(t)

	if err := svc.SetEntryDefaultOpen("webapp_filehelper", "browser"); err != nil {
		t.Fatalf("设置 browser: %v", err)
	}
	if got, _ := store.GetWebAppEntryByID("webapp_filehelper"); got.DefaultOpen != "browser" {
		t.Errorf("browser 未落库: %+v", got)
	}
	// 同值幂等重写不得报错、不得改值
	if err := svc.SetEntryDefaultOpen("webapp_filehelper", "browser"); err != nil {
		t.Errorf("同值重设应幂等无错: %v", err)
	}
	if err := svc.SetEntryDefaultOpen("webapp_filehelper", "window"); err != nil {
		t.Fatalf("设置 window: %v", err)
	}
	// ""=重置回默认（存空值，omitempty 即回缺键=window 语义）
	if err := svc.SetEntryDefaultOpen("webapp_filehelper", ""); err != nil {
		t.Fatalf("重置空值: %v", err)
	}
	if got, _ := store.GetWebAppEntryByID("webapp_filehelper"); got.DefaultOpen != "" {
		t.Errorf("重置后应为空串，实际 %q", got.DefaultOpen)
	}

	before, _ := store.GetWebAppEntryByID("webapp_filehelper")
	if err := svc.SetEntryDefaultOpen("webapp_filehelper", "tab"); err == nil {
		t.Error("非法形态应拒绝")
	}
	if err := svc.SetEntryDefaultOpen("webapp_filehelper", "Browser"); err == nil {
		t.Error("大小写变体不在取值域，应拒绝（前端传枚举值，不做宽容归一）")
	}
	if after, _ := store.GetWebAppEntryByID("webapp_filehelper"); after.DefaultOpen != before.DefaultOpen {
		t.Errorf("非法值拒绝却改了盘上值: %q -> %q", before.DefaultOpen, after.DefaultOpen)
	}
	if err := svc.SetEntryDefaultOpen("webapp_missing", "browser"); err == nil {
		t.Error("不存在条目应报错")
	}
	if err := svc.SetEntryDefaultOpen("  ", "browser"); err == nil {
		t.Error("空 ID 应报错")
	}
}

// TestSaveEntryPreservesDefaultOpen SaveEntry 通道透传：改名/改网址/改图标
// 全量回写不得吞掉 DefaultOpen（写侧另有专职 setter，SaveEntry 不碰该字段）。
func TestSaveEntryPreservesDefaultOpen(t *testing.T) {
	svc, store := newTestService(t)
	if err := svc.SetEntryDefaultOpen("webapp_filehelper", "browser"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveEntry("webapp_filehelper", "改名", "https://example.com/new", "🌐"); err != nil {
		t.Fatal(err)
	}
	got, ok := store.GetWebAppEntryByID("webapp_filehelper")
	if !ok || got.Name != "改名" || got.DefaultOpen != "browser" {
		t.Errorf("SaveEntry 后 DefaultOpen 丢失: %+v ok=%v", got, ok)
	}
}

// TestDefaultOpenPersistenceRoundTrip 持久化往返与存量零迁移：browser 落盘
// 重启后原样带出并透出到列表视图；无 defaultOpen 键的存量条目读出即空串
// （=window 行为，无需迁移）。
func TestDefaultOpenPersistenceRoundTrip(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWebAppService(store, func(string) error { return nil }, extapi.NewLeaseHolder(ID))

	id, err := svc.SaveEntry("", "浏览器站", "https://browser.example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEntryDefaultOpen(id, "browser"); err != nil {
		t.Fatal(err)
	}

	reopened, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	resvc := NewWebAppService(reopened, func(string) error { return nil }, extapi.NewLeaseHolder(ID))
	views, err := resvc.ListEntries()
	if err != nil {
		t.Fatal(err)
	}
	var browserView, presetView *WebAppEntryView
	for i := range views {
		switch views[i].ID {
		case id:
			browserView = &views[i]
		case "webapp_filehelper":
			presetView = &views[i]
		}
	}
	if browserView == nil || browserView.DefaultOpen != "browser" {
		t.Fatalf("browser 重启后未带出或视图未透出: %+v", views)
	}
	// 预置条目为存量无键形态：空串即 window 语义
	if presetView == nil || presetView.DefaultOpen != "" {
		t.Errorf("无键存量条目默认形态应为空串（=window），实际 %+v", presetView)
	}
}

// TestOpenByDefaultDispatchTable 轮盘/托盘点击分派表（headless seam，经
// openByDefault 即命令 Run 闭包同一入口）：browser 走系统浏览器通道；
// window/空（存量无键）/盘上坏值均走独立窗通道——headless 下 Open 因无
// application 报错，断言点是不误触浏览器通道（opened 恒空）。
func TestOpenByDefaultDispatchTable(t *testing.T) {
	cases := []struct {
		name        string
		defaultOpen string
		wantBrowser bool
	}{
		{"显式 window", "window", false},
		{"显式 browser", "browser", true},
		{"空=存量默认 window", "", false},
		{"坏值回落 window", "fullscreen", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var opened string
			store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			svc := NewWebAppService(store, func(u string) error { opened = u; return nil }, extapi.NewLeaseHolder(ID))
			id, err := svc.SaveEntry("", "分派站", "https://dispatch.example.com", "")
			if err != nil {
				t.Fatal(err)
			}
			// 绕开写侧闸门直改盘上形态，模拟存量/手改坏值
			entry, _ := store.GetWebAppEntryByID(id)
			entry.DefaultOpen = c.defaultOpen
			if err := store.UpsertWebAppEntry(entry); err != nil {
				t.Fatal(err)
			}

			err = svc.openByDefault(id)
			if c.wantBrowser {
				if err != nil || opened != "https://dispatch.example.com" {
					t.Errorf("browser 应走浏览器通道: err=%v opened=%q", err, opened)
				}
				return
			}
			if opened != "" {
				t.Errorf("%s 不得走浏览器通道，实际 opened=%q", c.name, opened)
			}
			if err == nil || !strings.Contains(err.Error(), "应用内打开") {
				t.Errorf("%s 应走独立窗通道（headless 下报无 application 错），实际 err=%v", c.name, err)
			}
		})
	}
}

func TestDeleteEntryAndOpenExternal(t *testing.T) {
	var opened string
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWebAppService(store, func(u string) error { opened = u; return nil }, extapi.NewLeaseHolder(ID))

	if err := svc.DeleteEntry("webapp_filehelper"); err != nil {
		t.Fatalf("删除预置条目: %v", err)
	}
	if err := svc.DeleteEntry("webapp_filehelper"); err == nil {
		t.Error("重复删除应报条目不存在")
	}
	if err := svc.DeleteEntry("  "); err == nil {
		t.Error("空 ID 应报错")
	}

	id, err := svc.SaveEntry("", "跳板", "https://jump.example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.OpenExternal(id); err != nil || opened != "https://jump.example.com" {
		t.Errorf("OpenExternal 应把定稿 URL 交给浏览器通道: err=%v opened=%q", err, opened)
	}
	if err := svc.OpenExternal("nope"); err == nil {
		t.Error("OpenExternal 不存在的条目应报错")
	}
}
