package app

import (
	"testing"

	"hanxi/internal/launcher"
	"hanxi/internal/settings"
)

// 可见性收口实现（ItemModuleID/FilterDisabledModuleItems）已上收
// internal/launcher 与轮盘径同源复用，本文件锁死托盘径吃到的那份语义不变。

// TestItemModuleID 条目→模块 ID 解析：command 取 key 前缀、route 取 "/ext/"
// 首段，exe/核心路由/非模块引用返回空串（保持可见）。
func TestItemModuleID(t *testing.T) {
	cases := []struct {
		name string
		item settings.TrayMenuItem
		want string
	}{
		{"命令条目", settings.TrayMenuItem{Type: settings.TrayItemCommand, Ref: "snipaste/launch"}, "snipaste"},
		{"带实体的命令条目", settings.TrayMenuItem{Type: settings.TrayItemCommand, Ref: "webapp/open:myapp"}, "webapp"},
		{"无斜杠的坏命令引用", settings.TrayMenuItem{Type: settings.TrayItemCommand, Ref: "nocommand"}, ""},
		{"扩展路由条目", settings.TrayMenuItem{Type: settings.TrayItemRoute, Ref: "/ext/ocr"}, "ocr"},
		{"扩展路由带子路径", settings.TrayMenuItem{Type: settings.TrayItemRoute, Ref: "/ext/frpc/detail"}, "frpc"},
		{"核心路由", settings.TrayMenuItem{Type: settings.TrayItemRoute, Ref: "/settings"}, ""},
		{"裸扩展前缀", settings.TrayMenuItem{Type: settings.TrayItemRoute, Ref: "/ext/"}, ""},
		{"exe 条目与模块体系正交", settings.TrayMenuItem{Type: settings.TrayItemExe, Ref: "", Path: `C:\Tools\x.exe`}, ""},
	}
	for _, c := range cases {
		if got := launcher.ItemModuleID(c.item); got != c.want {
			t.Errorf("%s: ItemModuleID(%+v)=%q, want %q", c.name, c.item, got, c.want)
		}
	}
}

// TestFilterDisabledModuleItems 可见性收口：停用模块的 route/命令条目被滤除，
// 启用模块保留，未知模块 ID（历史残留）与 exe/核心路由不受影响，group 子条目
// 递归过滤。
func TestFilterDisabledModuleItems(t *testing.T) {
	state := map[string]bool{
		"ocr":      false, // 停用
		"msgboard": true,  // 启用
	}
	items := []settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/ext/ocr", Enabled: true},
		{Type: settings.TrayItemCommand, Ref: "ocr/snip-clipboard", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/ext/msgboard", Enabled: true},
		{Type: settings.TrayItemCommand, Ref: "msgboard/toggle", Enabled: true},
		{Type: settings.TrayItemCommand, Ref: "ghost/some-cmd", Enabled: true}, // 注册表查无此模块：保留
		{Type: settings.TrayItemExe, Path: `C:\Tools\x.exe`, Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/settings", Enabled: true},
		{Type: settings.TrayItemGroup, Label: "组", Enabled: true, Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemCommand, Ref: "ocr/snip-clipboard", Enabled: true},
			{Type: settings.TrayItemRoute, Ref: "/ext/msgboard", Enabled: true},
		}},
	}

	got := launcher.FilterDisabledModuleItems(items, state)

	var keptRouteOCR, keptCmdOCR bool
	for _, it := range got {
		if it.Type == settings.TrayItemRoute && it.Ref == "/ext/ocr" {
			keptRouteOCR = true
		}
		if it.Type == settings.TrayItemCommand && it.Ref == "ocr/snip-clipboard" {
			keptCmdOCR = true
		}
	}
	if keptRouteOCR || keptCmdOCR {
		t.Errorf("停用模块 ocr 的 route/命令条目应被滤除, got %+v", got)
	}
	if len(got) != 6 {
		t.Fatalf("顶层应保留 6 条（msgboard route+命令、ghost 命令、exe、核心路由、组）, got %d: %+v", len(got), got)
	}
	group := got[5]
	if group.Type != settings.TrayItemGroup || len(group.Children) != 1 || group.Children[0].Ref != "/ext/msgboard" {
		t.Errorf("group 子条目应递归滤除 ocr 引用、保留 msgboard, got %+v", group)
	}
	// 入参切片不被就地修改（过滤产副本，children 改写只发生在副本上）
	if len(items[7].Children) != 2 {
		t.Errorf("原配置切片不应被修改, children=%d", len(items[7].Children))
	}
}

// TestFilterDisabledModuleItemsEdge 边界：空表（registry 为 nil 时）与空输入
// 原样返回，不过滤任何条目。
func TestFilterDisabledModuleItemsEdge(t *testing.T) {
	items := []settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/ext/ocr", Enabled: true},
		{Type: settings.TrayItemCommand, Ref: "ocr/snip-clipboard", Enabled: true},
	}
	if got := launcher.FilterDisabledModuleItems(items, nil); len(got) != 2 {
		t.Errorf("空启停表不应过滤任何条目, got %+v", got)
	}
	if got := launcher.FilterDisabledModuleItems(nil, map[string]bool{"ocr": false}); got != nil {
		t.Errorf("空输入应原样返回, got %+v", got)
	}
}
