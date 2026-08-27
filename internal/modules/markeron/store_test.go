package markeron

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreRoundTrip 设定 → 重建加载：activeVersion 完整往返且整形 JSON 落盘。
func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newMarkeronStore(dir)
	if got := s.GetActive(); got != "" {
		t.Fatalf("初始 activeVersion = %q, want 空", got)
	}
	if err := s.SetActive("v2.9.4"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "markeron.json")); err != nil {
		t.Fatalf("未落盘: %v", err)
	}

	// 重建加载（模拟重启）
	s2 := newMarkeronStore(dir)
	if got := s2.GetActive(); got != "v2.9.4" {
		t.Fatalf("重建后 activeVersion = %q, want v2.9.4", got)
	}
}

// TestStoreCorruptTolerant 内容损坏时按空配置继续，不阻断模块启动。
func TestStoreCorruptTolerant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "markeron.json"), []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newMarkeronStore(dir)
	if got := s.GetActive(); got != "" {
		t.Fatalf("损坏文件应回退空配置, got %q", got)
	}
	// 后续写入应能正常自愈覆盖损坏内容
	if err := s.SetActive("v2.10.0"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if got := newMarkeronStore(dir).GetActive(); got != "v2.10.0" {
		t.Fatalf("自愈失败: %q", got)
	}
}

// TestVersionCompare 验证多位数字段按数值而非字典序比较。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.10.0", "v2.9.4", 1}, // 字典序会误判 2.10.0 < 2.9.4
		{"v2.9.4", "v2.10.0", -1},
		{"v3.0.0", "v3.0.0", 0},
		{"2.9.4", "v2.9.4", 0}, // 前缀 v 容错
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestFollowOnExitDefaultAndPersist 联动开关：缺省 true，设 false 落盘重载保持。
func TestFollowOnExitDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()
	s := newMarkeronStore(dir)
	if !s.GetFollowOnExit() {
		t.Fatal("默认应为 true（随 HubKit 关闭）")
	}
	if err := s.SetFollowOnExit(false); err != nil {
		t.Fatalf("SetFollowOnExit: %v", err)
	}
	if s2 := newMarkeronStore(dir); s2.GetFollowOnExit() {
		t.Fatal("重载后应保持 false")
	}
}
