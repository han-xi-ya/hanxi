package settings_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/settings"
)

// newWebAppStore 在临时目录建独立配置存储。
func newWebAppStore(t *testing.T) (*settings.Store, string) {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, cfgFile
}

// TestNewStoreSeedsFilehelperPreset 出厂与旧配置升级都必须带出预置的微信文件传输助手：
// 新配置文件不存在、旧配置文件缺 webAppEntries 键，load 均解码进含预置的默认副本。
func TestNewStoreSeedsFilehelperPreset(t *testing.T) {
	t.Run("全新配置", func(t *testing.T) {
		store, _ := newWebAppStore(t)
		entries := store.GetWebAppEntries()
		if len(entries) != 1 || entries[0].ID != "webapp_filehelper" {
			t.Fatalf("新商店应预置且仅预置 filehelper，实际 %+v", entries)
		}
	})

	t.Run("旧配置缺键升级", func(t *testing.T) {
		dir := t.TempDir()
		cfgFile := filepath.Join(dir, "config.json")
		// 模拟 F10 之前版本写出的配置（不含 webAppEntries 键，且显式写出 theme 覆盖默认）
		legacy := map[string]any{"theme": "dark", "logRetainDays": 7}
		raw, err := json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgFile, raw, 0644); err != nil {
			t.Fatal(err)
		}

		store, err := settings.NewStore(cfgFile)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		entries := store.GetWebAppEntries()
		if len(entries) != 1 || entries[0].ID != "webapp_filehelper" {
			t.Fatalf("缺键旧配置升级应带出预置条目，实际 %+v", entries)
		}
		if store.Get().Theme != "dark" {
			t.Error("升级不得覆盖旧配置显式写出的字段")
		}
	})
}

// TestWebAppEntriesExplicitEmptyNotRevived 用户删光条目后（显式存 []），
// 重启不得复活预置；显式存 null 时按 nil 兜底为空切片而非默认预置。
func TestWebAppEntriesExplicitEmptyNotRevived(t *testing.T) {
	store, cfgFile := newWebAppStore(t)
	if err := store.DeleteWebAppEntry("webapp_filehelper"); err != nil {
		t.Fatal(err)
	}
	if got := store.GetWebAppEntries(); len(got) != 0 {
		t.Fatalf("删除后应为空，实际 %+v", got)
	}

	// 模拟重启：直接按原文件再开一个 Store
	reopened, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("reopen NewStore: %v", err)
	}
	if got := reopened.GetWebAppEntries(); len(got) != 0 {
		t.Errorf("删光后重启不应复活预置，实际 %+v", got)
	}

	// JSON 显式 null 走 nil 兜底重建为空切片
	if err := os.WriteFile(cfgFile, []byte(`{"webAppEntries":null}`), 0644); err != nil {
		t.Fatal(err)
	}
	nullStore, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("null NewStore: %v", err)
	}
	if got := nullStore.GetWebAppEntries(); got == nil || len(got) != 0 {
		t.Errorf("显式 null 应兜底为空切片而非 nil/复活预置，实际 %+v", got)
	}
}

// TestGetReturnsIsolatedWebAppEntries 与 BUG-033 同谱纪律：Get 副本的
// WebAppEntries 不得与 Store 共享底层数组。
func TestGetReturnsIsolatedWebAppEntries(t *testing.T) {
	store, _ := newWebAppStore(t)
	if err := store.UpsertWebAppEntry(settings.WebAppEntry{ID: "w1", Name: "原名"}); err != nil {
		t.Fatal(err)
	}

	cfg := store.Get()
	for i := range cfg.WebAppEntries {
		if cfg.WebAppEntries[i].ID == "w1" {
			cfg.WebAppEntries[i].Name = "MUTATED"
		}
	}

	if got, ok := store.GetWebAppEntryByID("w1"); !ok || got.Name != "原名" {
		t.Error("修改 Get() 副本污染了 Store 内存态（底层数组仍共享）")
	}
}

// TestWebAppUpsertDeleteLifecycle Upsert 按 ID 命中替换/否则追加保序，
// 落盘重启后语义一致；GetWebAppEntryByID 命中与未命中。
func TestWebAppUpsertDeleteLifecycle(t *testing.T) {
	store, cfgFile := newWebAppStore(t)

	if err := store.UpsertWebAppEntry(settings.WebAppEntry{ID: "webapp_1", Name: "一加", URL: "https://a.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertWebAppEntry(settings.WebAppEntry{ID: "webapp_2", Name: "二加", URL: "https://b.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertWebAppEntry(settings.WebAppEntry{ID: "webapp_1", Name: "一改名", URL: "https://a.example.com", Width: 1000}); err != nil {
		t.Fatal(err)
	}

	entries := store.GetWebAppEntries()
	if len(entries) != 3 { // 预置 filehelper 仍在首位
		t.Fatalf("应共 3 条，实际 %+v", entries)
	}
	if entries[1].Name != "一改名" || entries[1].Width != 1000 || entries[2].Name != "二加" {
		t.Errorf("Upsert 替换或追加顺序错误: %+v", entries)
	}
	if _, ok := store.GetWebAppEntryByID("nope"); ok {
		t.Error("不存在的 ID 不应命中")
	}
	if got, ok := store.GetWebAppEntryByID("webapp_2"); !ok || got.Name != "二加" {
		t.Errorf("GetWebAppEntryByID 命中错误: %+v %v", got, ok)
	}

	if err := store.DeleteWebAppEntry("webapp_1"); err != nil {
		t.Fatal(err)
	}
	reopened, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.GetWebAppEntryByID("webapp_1"); ok {
		t.Error("删除的条目重启后复活")
	}
	if _, ok := reopened.GetWebAppEntryByID("webapp_2"); !ok {
		t.Error("未删除的条目丢失")
	}
}

// TestDeleteWebAppEntryPrunesTrayRefs 删除条目必须同步剔除托盘/轮盘配置中
// 本条目的命令引用（顶层与 group 子层都要清），其余条目原样保留。
func TestDeleteWebAppEntryPrunesTrayRefs(t *testing.T) {
	store, _ := newWebAppStore(t)
	if err := store.UpsertWebAppEntry(settings.WebAppEntry{ID: "webapp_gone", Name: "将被删"}); err != nil {
		t.Fatal(err)
	}

	const deadRef = "webapp/open:webapp_gone"
	menu := []settings.TrayMenuItem{
		{Type: settings.TrayItemCommand, Ref: deadRef, Enabled: true},
		{Type: settings.TrayItemCommand, Ref: "webapp/open:webapp_kept", Enabled: true},
		{Type: settings.TrayItemRoute, Ref: "/ext/wechat", Enabled: true},
		{Type: settings.TrayItemGroup, Label: "组", Children: []settings.TrayMenuItem{
			{Type: settings.TrayItemCommand, Ref: deadRef, Enabled: true},
			{Type: settings.TrayItemCommand, Ref: "wechat/renew", Enabled: true},
		}},
	}
	if err := store.SetTrayMenu(menu); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteWebAppEntry("webapp_gone"); err != nil {
		t.Fatal(err)
	}

	got := store.GetTrayMenu()
	if len(got) != 3 {
		t.Fatalf("顶层死引用应被剔除，剩余 %+v", got)
	}
	if got[0].Ref != "webapp/open:webapp_kept" {
		t.Errorf("存活条目被误删: %+v", got[0])
	}
	if got[2].Children == nil || len(got[2].Children) != 1 || got[2].Children[0].Ref != "wechat/renew" {
		t.Errorf("group 子层死引用未清理或误删: %+v", got[2].Children)
	}
}
