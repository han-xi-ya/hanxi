package recordly

import (
	"os"
	"path/filepath"
	"testing"
)

// TestChannelAndFollowOnExitPersist 通道与联动开关：缺省 stable/true（旧配置兼容），
// 落盘重载保持；两字段互不干扰。
func TestChannelAndFollowOnExitPersist(t *testing.T) {
	dir := t.TempDir()

	s := newRecordlyStore(dir)
	if s.GetReleaseChannel() != ChannelStable {
		t.Fatalf("默认通道应为 stable，实际 %s", s.GetReleaseChannel())
	}
	if !s.GetFollowOnExit() {
		t.Fatal("followOnExit 默认应为 true（随 Hanxi 关闭）")
	}

	if err := s.SetReleaseChannel(ChannelBeta); err != nil {
		t.Fatalf("SetReleaseChannel: %v", err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	s2 := newRecordlyStore(dir)
	if s2.GetReleaseChannel() != ChannelBeta || s2.GetFollowOnExit() {
		t.Fatalf("重载后应分别保持 beta/false，实际 %s/%v", s2.GetReleaseChannel(), s2.GetFollowOnExit())
	}

	if err := s.SetReleaseChannel("nightly"); err == nil {
		t.Error("非法通道应拒绝")
	}
	if s.GetReleaseChannel() != ChannelBeta {
		t.Error("非法设定不应破坏现值")
	}
}

// TestStoreCorruptionTolerant 配置文件损坏：静默回退默认值，不阻断模块。
func TestStoreCorruptionTolerant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "recordly.json"), []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newRecordlyStore(dir)
	if s.GetReleaseChannel() != ChannelStable || !s.GetFollowOnExit() {
		t.Fatalf("损坏配置应回退默认值: %s/%v", s.GetReleaseChannel(), s.GetFollowOnExit())
	}
}
