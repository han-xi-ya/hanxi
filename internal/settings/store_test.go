package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"hubkit/internal/settings"
)

func TestStorePersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hubkit-test-settings-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfgFile := filepath.Join(tmpDir, "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// 验证默认状态
	if !store.IsModuleEnabled("frpc", true) {
		t.Errorf("default frpc should be enabled")
	}

	// 禁用 frpc
	if err := store.SetModuleEnabled("frpc", false); err != nil {
		t.Fatalf("SetModuleEnabled failed: %v", err)
	}

	if store.IsModuleEnabled("frpc", true) {
		t.Errorf("frpc should be disabled")
	}

	// 重新加载 store 模拟重启
	store2, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore reload failed: %v", err)
	}

	if store2.IsModuleEnabled("frpc", true) {
		t.Errorf("persisted frpc should remain disabled across restart")
	}
}
