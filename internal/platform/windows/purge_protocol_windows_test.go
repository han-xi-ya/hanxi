//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPurgeProtocolRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id := "req-0123456789abcdef"
	path, err := NewPurgeRequestFile(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("结果文件跑出 runtime 目录: %s", path)
	}
	if err := os.WriteFile(path, []byte(`{"protocolVersion":1,"requestId":"req-0123456789abcdef","state":"success","beforeAvailableBytes":10,"afterAvailableBytes":20,"message":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPurgeResult(path, id)
	if err != nil || got.AfterAvailableBytes != 20 {
		t.Fatalf("读取结果失败: %+v %v", got, err)
	}
	if _, err := ReadPurgeResult(path, "req-ffffffffffffffff"); err == nil {
		t.Fatal("request ID 不匹配必须拒绝")
	}
}

func TestPurgeProtocolRejectsAndCleans(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewPurgeRequestFile(dir, "bad"); err == nil {
		t.Fatal("短 ID 必须拒绝")
	}
	old := filepath.Join(dir, "hanxi-purge-old.json")
	if err := os.WriteFile(old, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	removed := CleanupStalePurgeResults(dir, time.Now())
	if len(removed) != 1 || removed[0] != old {
		t.Fatalf("旧结果清理异常: %v", removed)
	}
}
