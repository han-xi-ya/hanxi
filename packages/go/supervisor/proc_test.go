package supervisor

import (
	"os"
	"reflect"
	"runtime"
	"testing"
)

// ---------- defaultOpener：Env 追加与 HideWindow 平台钩子（跨平台可编译断言） ----------

func TestDefaultOpenerEnvAppend(t *testing.T) {
	h, err := defaultOpener(Spec{Exe: "tool.exe", Env: []string{"DDNS_GO_DAEMON=1"}})
	if err != nil {
		t.Fatalf("defaultOpener: %v", err)
	}
	want := append(os.Environ(), "DDNS_GO_DAEMON=1")
	if got := h.(*execHandle).cmd.Env; !reflect.DeepEqual(got, want) {
		t.Fatal("Env 应追加在继承的当前进程环境之后")
	}
}

func TestDefaultOpenerEnvEmptyInherits(t *testing.T) {
	h, err := defaultOpener(Spec{Exe: "tool.exe"})
	if err != nil {
		t.Fatalf("defaultOpener: %v", err)
	}
	if h.(*execHandle).cmd.Env != nil {
		t.Fatal("Env 空应维持 cmd.Env==nil 的纯继承语义（键值不丢失）")
	}
}

// TestDefaultOpenerHideWindowHook 跨平台只断言钩子生效方向（平台字段细节见
// child_windows_test.go）：Windows 注入 SysProcAttr，其他平台 no-op。
func TestDefaultOpenerHideWindowHook(t *testing.T) {
	h, err := defaultOpener(Spec{Exe: "tool.exe", HideWindow: true})
	if err != nil {
		t.Fatalf("defaultOpener: %v", err)
	}
	attr := h.(*execHandle).cmd.SysProcAttr
	if runtime.GOOS == "windows" {
		if attr == nil {
			t.Fatal("windows 上 HideWindow 应注入 SysProcAttr")
		}
	} else if attr != nil {
		t.Fatal("非 windows 平台 HideWindow 应为 no-op")
	}

	h2, err := defaultOpener(Spec{Exe: "tool.exe"})
	if err != nil {
		t.Fatalf("defaultOpener: %v", err)
	}
	if h2.(*execHandle).cmd.SysProcAttr != nil {
		t.Fatal("未请求 HideWindow 时不应触碰 SysProcAttr")
	}
}
