package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/settings"
)

// TestNewStoreFillsMissingFieldsWithDefaults 锁 BUG-029 回归：旧配置文件缺少
// 新增字段时必须回落出厂默认，而非 Go 零值（false/0/空串）。
func TestNewStoreFillsMissingFieldsWithDefaults(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgFile, []byte(`{"theme":"dark"}`), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	cfg := store.Get()
	if cfg.Theme != "dark" {
		t.Errorf("JSON 显式值应保留，得到 %q", cfg.Theme)
	}
	if !cfg.MinimizeToTray {
		t.Error("缺失字段 minimizeToTray 应回落默认 true，而非零值 false")
	}
	if cfg.LogRetainDays != 7 {
		t.Errorf("缺失字段 logRetainDays 应回落默认 7，得到 %d", cfg.LogRetainDays)
	}
	if cfg.Language != "zh-CN" {
		t.Errorf("缺失字段 language 应回落默认 zh-CN，得到 %q", cfg.Language)
	}
	// wechat 出厂默认端点已移交业务模块兜底（settings 不再持有该业务默认，
	// 见 DefaultSettings 注释），此处口径随之反转：缺失字段回落空串。
	if cfg.Wechat.BaseURL != "" {
		t.Errorf("wechat.baseUrl 默认应为空串（由 wechat 模块兜底），得到 %q", cfg.Wechat.BaseURL)
	}
}

// TestUpdateRollbackOnSaveFailure 锁 BUG-030 回归：落盘失败时内存必须保持原值，
// 不允许"内存已更新、磁盘写入失败"的在线/离线分叉。
func TestUpdateRollbackOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// 构造必然失败的落盘：目标路径变成非空目录，Rename 覆盖失败
	if err := os.Mkdir(cfgFile, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgFile, "blocker"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := store.Update(func(c *settings.AppSettings) { c.Theme = "dark" }); err == nil {
		t.Fatal("落盘目标被目录占位时 Update 应返回错误")
	}
	if got := store.Get().Theme; got == "dark" {
		t.Error("Update 失败后内存态不应与磁盘分叉（回滚失效）")
	}
}

// TestGetReturnsIsolatedWechatAccounts 锁 BUG-033 回归：Get 副本的 WechatAccounts
// 不得与 Store 共享底层数组，调用方按下标写入不能污染内存态。
func TestGetReturnsIsolatedWechatAccounts(t *testing.T) {
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.UpsertWechatAccount(settings.WechatAccount{ID: "a1", BotToken: "secret"}); err != nil {
		t.Fatal(err)
	}

	cfg := store.Get()
	cfg.WechatAccounts[0].BotToken = "MUTATED"

	if again := store.GetWechatAccounts(); again[0].BotToken != "secret" {
		t.Error("修改 Get() 副本污染了 Store 内存态（底层数组仍共享）")
	}
}
