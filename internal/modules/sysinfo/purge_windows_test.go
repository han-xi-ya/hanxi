//go:build windows

package sysinfo

import (
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/platform/windows"
)

func TestRunPurgeStandbyHelperRejectsForeignPath(t *testing.T) {
	dir := t.TempDir()
	err := RunPurgeStandbyHelper(filepath.Join(dir, "elsewhere.json"), "purge-request", false)
	// 审查 P1#6 收紧后由 runtime 目录钉死校验先行拒止——任何拒绝文案均可，
	// 关键是**必须拒**（旧契约只验 ID 前缀，可被同形状他目录路径绕过）。
	if err == nil || !strings.Contains(err.Error(), "拒") {
		t.Fatalf("应拒绝外目录/不匹配的结果路径，got %v", err)
	}
}

func TestLaunchPurgeHelperElevatedRequiresPaths(t *testing.T) {
	if _, err := LaunchPurgeHelperElevated("", `C:\hanxi.exe`, "purge-1234567890"); err == nil {
		t.Fatal("空 runtime 目录必须拒绝")
	}
	if _, err := LaunchPurgeHelperElevated(`C:\runtime`, "", "purge-1234567890"); err == nil {
		t.Fatal("空宿主程序必须拒绝")
	}
}

func TestWritePurgeResultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id := "purge-0123456789abcdef"
	path := filepath.Join(dir, "hanxi-purge-"+id+"-x.json")
	payload := windows.PurgeResultFile{
		RequestID: id, Mode: windows.PurgeHelperMode, State: "success", Message: "ok",
		BeforeAvailableBytes: 5, AfterAvailableBytes: 9,
		BeforeLedger: windows.MemoryLedger{TotalBytes: 100, AvailableBytes: 5, PagesMeasured: false},
		AfterLedger:  windows.MemoryLedger{TotalBytes: 100, AvailableBytes: 9, PagesMeasured: false},
		Elevated:     true,
	}
	if err := windows.WritePurgeResult(path, payload); err != nil {
		t.Fatal(err)
	}
	got, err := windows.ReadPurgeResult(path, id)
	if err != nil || got.State != "success" || got.AfterAvailableBytes != 9 || !got.Elevated {
		t.Fatalf("helper 结果写读不一致: %+v %v", got, err)
	}
}

func TestRunEmptyWorkingSetsHelperRejectsForeignPath(t *testing.T) {
	dir := t.TempDir()
	err := RunEmptyWorkingSetsHelper(filepath.Join(dir, "elsewhere.json"), "ews-request", false)
	if err == nil || !strings.Contains(err.Error(), "拒") {
		t.Fatalf("workingsets helper 同样必须拒绝外目录结果路径，got %v", err)
	}
}

func TestLaunchEmptyWorkingSetsHelperElevatedRequiresPaths(t *testing.T) {
	if _, err := LaunchEmptyWorkingSetsHelperElevated("", `C:\hanxi.exe`, "ews-1234567890abc"); err == nil {
		t.Fatal("空 runtime 目录必须拒绝")
	}
	if _, err := LaunchEmptyWorkingSetsHelperElevated(`C:\runtime`, "x", "ews-1234567890abc"); err == nil {
		t.Fatal("空宿主程序必须拒绝")
	}
}
