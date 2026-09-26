package translucenttb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeGarbage 向 store 目录写入损坏的 translucenttb.json（模拟半截写入/手工改坏）。
func writeGarbage(dir string) error {
	return os.WriteFile(filepath.Join(dir, "translucenttb.json"), []byte(`{"activeVersion": "20`), 0644)
}

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 false（默认不随 Hanxi 关闭），
// 设 true 后落盘重载仍为 true；存取与 ActiveVersion 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newTranslucentTBStore(dir)
	if s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 false（不随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newTranslucentTBStore(dir)
	if !s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 true")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("2026.2"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
	if got := newTranslucentTBStore(dir).GetActive(); got != "2026.2" {
		t.Fatalf("重载后 activeVersion = %q, want 2026.2", got)
	}
}

// TestStoreCorruptTolerance 损坏文件容忍：解析失败按空配置继续，不阻断模块。
func TestStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	if err := writeGarbage(dir); err != nil {
		t.Fatal(err)
	}
	s := newTranslucentTBStore(dir)
	if s.GetActive() != "" || s.GetFollowOnExit() || s.AVCrashCount() != 0 {
		t.Fatalf("损坏配置应退化为默认值，实际 %+v", s)
	}
}

// TestAVCrashRecordAccumulateAcrossReload AV 崩溃落账：无账 = 0；逐笔记账
// Count 累计、最近一笔 Version/At/Code 覆盖；重载（进程重启等价）账目不丢。
func TestAVCrashRecordAccumulateAcrossReload(t *testing.T) {
	dir := t.TempDir()
	s := newTranslucentTBStore(dir)
	if s.AVCrashCount() != 0 {
		t.Fatal("新 store 无 AV 账，计数应为 0")
	}

	at1 := time.Now().Add(-time.Hour)
	if err := s.RecordAVCrash(3221225477, "2026.2", at1); err != nil {
		t.Fatalf("RecordAVCrash: %v", err)
	}
	if s.AVCrashCount() != 1 {
		t.Fatalf("首笔记账后计数应为 1, got %d", s.AVCrashCount())
	}

	at2 := time.Now()
	if err := s.RecordAVCrash(3221225477, "2025.1", at2); err != nil {
		t.Fatalf("RecordAVCrash: %v", err)
	}
	s2 := newTranslucentTBStore(dir)
	if s2.AVCrashCount() != 2 {
		t.Fatalf("重载后计数应跨重启保真为 2, got %d", s2.AVCrashCount())
	}

	// 落盘形状锁死 {code, version, at, count}（count 为累计键）。
	raw, err := os.ReadFile(filepath.Join(dir, "translucenttb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		AVCrash *struct {
			Code    int       `json:"code"`
			Version string    `json:"version"`
			At      time.Time `json:"at"`
			Count   int       `json:"count"`
		} `json:"avCrash"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("落盘 JSON 解析失败: %v\n%s", err, raw)
	}
	if cfg.AVCrash == nil || cfg.AVCrash.Code != 3221225477 || cfg.AVCrash.Version != "2025.1" ||
		cfg.AVCrash.Count != 2 || !cfg.AVCrash.At.Equal(at2.UTC()) {
		t.Fatalf("落盘账目形状异常: %s", raw)
	}
}

// TestAVCrashFieldOmittedWhenClean 无账可记时 avCrash 键缺席（老配置文件形状不变）。
func TestAVCrashFieldOmittedWhenClean(t *testing.T) {
	dir := t.TempDir()
	s := newTranslucentTBStore(dir)
	if err := s.SetActive("2026.2"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "translucenttb.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "avCrash") {
		t.Fatalf("未记账不得写出 avCrash 键: %s", raw)
	}
}
