package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedManagedSettingsCreatesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := seedManagedSettings(dir); err != nil {
		t.Fatalf("seedManagedSettings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, settingsFileName))
	if err != nil {
		t.Fatalf("种子文件未创建: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("种子非合法 JSON: %v", err)
	}
	if v, ok := m["AutoCheckUpdate"]; !ok || v != false {
		t.Errorf("种子应关闭自动更新检查: %v", m)
	}
	if len(m) != 1 {
		t.Errorf("种子必须最小化（其余字段交由上游默认值）: %v", m)
	}
}

// TestSeedManagedSettingsNeverOverwrites 核心红线：已存在的用户配置一个字节都不能动。
func TestSeedManagedSettingsNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, settingsFileName)
	original := `{"AutoCheckUpdate":true,"Skin":"Monokai_Pro","MonitorItems":[{"Key":"CPU Usage"}]}`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := seedManagedSettings(dir); err != nil {
		t.Fatalf("seedManagedSettings: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("已有配置被改写:\n期望 %s\n实际 %s", original, data)
	}
}

func TestSeedManagedSettingsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := seedManagedSettings(dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, settingsFileName))
	if err := seedManagedSettings(dir); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, settingsFileName))
	if string(first) != string(second) {
		t.Error("重复 seed 不应改变文件内容")
	}
	if !strings.Contains(string(first), "AutoCheckUpdate") {
		t.Error("种子内容异常")
	}
}
