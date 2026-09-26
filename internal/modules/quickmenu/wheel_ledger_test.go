// wheel_ledger_test.go 锁死轮盘三径统一取账点 wheelView（GetStatus.ItemCount /
// ListItems / Launch 全经它）的账本源 = 轮盘独立账 WheelMenu，并做双向隔离断言：
// 改托盘账不改轮盘弹出清单、改轮盘账不改托盘径取账（托盘重建不连带侧由
// internal/app/wheel_menu_test.go 的 trayRebuild 断言把守）。
package quickmenu

import (
	"path/filepath"
	"slices"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

func newLedgerService(t *testing.T) (*QuickMenuService, *settings.Store) {
	t.Helper()
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NewQuickMenuService(store, nil, extapi.NewLeaseHolder(ID)), store
}

// viewRefs 收集 wheelView 顶层扇区的 route 引用（含拍平后的组内叶子）。
func viewRefs(view []wheelNode) []string {
	out := make([]string, 0, len(view))
	for _, n := range view {
		out = append(out, n.item.Ref)
	}
	return out
}

func routeLedgerItems(refs ...string) []settings.TrayMenuItem {
	out := make([]settings.TrayMenuItem, 0, len(refs))
	for _, r := range refs {
		out = append(out, settings.TrayMenuItem{Type: settings.TrayItemRoute, Ref: r, Enabled: true})
	}
	return out
}

func TestWheelViewReadsWheelLedger(t *testing.T) {
	svc, store := newLedgerService(t)
	if err := store.SetTrayMenu(routeLedgerItems("/tray-only")); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/wheel-a", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/wheel-b", Enabled: false}, // 禁用条目不占扇区
	}); err != nil {
		t.Fatal(err)
	}

	if got := viewRefs(svc.wheelView()); !slices.Equal(got, []string{"/wheel-a"}) {
		t.Errorf("轮盘清单应只来自轮盘账启用条目，实际 %v", got)
	}
}

// TestWheelLedgerIsolationBothWays 双向隔离：托盘账任意改写不影响弹出清单；
// 轮盘账改写即时跟随（现读语义）且不外溢到托盘径 EnabledItems 取账。
func TestWheelLedgerIsolationBothWays(t *testing.T) {
	svc, store := newLedgerService(t)
	if err := store.SetWheelMenu(routeLedgerItems("/wheel-a")); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTrayMenu(routeLedgerItems("/tray-a")); err != nil {
		t.Fatal(err)
	}
	if got := viewRefs(svc.wheelView()); !slices.Equal(got, []string{"/wheel-a"}) {
		t.Fatalf("基线弹出清单错误，实际 %v", got)
	}

	// 方向一：改托盘账（含清空）→ 轮盘弹出清单不变。
	if err := store.SetTrayMenu(routeLedgerItems("/tray-b", "/tray-c")); err != nil {
		t.Fatal(err)
	}
	if got := viewRefs(svc.wheelView()); !slices.Equal(got, []string{"/wheel-a"}) {
		t.Errorf("改托盘账不得改变轮盘弹出清单，实际 %v", got)
	}

	// 方向二：改轮盘账 → 弹出清单即时跟随，托盘径 EnabledItems 取账不变。
	if err := store.SetWheelMenu(routeLedgerItems("/wheel-x", "/wheel-y")); err != nil {
		t.Fatal(err)
	}
	if got := viewRefs(svc.wheelView()); !slices.Equal(got, []string{"/wheel-x", "/wheel-y"}) {
		t.Errorf("轮盘账改写后弹出清单未现读跟随，实际 %v", got)
	}
	tray := svc.disp.EnabledItems()
	if len(tray) != 2 || tray[0].Ref != "/tray-b" || tray[1].Ref != "/tray-c" {
		t.Errorf("改轮盘账不得连带托盘径取账，实际 %+v", tray)
	}
}

// TestWheelViewGroupFlattenKeepsLedgerSource 分组条目同样吃轮盘账：二级开关
// 关闭拍平、开启树形两形态均不得从托盘账借条目。
func TestWheelViewGroupFlattenKeepsLedgerSource(t *testing.T) {
	svc, store := newLedgerService(t)
	group := []settings.TrayMenuItem{{Type: settings.TrayItemRoute, Ref: "/tray-leaf", Enabled: true}}
	if err := store.SetTrayMenu(routeLedgerItems(group[0].Ref)); err != nil {
		t.Fatal(err)
	}
	wheelGroup := settings.TrayMenuItem{Type: settings.TrayItemGroup, Label: "轮盘组", Enabled: true,
		Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemRoute, Ref: "/wheel-kid", Enabled: true},
			{Type: settings.TrayItemRoute, Ref: "/wheel-kid-off", Enabled: false},
		}}
	if err := store.SetWheelMenu([]settings.TrayMenuItem{wheelGroup}); err != nil {
		t.Fatal(err)
	}

	// 出厂默认二级开启：组挂树、空启用子条目滤除。
	view := svc.wheelView()
	if len(view) != 1 || view[0].item.Type != settings.TrayItemGroup ||
		len(view[0].kids) != 1 || view[0].kids[0].Ref != "/wheel-kid" {
		t.Fatalf("二级开启形态错误：%+v", view)
	}
	// 关闭二级：组内启用叶子拍平为主盘扇区，托盘账 /tray-leaf 始终不出现。
	if err := store.SetQuickMenuTwoTier(false); err != nil {
		t.Fatal(err)
	}
	if got := viewRefs(svc.wheelView()); !slices.Equal(got, []string{"/wheel-kid"}) {
		t.Errorf("拍平形态应只含轮盘账启用叶子，实际 %v", got)
	}
}
