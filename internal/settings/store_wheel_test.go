// store_wheel_test.go 锁死轮盘独立账本（WheelMenu）的 load 迁移契约
// （机主拍板 2026-09-26，废止 N41"不存在第二份轮盘账本"口径）：
// 缺键旧 JSON 首次派生自 TrayMenu 并持久化 / 已有键绝不覆盖 / 深拷贝逐层断引用。
// 本文件属包内测试（package settings）：迁移的"拷贝断指针共享"无法经公开 API
// 观测（存储层所有写路径均整体换装），必须直查 Store.data 的两个切片。
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// legacyWheelConfig 模拟"托盘单账时代"写出的旧 config.json：有 trayMenu（含 group
// 嵌套），无 wheelMenu 键。
func legacyWheelConfig(t *testing.T, dir string) string {
	t.Helper()
	cfgFile := filepath.Join(dir, "config.json")
	legacy := map[string]any{
		"theme": "dark",
		"trayMenu": []TrayMenuItem{
			{Type: TrayItemRoute, Ref: "/ext/ocr", Enabled: true, Label: "截图"},
			{Type: TrayItemGroup, Label: "工具", Enabled: true, Children: []TrayMenuItem{
				{Type: TrayItemExe, Path: `C:\Tools\x.exe`, Enabled: true},
			}},
		},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, raw, 0644); err != nil {
		t.Fatal(err)
	}
	return cfgFile
}

// TestWheelMenuDerivedOnLegacyConfig 无键旧 JSON load → WheelMenu 深拷贝 TrayMenu
// 落入并持久化一次；两份账逐层断引用；重启不重复派生（幂等）。
func TestWheelMenuDerivedOnLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := legacyWheelConfig(t, dir)

	store, err := NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tray, wheel := store.GetTrayMenu(), store.GetWheelMenu()
	if len(wheel) != len(tray) {
		t.Fatalf("派生账应与托盘账同条数：wheel=%+v tray=%+v", wheel, tray)
	}
	if wheel[1].Label != "工具" || len(wheel[1].Children) != 1 ||
		wheel[1].Children[0].Path != `C:\Tools\x.exe` {
		t.Fatalf("派生须含 group 子层完整树：%+v", wheel[1])
	}

	// 断引用直查内存态：派生拷贝与 TrayMenu 不得共享任何层 Children 底层数组。
	if &store.data.TrayMenu[1].Children[0] == &store.data.WheelMenu[1].Children[0] {
		t.Error("派生深拷贝未断开 group Children 指针共享")
	}
	// 顶层切片本身同样不得共享底层数组（SetTrayMenu 类整体换装路径已由 clone
	// 兜底，此处防的就是 load 里偷懒的 `data.WheelMenu = data.TrayMenu`）。
	if len(store.data.TrayMenu) > 0 && &store.data.TrayMenu[0] == &store.data.WheelMenu[0] {
		t.Error("派生深拷贝与 TrayMenu 共享顶层底层数组")
	}

	// 派生必须已持久化：原始文件里现在要有 wheelMenu 键。
	raw, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys["wheelMenu"]; !ok {
		t.Fatal("派生后未落盘：config.json 仍缺 wheelMenu 键")
	}
	if string(keys["theme"]) != `"dark"` {
		t.Error("派生落盘不得改写其余配置")
	}

	// 幂等：落盘后再改托盘账并重启，轮盘账不得被二次派生回灌。
	if err := store.SetTrayMenu([]TrayMenuItem{{Type: TrayItemRoute, Ref: "/settings", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(cfgFile)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if w := reopened.GetWheelMenu(); len(w) != 2 || w[1].Label != "工具" {
		t.Errorf("重启后轮盘账应保持派生值，实际 %+v", w)
	}
	if tr := reopened.GetTrayMenu(); len(tr) != 1 || tr[0].Ref != "/settings" {
		t.Errorf("托盘账改动丢失：%+v", tr)
	}
}

// TestWheelMenuExistingKeyNeverOverwritten 已有 wheelMenu 键（数组，含显式空数组）
// 时 load 不得用 TrayMenu 覆盖用户独立账；null 视同缺键走派生。
func TestWheelMenuExistingKeyNeverOverwritten(t *testing.T) {
	t.Run("显式数组保原值", func(t *testing.T) {
		dir := t.TempDir()
		cfgFile := filepath.Join(dir, "config.json")
		raw := `{"trayMenu":[{"type":"route","ref":"/settings","enabled":true}],` +
			`"wheelMenu":[{"type":"route","ref":"/ext/ocr","label":"轮盘专用","enabled":true}]}`
		if err := os.WriteFile(cfgFile, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		store, err := NewStore(cfgFile)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		w := store.GetWheelMenu()
		if len(w) != 1 || w[0].Ref != "/ext/ocr" || w[0].Label != "轮盘专用" {
			t.Errorf("已有 wheelMenu 键被覆盖：%+v", w)
		}
	})

	t.Run("显式空数组不复活", func(t *testing.T) {
		dir := t.TempDir()
		cfgFile := filepath.Join(dir, "config.json")
		raw := `{"trayMenu":[{"type":"route","ref":"/settings","enabled":true}],"wheelMenu":[]}`
		if err := os.WriteFile(cfgFile, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		store, err := NewStore(cfgFile)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		if w := store.GetWheelMenu(); len(w) != 0 {
			t.Errorf("显式空轮盘账被 TrayMenu 复活：%+v", w)
		}
	})

	t.Run("显式null视同缺键派生", func(t *testing.T) {
		dir := t.TempDir()
		cfgFile := filepath.Join(dir, "config.json")
		raw := `{"trayMenu":[{"type":"route","ref":"/settings","enabled":true}],"wheelMenu":null}`
		if err := os.WriteFile(cfgFile, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		store, err := NewStore(cfgFile)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		if w := store.GetWheelMenu(); len(w) != 1 || w[0].Ref != "/settings" {
			t.Errorf("null 应走派生，实际 %+v", w)
		}
	})
}

// TestWheelTraySetGetIsolated Set/Get 双账互不连带 + Get 副本深拷贝隔离
// （改 GetWheelMenu 返回值不得污染 Store 内存态，与 BUG-033 同谱纪律）。
func TestWheelTraySetGetIsolated(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	store, err := NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	wheel := []TrayMenuItem{
		{Type: TrayItemGroup, Label: "轮盘组", Enabled: true, Children: []TrayMenuItem{
			{Type: TrayItemExe, Path: `C:\Tools\w.exe`, Enabled: true},
		}},
	}
	if err := store.SetWheelMenu(wheel); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTrayMenu([]TrayMenuItem{{Type: TrayItemRoute, Ref: "/settings", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if tr := store.GetTrayMenu(); len(tr) != 1 {
		t.Errorf("轮盘账写入连带改了托盘账：%+v", tr)
	}

	got := store.GetWheelMenu()
	got[0].Children[0].Path = `MUTATED`
	if again := store.GetWheelMenu(); again[0].Children[0].Path != `C:\Tools\w.exe` {
		t.Error("GetWheelMenu 副本与 Store 共享 Children（深拷贝缺失）")
	}

	// 入库侧同样断引用：调用方复用同一切片改子层不得回灌 Store。
	wheel[0].Children[0].Path = `C:\Tools\changed.exe`
	if after := store.GetWheelMenu(); after[0].Children[0].Path != `C:\Tools\w.exe` {
		t.Errorf("SetWheelMenu 入库未深拷贝，调用方后续改动回灌：%+v", after)
	}
}

// TestDeleteWebAppEntryPrunesBothLedgers 死引用剔除必须双账同清：只清托盘账
// 会在轮盘里留下"点击必报错"的死条目（两账分家后的新盲区）。
func TestDeleteWebAppEntryPrunesBothLedgers(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	store, err := NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.UpsertWebAppEntry(WebAppEntry{ID: "gone", Name: "将被删"}); err != nil {
		t.Fatal(err)
	}

	dead := []TrayMenuItem{
		{Type: TrayItemCommand, Ref: "webapp/open:gone", Enabled: true},
		{Type: TrayItemRoute, Ref: "/settings", Enabled: true},
	}
	if err := store.SetTrayMenu(dead); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWheelMenu(dead); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteWebAppEntry("gone"); err != nil {
		t.Fatal(err)
	}
	if tr := store.GetTrayMenu(); len(tr) != 1 || tr[0].Ref != "/settings" {
		t.Errorf("托盘账死引用未清理：%+v", tr)
	}
	if w := store.GetWheelMenu(); len(w) != 1 || w[0].Ref != "/settings" {
		t.Errorf("轮盘账死引用未清理：%+v", w)
	}
}
