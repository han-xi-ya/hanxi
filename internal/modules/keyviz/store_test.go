package keyviz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestStoreRoundTrip activeVersion 与 followOnExit 持久化往返 + 默认值。
func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newKeyvizStore(dir)

	if s.GetActive() != "" {
		t.Fatalf("初始 activeVersion 应为空，实际 %q", s.GetActive())
	}
	if !s.GetFollowOnExit() {
		t.Fatal("followOnExit 默认应为 true")
	}

	if err := s.SetActive("v2.1.1"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}

	// 重新加载验证落盘
	s2 := newKeyvizStore(dir)
	if s2.GetActive() != "v2.1.1" {
		t.Errorf("重载 activeVersion = %q, want v2.1.1", s2.GetActive())
	}
	if s2.GetFollowOnExit() {
		t.Error("重载 followOnExit 应为 false")
	}
}

// TestStoreCorruptionTolerant 损坏 JSON 不阻断模块：按空配置继续。
func TestStoreCorruptionTolerant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keyviz.json"), []byte("{ broken"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newKeyvizStore(dir)
	if s.GetActive() != "" || !s.GetFollowOnExit() {
		t.Fatalf("损坏容忍失败: active=%q follow=%v", s.GetActive(), s.GetFollowOnExit())
	}
}

// TestStoreAtomicNoTmpLeftover save 走 tmp+rename，成功路径不留残留 tmp。
func TestStoreAtomicNoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	s := newKeyvizStore(dir)
	if err := s.SetActive("v2.1.1"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if ext := filepath.Ext(e.Name()); e.Name() != "keyviz.json" {
			t.Fatalf("目录只应有 keyviz.json，发现 %s（ext=%s）", e.Name(), ext)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "keyviz.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg keyvizConfig
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatalf("落盘内容非合法 JSON: %s", raw)
	}
	if cfg.ActiveVersion != "v2.1.1" {
		t.Errorf("落盘 activeVersion = %q", cfg.ActiveVersion)
	}
}
