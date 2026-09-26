// wheel_menu_test.go 锁死轮盘独立账本的 RPC 门面契约：SetWheelMenu 与
// SetTrayMenu 共用同一套条目校验（单一实现）、落盘互不连带、
// 原生托盘不因 WheelMenu 变更重建（互不干扰是本任务灵魂）。
package app

import (
	"path/filepath"
	"testing"

	"hanxi/internal/settings"
)

type wheelTestEnv struct {
	svc      *AppService
	store    *settings.Store
	cfgFile  string
	rebuilds int
}

func newWheelTestEnv(t *testing.T) *wheelTestEnv {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	env := &wheelTestEnv{store: store, cfgFile: cfgFile}
	env.svc = &AppService{store: store, trayRebuild: func() { env.rebuilds++ }}
	return env
}

// TestSetWheelMenuSharesTrayValidation 两本账的拒收行为逐字一致（同一
// cleanTrayMenuItems 实现，防两套校验漂移）：逐一喂同款非法条目比错误文案。
func TestSetWheelMenuSharesTrayValidation(t *testing.T) {
	env := newWheelTestEnv(t)

	invalidCases := map[string][]settings.TrayMenuItem{
		"未知类型":   {{Type: "shortcut", Ref: "x"}},
		"命令缺引用":  {{Type: settings.TrayItemCommand, Ref: "  ", Enabled: true}},
		"路由缺引用":  {{Type: settings.TrayItemRoute, Ref: ""}},
		"exe缺路径": {{Type: settings.TrayItemExe, Path: ""}},
		"分组无名":   {{Type: settings.TrayItemGroup, Label: "", Children: []settings.TrayMenuItem{{Type: settings.TrayItemRoute, Ref: "/settings"}}}},
		"分组空子条目": {{Type: settings.TrayItemGroup, Label: "组"}},
		"分组套分组": {{Type: settings.TrayItemGroup, Label: "组", Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemGroup, Label: "娃", Children: []settings.TrayMenuItem{{Type: settings.TrayItemRoute, Ref: "/settings"}}},
		}}},
		"分组子条目非法": {{Type: settings.TrayItemGroup, Label: "组", Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemCommand, Ref: ""},
		}}},
	}
	for name, items := range invalidCases {
		trayErr := env.svc.SetTrayMenu(items)
		wheelErr := env.svc.SetWheelMenu(items)
		if trayErr == nil || wheelErr == nil {
			t.Fatalf("%s: 应双双拒收 tray=%v wheel=%v", name, trayErr, wheelErr)
		}
		if trayErr.Error() != wheelErr.Error() {
			t.Errorf("%s: 两本账校验文案漂移 tray=%q wheel=%q", name, trayErr.Error(), wheelErr.Error())
		}
	}
	if env.rebuilds != 0 {
		t.Errorf("非法提交不得触发任何托盘重建，实际 %d", env.rebuilds)
	}
}

// TestSetWheelMenuPersistsWithoutTrayRebuild 轮盘账保存：校验规整入库、
// 托盘账与原生托盘重建一概不动（不调 trayRebuild）。
func TestSetWheelMenuPersistsWithoutTrayRebuild(t *testing.T) {
	env := newWheelTestEnv(t)

	// 先立托盘账基准，验证后续轮盘写完全不连带它。
	if err := env.svc.SetTrayMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/settings", Label: "托盘基准", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if env.rebuilds != 1 {
		t.Fatalf("SetTrayMenu 应重建原生托盘一次，实际 %d", env.rebuilds)
	}

	if err := env.svc.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemCommand, Ref: " frpc/start ", Enabled: true},
		{Type: settings.TrayItemGroup, Label: " 组 ", Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemExe, Path: ` C:\T\x.exe `, Enabled: true},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if env.rebuilds != 1 {
		t.Error("WheelMenu 变更不得触发原生托盘重建（互不干扰）")
	}
	if tr := env.store.GetTrayMenu(); len(tr) != 1 || tr[0].Label != "托盘基准" {
		t.Errorf("轮盘写入连带污染托盘账：%+v", tr)
	}
	w := env.store.GetWheelMenu()
	if len(w) != 2 || w[0].Ref != "frpc/start" || w[1].Label != "组" ||
		w[1].Children[0].Path != `C:\T\x.exe` {
		t.Errorf("轮盘账入库/规整错误：%+v", w)
	}
}

// TestWheelHotChannelIsLiveStoreRead 热更通道实证：quickmenu 每次弹出经
// launcher.Dispatcher.EnabledItems() → store 现读取账，故 SetWheelMenu 落盘后
// "下次弹出吃到新配置"无需任何事件——新进程（重开 store）现读即得新账。
func TestWheelHotChannelIsLiveStoreRead(t *testing.T) {
	env := newWheelTestEnv(t)
	if err := env.svc.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/ext/ocr", Label: "改后轮盘", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}

	reopened, err := settings.NewStore(env.cfgFile)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	w := reopened.GetWheelMenu()
	if len(w) != 1 || w[0].Label != "改后轮盘" {
		t.Errorf("重开 store 现读应为新轮盘账，实际 %+v", w)
	}
	// 同谱验证反向隔离：轮盘改账不碰托盘——重开后的托盘账仍为出厂空账。
	if tr := reopened.GetTrayMenu(); len(tr) != 0 {
		t.Errorf("轮盘写入不应连带托盘账，重开却见 %+v", tr)
	}
}

// TestGetWheelMenuNilStore 与 GetTrayMenu 同形：store 未装配时回空切片不 panic，
// Set 报错不上 panic。
func TestGetWheelMenuNilStore(t *testing.T) {
	svc := &AppService{}
	if got := svc.GetWheelMenu(); got == nil || len(got) != 0 {
		t.Errorf("nil store 应回空切片，实际 %+v", got)
	}
	if err := svc.SetWheelMenu([]settings.TrayMenuItem{{Type: settings.TrayItemRoute, Ref: "/settings"}}); err == nil {
		t.Error("nil store 时 SetWheelMenu 应报错")
	}
}
