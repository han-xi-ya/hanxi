// status_launch_test.go 钉死 quickmenu 前端绑定面的外部事实：
// ① GetStatus 各字段 = 盘值钳制结果 / 钩子在位事实 / wheelView 展示序计数
// （含二级开关两种形态与停用模块收口）；
// ② ListItems 的图标解析词表（route/command/group/exe 四型 + 未登记回退）与
// Hint 取值口径；
// ③ Launch 的索引校验矩阵——非法派发必须被拒且不触发任何执行。
package quickmenu

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// navModule 最小 extapi.Module 实现：只供注册表产 Nav/启停表。
type navModule struct {
	id   string
	navs []extapi.NavEntry
}

func (m *navModule) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{ID: m.id, Name: "测试模块 " + m.id}
}
func (m *navModule) Nav() []extapi.NavEntry       { return m.navs }
func (m *navModule) Services() []extapi.Service   { return nil }
func (m *navModule) OnInit(context.Context) error { return nil }
func (m *navModule) OnDestroy() error             { return nil }
func (m *navModule) IsInitialized() bool          { return true }

func newRegSvc(t *testing.T, mods ...*navModule) (*QuickMenuService, *settings.Store, *extapi.Registry) {
	t.Helper()
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	reg := extapi.NewRegistry(nil)
	list := make([]extapi.Module, 0, len(mods))
	for _, m := range mods {
		list = append(list, m)
	}
	if err := reg.Register(list...); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return NewQuickMenuService(store, reg, extapi.NewLeaseHolder(ID)), store, reg
}

func TestGetStatusFieldsReflectStoreAndWheel(t *testing.T) {
	svc, store, reg := newRegSvc(t,
		&navModule{id: "livemod", navs: []extapi.NavEntry{{ID: "livemod", Route: "/ext/livemod", Title: "在用", Order: 1, Section: extapi.SectionExt}}},
		&navModule{id: "deadmod", navs: []extapi.NavEntry{{ID: "deadmod", Route: "/ext/deadmod", Title: "停用", Order: 2, Section: extapi.SectionExt}}},
	)
	if err := reg.SetEnabled("deadmod", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if err := store.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/ext/livemod", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/ext/deadmod", Enabled: true},  // 模块已停用：不得占扇区
		{Type: settings.TrayItemRoute, Ref: "/ext/ghostmod", Enabled: true}, // 未知模块：按账保留
		{Type: settings.TrayItemGroup, Label: "组", Enabled: true, Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemRoute, Ref: "/settings", Enabled: true},
			{Type: settings.TrayItemRoute, Ref: "/ext/other", Enabled: false},
		}},
		{Type: settings.TrayItemExe, Path: "C:\\nope\\nope.exe", Enabled: false}, // 禁用条目不占位
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetQuickMenuTrigger(3000, 2); err != nil { // 出域盘值：读数必须钳后给出
		t.Fatal(err)
	}

	st, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	// 二级默认开启：livemod + ghost + 组 = 3 扇区；停用模块的 1 条被剔除
	if st.ItemCount != 3 {
		t.Errorf("ItemCount(二级开) = %d, want 3（%+v）", st.ItemCount, st)
	}
	if st.TrapActive {
		t.Error("从未 start 的服务 TrapActive 必须为 false（不得虚报钩子在位）")
	}
	if st.HoldMs != 1500 || st.MoveTol != 4 {
		t.Errorf("坏盘值读数 = (%d,%d), want 钳后 (1500,4)", st.HoldMs, st.MoveTol)
	}
	if !st.TwoTier {
		t.Error("出厂默认二级轮盘应开启")
	}

	// 关闭二级：组拍平为 1 个启用叶子，livemod+ghost+拍平叶子 = 3 → 但组节点自身不再占位
	if err := store.SetQuickMenuTwoTier(false); err != nil {
		t.Fatal(err)
	}
	st2, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus(二级关): %v", err)
	}
	if st2.ItemCount != 3 || st2.TwoTier {
		t.Errorf("二级关 ItemCount = %d (twoTier=%v), want 3/false（组拍平后叶子计数）", st2.ItemCount, st2.TwoTier)
	}
}

// TestListItemsIconAndHintContract 钉死扇区视图模型的图标词表与 Hint 取值：
// route 复用导航图标（剥 i: 前缀、app: 原样透传、查不到回退 layout）；
// command 按归属模块找导航（ref "moduleId/commandId"）、查不到回 terminal；
// group→layers、exe→box；exe 的 Hint 取 Path、其余取 Ref、group 留空。
func TestListItemsIconAndHintContract(t *testing.T) {
	svc, store, _ := newRegSvc(t,
		&navModule{id: "modI", navs: []extapi.NavEntry{{ID: "modI", Route: "/ext/modI", Title: "甲", Icon: "i:wifi", Order: 1, Section: extapi.SectionExt}}},
		&navModule{id: "modApp", navs: []extapi.NavEntry{{ID: "modApp", Route: "/ext/modApp", Title: "乙", Icon: "app:beta", Order: 2, Section: extapi.SectionExt}}},
	)
	if err := store.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/ext/modI", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/ext/modApp", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/ext/unknown", Enabled: true},
		{Type: settings.TrayItemCommand, Ref: "modI/launch", Enabled: true},
		{Type: settings.TrayItemCommand, Ref: "nomod/cmd", Enabled: true},
		{Type: settings.TrayItemExe, Label: "记事本", Path: "C:\\Windows\\System32\\notepad.exe", Enabled: true},
		{Type: settings.TrayItemGroup, Label: "组", Enabled: true, Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemRoute, Ref: "/ext/modI", Enabled: true},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	items, err := svc.ListItems()
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 7 {
		t.Fatalf("扇区数 = %d, want 7: %+v", len(items), items)
	}
	type want struct {
		kind, hint, icon string
		label            string
	}
	wants := []want{
		{settings.TrayItemRoute, "/ext/modI", "wifi", ""},       // 标签空回退导航 Title
		{settings.TrayItemRoute, "/ext/modApp", "app:beta", ""}, // app: 前缀原样透传
		{settings.TrayItemRoute, "/ext/unknown", "layout", ""},  // 未登记路由回退
		{settings.TrayItemCommand, "modI/launch", "wifi", ""},   // 命令按模块找导航图标
		{settings.TrayItemCommand, "nomod/cmd", "terminal", ""}, // 未知模块命令回退
		{settings.TrayItemExe, "C:\\Windows\\System32\\notepad.exe", "box", "记事本"},
		{settings.TrayItemGroup, "", "layers", "组"},
	}
	for i, w := range wants {
		it := items[i]
		if it.Index != i {
			t.Errorf("第 %d 项 Index = %d（必须等于展示序）", i, it.Index)
		}
		if it.Type != w.kind || it.Hint != w.hint || it.Icon != w.icon {
			t.Errorf("第 %d 项 = {type:%q hint:%q icon:%q}, want {%q %q %q}", i, it.Type, it.Hint, it.Icon, w.kind, w.hint, w.icon)
		}
		if w.label != "" && it.Label != w.label {
			t.Errorf("第 %d 项 Label = %q, want %q", i, it.Label, w.label)
		}
		if it.Type != settings.TrayItemGroup && len(it.Children) != 0 {
			t.Errorf("非组条目第 %d 项 Children 应为空: %+v", i, it.Children)
		}
	}
	group := items[6]
	if len(group.Children) != 1 || group.Children[0].Index != 0 ||
		group.Children[0].Icon != "wifi" || group.Children[0].Type != settings.TrayItemRoute {
		t.Errorf("组子条目 = %+v, want 单个 route(/ext/modI) icon=wifi index=0", group.Children)
	}
}

var errLaunchRejected = errors.New("应被拒绝")

// TestLaunchValidationMatrix 非法派发路径全部如实拒绝：空路径、越界（正负）、
// 对非组条目请求子索引、子索引越界、点组本体执行；二级关闭拍平后组引用消失。
func TestLaunchValidationMatrix(t *testing.T) {
	svc, store, _ := newRegSvc(t)
	if err := store.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/settings", Enabled: true}, // 0：核心路由（非模块引用，不滤）
		{Type: settings.TrayItemGroup, Label: "组", Enabled: true, Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemRoute, Ref: "/ext/a", Enabled: true},
			{Type: settings.TrayItemRoute, Ref: "/ext/b", Enabled: true},
		}},
		{Type: settings.TrayItemRoute, Ref: "/ext/leaf-off-parent", Enabled: false}, // 禁用不入展示序
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		path  []int
		check func(error) bool
	}{
		{"空路径", []int{}, func(e error) bool { return e != nil }},
		{"nil 路径", nil, func(e error) bool { return e != nil }},
		{"主索引负", []int{-1}, func(e error) bool { return e != nil }},
		{"主索引越界", []int{2}, func(e error) bool { return e != nil }}, // 禁用条目占账不占位：展示序只有 2 项(0,1)
		{"非组请求子项", []int{0, 1}, func(e error) bool { return e != nil }},
		{"组本体直接执行", []int{1}, func(e error) bool { return e != nil }},
		{"子索引越界", []int{1, 2}, func(e error) bool { return e != nil }},
		{"子索引负", []int{1, -1}, func(e error) bool { return e != nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.Launch(c.path)
			if err == nil {
				t.Errorf("Launch(%v) 不该成功（%v）", c.path, errLaunchRejected)
			} else if !c.check(err) {
				t.Errorf("Launch(%v) 错误形态异常: %v", c.path, err)
			}
		})
	}

	// 合法路径：叶子与组内子条目派发无错（route 目标经 navigateMain 落空是
	// 应用缺席下的既语义，不影响 RPC 返回值）；拍平形态下组本体变可执行叶子、
	// 原组子索引 [1,j] 反被拒。
	if err := svc.Launch([]int{0}); err != nil {
		t.Errorf("合法叶子派发应无错: %v", err)
	}
	if err := svc.Launch([]int{1, 0}); err != nil {
		t.Errorf("组内合法子条目派发应无错: %v", err)
	}
	if err := store.SetQuickMenuTwoTier(false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Launch([]int{1}); err != nil { // 拍平后 [1]=组首叶子 /ext/a
		t.Errorf("拍平形态合法派发应无错: %v", err)
	}
	if err := svc.Launch([]int{1, 1}); err == nil { // 拍平后没有"组"了
		t.Error("拍平形态下二元路径必须被拒（该条目不是分组）")
	}
}

// TestGetStatusNilStoreNoApp 无 store 无应用的最小装配：读数走出厂值、不报错。
func TestGetStatusNilStoreNoApp(t *testing.T) {
	svc := NewQuickMenuService(nil, nil, extapi.NewLeaseHolder(ID))
	st, err := svc.GetStatus()
	if err != nil {
		t.Fatalf("nil store 读数不应报错: %v", err)
	}
	want := Status{TrapActive: false, HoldMs: factoryHoldMs, MoveTol: factoryMovePx, ItemCount: 0, TwoTier: false}
	if st != want {
		t.Errorf("nil store GetStatus = %+v, want 出厂降级 %+v", st, want)
	}
	if _, err := svc.GetTwoTier(); err != nil {
		t.Errorf("GetTwoTier: %v", err)
	}
}
