package usbipd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// fakeExit 轻量退出码替身（exec.ExitError 的 ProcessState 伪造不了）。
type fakeExit struct {
	code int
	text string
}

func (f *fakeExit) Error() string { return f.text }
func (f *fakeExit) ExitCode() int { return f.code }

func fakeRun(stdout, stderr string, err error) RunFunc {
	return func(context.Context, ...string) (string, string, error) {
		return stdout, stderr, err
	}
}

// captureRun 记录最近一次参数并回放固定输出。
func captureRun(stdout string, err error) (RunFunc, *[]string) {
	var got []string
	return func(_ context.Context, args ...string) (string, string, error) {
		got = args
		return stdout, "", err
	}, &got
}

func TestRunnerVersionOK(t *testing.T) {
	r, args := captureRun("usbipd 5.3.0+54 (branch x)\n", nil)
	v, err := NewRunnerWith(r).Version(context.Background())
	if err != nil || v != "5.3.0" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	if strings.Join(*args, " ") != "--version" {
		t.Errorf("探测命令应为 --version, got %v", *args)
	}
}

func TestRunnerVersionNotInstalled(t *testing.T) {
	r, _ := captureRun("", exec.ErrNotFound)
	_, err := NewRunnerWith(r).Version(context.Background())
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("未安装必须命中 ErrNotInstalled 哨兵: %v", err)
	}
}

func TestRunnerVersionGarbageOutput(t *testing.T) {
	// 命令在跑但输出里没有版本号（如 PATH 被冒名劫持）：不得谎报已装。
	r, _ := captureRun("usbipd: unknown invocation", nil)
	if _, err := NewRunnerWith(r).Version(context.Background()); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("异常输出应归为未安装: %v", err)
	}
}

func TestRunnerStateParsesAndPassesArgs(t *testing.T) {
	r, args := captureRun(sampleState, nil)
	devs, err := NewRunnerWith(r).State(context.Background())
	if err != nil || len(devs) != 4 {
		t.Fatalf("State = %d 行, %v", len(devs), err)
	}
	if strings.Join(*args, " ") != "state" {
		t.Errorf("取数命令应为 state, got %v", *args)
	}
}

func TestRunnerStateExitErrorChinese(t *testing.T) {
	r, _ := captureRun("", &fakeExit{code: 3, text: "Access is denied"})
	_, err := NewRunnerWith(r).State(context.Background())
	var ee *ExecError
	if !errors.As(err, &ee) || ee.Code != 3 {
		t.Fatalf("非零退出应带类型化凭证: %v", err)
	}
	if !strings.Contains(ee.Error(), "管理员") {
		t.Errorf("AccessDenied 应中文化: %v", ee)
	}
}

func TestRunnerAttachDetachArgs(t *testing.T) {
	r, args := captureRun("", nil)
	run := NewRunnerWith(r)
	if err := run.Attach(context.Background(), "Ubuntu", "1-4"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*args, " "); got != "attach --wsl Ubuntu --busid 1-4" {
		t.Errorf("attach 命令 = %q", got)
	}
	if err := run.Detach(context.Background(), "1-4"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*args, " "); got != "detach --busid 1-4" {
		t.Errorf("detach 命令 = %q", got)
	}
	if err := run.Attach(context.Background(), "Ubuntu", "bad;id"); err == nil {
		t.Error("坏 busid 不得触达命令通道")
	}
}

func TestRunnerAttachFriendlyErrors(t *testing.T) {
	r, _ := captureRun("Device is not shared; run 'usbipd bind --busid 1-4' as administrator first.", &fakeExit{code: 1})
	err := NewRunnerWith(r).Attach(context.Background(), "Ubuntu", "1-4")
	if err == nil || !strings.Contains(err.Error(), "设备尚未共享") {
		t.Fatalf("attach 未共享失败应中文化: %v", err)
	}
}

func TestDecodeMaybeUTF16(t *testing.T) {
	// UTF-16LE "ok" 字节流：含 NUL、偶数长，必须解回 ASCII。
	u16 := []byte{'o', 0, 'k', 0}
	if got := decodeMaybeUTF16(u16); got != "ok" {
		t.Errorf("UTF-16 防呆解码失败: %q", got)
	}
	if got := decodeMaybeUTF16([]byte("ok")); got != "ok" {
		t.Errorf("UTF-8 路径不该被误改: %q", got)
	}
}

func TestExecErrorNoPanicOnUnknown(t *testing.T) {
	ee := &ExecError{Code: -1, Args: []string{"state"}, Output: ""}
	if !strings.Contains(fmt.Sprint(ee), "退出码") {
		t.Errorf("兜底文案缺失: %v", ee)
	}
}
