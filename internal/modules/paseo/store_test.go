package paseo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActiveChannelAndFollowOnExitPersist(t *testing.T) {
	dir := t.TempDir()
	s := newPaseoStore(dir)
	if s.GetActive() != "" || s.GetReleaseChannel() != ChannelStable || s.GetFollowOnExit() {
		t.Fatalf("默认值异常: active=%q channel=%q follow=%v", s.GetActive(), s.GetReleaseChannel(), s.GetFollowOnExit())
	}
	if err := s.SetActive("0.8.0-beta.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReleaseChannel(ChannelBeta); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFollowOnExit(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "paseo.json")); err != nil {
		t.Fatalf("未落盘: %v", err)
	}

	// 重载验证持久化闭环
	s2 := newPaseoStore(dir)
	if s2.GetActive() != "0.8.0-beta.1" || s2.GetReleaseChannel() != ChannelBeta || !s2.GetFollowOnExit() {
		t.Fatalf("重载异常: %+v", s2)
	}

	// 非法通道拒绝
	if err := s2.SetReleaseChannel("nightly"); err == nil {
		t.Fatal("非法通道应拒绝")
	}
	// 空串 active = 恢复自动最新
	if err := s2.SetActive(""); err != nil {
		t.Fatal(err)
	}
	if newPaseoStore(dir).GetActive() != "" {
		t.Fatal("active 应已清空")
	}
}

func TestStoreCorruptionTolerant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "paseo.json"), []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newPaseoStore(dir)
	if s.GetActive() != "" || s.GetReleaseChannel() != ChannelStable || s.GetFollowOnExit() {
		t.Fatalf("损坏内容应按空配置继续: %+v", s)
	}
}
