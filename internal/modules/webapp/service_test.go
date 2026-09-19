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
