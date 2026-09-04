package ddnsgo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreRoundTrip 三字段持久化往返：设定 → 重建 store → 读回一致。
func TestStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	s := newDdnsgoStore(dir)
	if s.GetActive() != "" {
		t.Errorf("新 store active 应为空，实际 %q", s.GetActive())
	}
	if !s.GetFollowOnExit() {
		t.Error("默认应随 Hanxi 退出")
	}
	if s.GetListenPort() != defaultListenPort {
		t.Errorf("默认端口 = %d, want %d", s.GetListenPort(), defaultListenPort)
	}

	if err := s.SetActive("v6.17.6"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetListenPort(8080); err != nil {
		t.Fatal(err)
	}

	// 重新加载
	s2 := newDdnsgoStore(dir)
	if s2.GetActive() != "v6.17.6" {
		t.Errorf("active = %q, want v6.17.6", s2.GetActive())
	}
	if s2.GetFollowOnExit() {
		t.Error("followOnExit 应为 false")
	}
	if s2.GetListenPort() != 8080 {
		t.Errorf("listenPort = %d, want 8080", s2.GetListenPort())
	}
}

// TestStoreListenPortClampOnLoad 落盘文件被外部篡改为越界端口时，load 回退默认值。
func TestStoreListenPortClampOnLoad(t *testing.T) {
	dir := t.TempDir()
	bad := `{"activeVersion":"v1.0.0","listenPort":99999}`
	if err := os.WriteFile(filepath.Join(dir, "ddnsgo.json"), []byte(bad), 0644); err != nil {
		t.Fatal(err)
	}
	s := newDdnsgoStore(dir)
	if s.GetListenPort() != defaultListenPort {
		t.Errorf("越界端口应回退默认，实际 %d", s.GetListenPort())
	}
}
