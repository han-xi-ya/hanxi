// store_more_test.go 补 papertodoStore 账本语义的未钉分支：盘上坏值的读取侧兜底、
// followOnExit 三态（缺键/显式 false/显式 true）经 JSON 的持久语义、
// 拒绝写入时文件字节分毫不动（NAS 同步账不许被非法写入污染）。
package papertodo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeRawConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "papertodo.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStoreOnDiskVariantDirtyFallsBackDefault(t *testing.T) {
	dir := t.TempDir()
	writeRawConfig(t, dir, `{"variant":"win7","followOnExit":true}`)
	s := newPapertodoStore(dir)
	if s.GetVariant() != defaultVariant {
		t.Errorf("盘上未知变体必须回落默认，got %q", s.GetVariant())
	}
	// 坏 variant 不连坐其他键：同文件的 followOnExit 照常读取
	if !s.GetFollowOnExit() {
		t.Error("同文件合法键不应被坏 variant 拖累")
	}
	// 下一次合法写入不得"顺手洗掉"未识别内容之外的事实：写后两键各归其位
	if err := s.SetVariant("no-runtime"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "papertodo.json")); !json.Valid(b) {
		t.Errorf("保存产物非合法 JSON: %s", b)
	}
	s2 := newPapertodoStore(dir)
	if s2.GetVariant() != "no-runtime" || !s2.GetFollowOnExit() {
		t.Errorf("坏账修复后往返失真: %+v %+v", s2.GetVariant(), s2.GetFollowOnExit())
	}
}

// TestStoreFollowOnExitThreeStates 指针语义：缺键=false 但显式 false 必须可持久
// （否则"关掉联动"永远写不进账）；true 往返同理。
func TestStoreFollowOnExitThreeStates(t *testing.T) {
	states := []struct {
		name string
		raw  string
		want bool
	}{
		{"缺键", `{"variant":"no-runtime"}`, false},
		{"显式 false", `{"variant":"no-runtime","followOnExit":false}`, false},
		{"显式 true", `{"variant":"no-runtime","followOnExit":true}`, true},
	}
	for _, c := range states {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRawConfig(t, dir, c.raw)
			if got := newPapertodoStore(dir).GetFollowOnExit(); got != c.want {
				t.Errorf("读取 %s → %v, want %v", c.raw, got, c.want)
			}
		})
	}

	// 写侧闭环：显式关掉后重开 store 仍是关（false 不落账的回归钉）
	dir := t.TempDir()
	s := newPapertodoStore(dir)
	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "papertodo.json"))
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("落盘非法 JSON: %s", raw)
	}
	if v, ok := m["followOnExit"]; !ok || v != false {
		t.Errorf("false 必须显式落账（缺键会被读侧当 false，但账目必须写真实值）: %s", raw)
	}
	if newPapertodoStore(dir).GetFollowOnExit() {
		t.Error("重启后开关值漂移")
	}
}

// TestStoreRejectedWriteLeavesFileUntouched 拒绝的 SetVariant 不得改写盘上账：
// 文件字节必须与拒绝前逐字节相同（含时间戳类元数据不依赖，只断言内容）。
func TestStoreRejectedWriteLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	s := newPapertodoStore(dir)
	if err := s.SetVariant("no-runtime"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "papertodo.json"))
	if err := s.SetVariant("Windows7"); err == nil {
		t.Fatal("未知变体应拒")
	}
	if s.GetVariant() != "no-runtime" {
		t.Errorf("内存账被非法写入污染: %s", s.GetVariant())
	}
	after, _ := os.ReadFile(filepath.Join(dir, "papertodo.json"))
	if string(before) != string(after) {
		t.Errorf("拒绝写入前文件被改写:\nbefore=%s\nafter=%s", before, after)
	}
}
