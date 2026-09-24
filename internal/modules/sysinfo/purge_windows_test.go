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
	if err == nil || !strings.Contains(err.Error(), "request ID") {
		t.Fatalf("应拒绝与 request ID 不匹配的结果路径，got %v", err)
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
	if err := windows.WritePurgeResult(path, id, "success", "ok", "", 5, 9, true); err != nil {
		t.Fatal(err)
	}
	got, err := windows.ReadPurgeResult(path, id)
	if err != nil || got.State != "success" || got.AfterAvailableBytes != 9 || !got.Elevated {
		t.Fatalf("helper 结果写读不一致: %+v %v", got, err)
	}
}
