package ccswitch

import "testing"

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 true（旧配置兼容），
// 设 false 后落盘重载仍为 false；存取与 ActiveVersion 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newCCSwitchStore(dir)
	if !s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 true（随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newCCSwitchStore(dir)
	if s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 false")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("v1.2.3"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
}
