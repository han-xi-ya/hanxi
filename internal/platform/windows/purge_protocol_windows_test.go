//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(path, []byte(`{"protocolVersion":2,"mode":"purge-standby","requestId":"req-0123456789abcdef","state":"success","beforeAvailableBytes":10,"afterAvailableBytes":20,"beforeLedger":{"totalBytes":100,"availableBytes":10,"standbyBytes":4,"modifiedBytes":2,"pagesMeasured":true},"afterLedger":{"totalBytes":100,"availableBytes":20,"standbyBytes":1,"modifiedBytes":2,"pagesMeasured":true},"message":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPurgeResult(path, id)
	if err != nil || got.AfterAvailableBytes != 20 {
		t.Fatalf("读取结果失败: %+v %v", got, err)
	}
	if !got.BeforeLedger.PagesMeasured || got.BeforeLedger.StandbyBytes != 4 || got.AfterLedger.StandbyBytes != 1 {
		t.Fatalf("账目载荷透传失真: %+v", got)
	}
	if _, err := ReadPurgeResult(path, "req-ffffffffffffffff"); err == nil {
		t.Fatal("request ID 不匹配必须拒绝")
	}
}

func TestPurgeProtocolV1PayloadRejected(t *testing.T) {
	dir := t.TempDir()
	id := "req-0123456789abcdef"
	path := filepath.Join(dir, purgeResultPrefix+id+"-1.json")
	if err := os.WriteFile(path, []byte(`{"protocolVersion":1,"requestId":"`+id+`","state":"success","beforeAvailableBytes":10,"afterAvailableBytes":20,"message":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPurgeResult(path, id); err == nil || !strings.Contains(err.Error(), "协议") {
		t.Fatalf("v1 载荷必须因版本严格校验拒收，got %v", err)
	}
}

func TestPurgeProtocolWriteReadWithLedgerAndCounts(t *testing.T) {
	dir := t.TempDir()
	id := "ews-0123456789abcdef"
	path := filepath.Join(dir, purgeResultPrefix+id+"-x.json")
	payload := PurgeResultFile{
		RequestID: id, Mode: EmptyWorkingSetsHelperMode, State: "success", Message: "ok",
		BeforeAvailableBytes: 5, AfterAvailableBytes: 9,
		BeforeLedger:        MemoryLedger{TotalBytes: 100, AvailableBytes: 5, StandbyBytes: 2, ModifiedBytes: 1, PagesMeasured: true},
		AfterLedger:         MemoryLedger{TotalBytes: 100, AvailableBytes: 9, StandbyBytes: 6, ModifiedBytes: 1, PagesMeasured: true},
		EmptiedProcessCount: 123,
		SkippedProcessCount: 45,
		Elevated:            true,
	}
	if err := WritePurgeResult(path, payload); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPurgeResult(path, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != EmptyWorkingSetsHelperMode || got.EmptiedProcessCount != 123 || got.SkippedProcessCount != 45 || got.ProtocolVersion != PurgeProtocolVersion {
		t.Fatalf("helper 结果写读不一致: %+v", got)
	}
	if got.AfterLedger.StandbyBytes != 6 || !got.BeforeLedger.PagesMeasured {
		t.Fatalf("账目逐项链账失真: %+v", got)
	}
}

func TestPurgeProtocolRejectsUnknownMode(t *testing.T) {
	dir := t.TempDir()
	id := "req-0123456789abcdef"
	path := filepath.Join(dir, purgeResultPrefix+id+"-1.json")
	if err := WritePurgeResult(path, PurgeResultFile{RequestID: id, Mode: "launch-missiles", State: "success"}); err == nil {
		t.Fatal("未知 mode 写入必须拒绝")
	}
	if err := os.WriteFile(path, []byte(`{"protocolVersion":2,"mode":"nope","requestId":"`+id+`","state":"success","beforeLedger":{},"afterLedger":{},"message":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPurgeResult(path, id); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("未知 mode 读取必须拒绝，got %v", err)
	}
}

func TestPurgeProtocolRejectsForeignPath(t *testing.T) {
	dir := t.TempDir()
	if err := WritePurgeResult(filepath.Join(dir, "elsewhere.json"), PurgeResultFile{RequestID: "req-0123456789abcdef", Mode: PurgeHelperMode, State: "success"}); err == nil {
		t.Fatal("非本 request 前缀文件名必须拒写")
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

func TestRunPurgeHelperRejectsUnknownMode(t *testing.T) {
	if _, err := RunPurgeHelper(`C:\hanxi.exe`, t.TempDir(), "purge-0123456789abcdef", "bogus-mode"); err == nil {
		t.Fatal("未知 helper mode 必须在拉起任何进程前拒绝")
	}
}
