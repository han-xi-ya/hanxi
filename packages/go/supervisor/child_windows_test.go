//go:build windows

package supervisor

import "testing"

// TestDefaultOpenerHideWindowWindows 真实断言 Windows 下 CREATE_NO_WINDOW +
// HideWindow 标志注入（fake opener 链路只能断言透传，见 engine_test.go）。
func TestDefaultOpenerHideWindowWindows(t *testing.T) {
	h, err := defaultOpener(Spec{Exe: `C:\tool\tool.exe`, HideWindow: true})
	if err != nil {
		t.Fatalf("defaultOpener: %v", err)
	}
	attr := h.(*execHandle).cmd.SysProcAttr
	if attr == nil {
		t.Fatal("HideWindow=true 应注入 SysProcAttr")
	}
	if attr.CreationFlags != createNoWindow {
		t.Fatalf("CreationFlags = %#x, want %#x (CREATE_NO_WINDOW)", attr.CreationFlags, createNoWindow)
	}
	if !attr.HideWindow {
		t.Fatal("SysProcAttr.HideWindow 应为 true")
	}
}
