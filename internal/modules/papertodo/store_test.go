package papertodo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreDefaultsAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newPapertodoStore(dir)
	if got := s.GetVariant(); got != defaultVariant {
		t.Errorf("默认变体应为 %s，实际 %s", defaultVariant, got)
	}
	if !s.GetFollowOnExit() {
		t.Error("followOnExit 默认应为 true")
	}

	if err := s.SetVariant("no-runtime"); err != nil {
		t.Fatalf("SetVariant: %v", err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	// 重新加载验证落盘
	s2 := newPapertodoStore(dir)
	if s2.GetVariant() != "no-runtime" || s2.GetFollowOnExit() {
		t.Errorf("持久化往返错误: %+v", s2)
	}
}

func TestStoreRejectsUnknownVariant(t *testing.T) {
	s := newPapertodoStore(t.TempDir())
	if err := s.SetVariant("win7"); err == nil {
		t.Fatal("未知变体应拒绝")
	}
	if s.GetVariant() != defaultVariant {
		t.Error("拒绝后应保持原值")
	}
}

func TestStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "papertodo.json"), []byte("{-broken"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPapertodoStore(dir)
	// 损坏容忍：回退默认值而非报错阻断
	if s.GetVariant() != defaultVariant || !s.GetFollowOnExit() {
		t.Errorf("损坏配置应回退默认: %+v", s)
	}
}

func TestVariantHelpers(t *testing.T) {
	if !validVariant(defaultVariant) || validVariant("") {
		t.Error("validVariant 判定错误")
	}
	if variantName("no-runtime") == variantName("self-contained") {
		t.Error("变体中文名应区分")
	}
	if hint := variantHint("no-runtime"); hint == variantHint("self-contained") {
		t.Error("就绪超时提示应区分变体")
	}
}
