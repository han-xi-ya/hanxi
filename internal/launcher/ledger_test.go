// ledger_test.go 锁死取账源双账契约：EnabledItems 只读托盘账 TrayMenu、
// WheelItems 只读轮盘账 WheelMenu（同一套启用过滤语义），任一本账的写入
// 不得连带另一本账的读数——消费端切换（轮盘/托盘配置独立）的 launcher 层根据地。
package launcher

import (
	"path/filepath"
	"slices"
	"testing"

	"hanxi/internal/settings"
)

func newLedgerStore(t *testing.T) *settings.Store {
	t.Helper()
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func routeRefs(items []settings.TrayMenuItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Ref)
	}
	return out
}

func routeItems(refs ...string) []settings.TrayMenuItem {
	out := make([]settings.TrayMenuItem, 0, len(refs))
	for _, r := range refs {
		out = append(out, settings.TrayMenuItem{Type: settings.TrayItemRoute, Ref: r, Enabled: true})
	}
	return out
}

func TestEnabledAndWheelReadSeparateLedgers(t *testing.T) {
	store := newLedgerStore(t)
	// 两账各立两条：一启用一禁用，取账只应见各自账内的启用条目。
	if err := store.SetTrayMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/tray-on", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/tray-off", Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/wheel-on", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/wheel-off", Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	d := New(nil, store, nil)

	if got := routeRefs(d.EnabledItems()); !slices.Equal(got, []string{"/tray-on"}) {
		t.Errorf("EnabledItems 应只见托盘账启用条目，实际 %v", got)
	}
	if got := routeRefs(d.WheelItems()); !slices.Equal(got, []string{"/wheel-on"}) {
		t.Errorf("WheelItems 应只见轮盘账启用条目，实际 %v", got)
	}

	// 双向隔离①：改写轮盘账，托盘取账纹丝不动（原生托盘重建路径不吃轮盘账）。
	if err := store.SetWheelMenu(routeItems("/wheel-new")); err != nil {
		t.Fatal(err)
	}
	if got := routeRefs(d.EnabledItems()); !slices.Equal(got, []string{"/tray-on"}) {
		t.Errorf("轮盘账写入不得连带托盘取账，实际 %v", got)
	}
	if got := routeRefs(d.WheelItems()); !slices.Equal(got, []string{"/wheel-new"}) {
		t.Errorf("WheelItems 未跟随轮盘账更新，实际 %v", got)
	}

	// 双向隔离②：改写托盘账，轮盘取账不变（轮盘弹出不受托盘配置干扰）。
	if err := store.SetTrayMenu(routeItems("/tray-new")); err != nil {
		t.Fatal(err)
	}
	if got := routeRefs(d.WheelItems()); !slices.Equal(got, []string{"/wheel-new"}) {
		t.Errorf("托盘账写入不得连带轮盘取账，实际 %v", got)
	}
	if got := routeRefs(d.EnabledItems()); !slices.Equal(got, []string{"/tray-new"}) {
		t.Errorf("EnabledItems 未跟随托盘账更新，实际 %v", got)
	}
}

// TestLedgerSourcesNilStore store 未装配时两径同形回空不 panic；
// registry 为 nil 时 FilterDisabledModules 不过滤任何条目（与历史托盘径语义一致）。
func TestLedgerSourcesNilStore(t *testing.T) {
	d := New(nil, nil, nil)
	if len(d.EnabledItems()) != 0 || len(d.WheelItems()) != 0 {
		t.Errorf("nil store 应回空，实际 tray=%v wheel=%v", d.EnabledItems(), d.WheelItems())
	}
	items := []settings.TrayMenuItem{{Type: settings.TrayItemCommand, Ref: "ghost/x", Enabled: true}}
	if got := d.FilterDisabledModules(items); len(got) != 1 {
		t.Errorf("nil registry 不应过滤任何条目，实际 %+v", got)
	}
}
