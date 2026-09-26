package gonavi

import "testing"

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 false（默认不随 Hanxi 关闭，
// Detached 随家族），设 true 后落盘重载仍为 true；存取与 ActiveVersion 互不干扰。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()

	s := newGoNaviStore(dir)
	if s.GetFollowOnExit() {
		t.Fatal("新 store 的 followOnExit 默认应为 false（不随 Hanxi 关闭）")
	}

	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newGoNaviStore(dir)
	if !s2.GetFollowOnExit() {
		t.Fatal("重载后 followOnExit 应保持 true")
	}

	// SetActive 不影响联动开关
	if err := s.SetActive("v0.8.9"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if !s.GetFollowOnExit() {
		t.Fatal("SetActive 不应影响 followOnExit")
	}
	s3 := newGoNaviStore(dir)
	if got := s3.GetActive(); got != "v0.8.9" {
		t.Fatalf("重载后 activeVersion 应保持 v0.8.9，实得 %q", got)
	}
}
