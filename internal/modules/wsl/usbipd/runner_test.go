package usbipd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// ---- PATH 失效回退（winget/MSI 机器级安装后运行中进程看不到新 PATH） ----

// whereExe 找一个必然存在、跑得快且必然非零退出的系统工具，充当
// "MSI 固定落位命中"的替身目标（真实执行路径要过 exec，替身命令必须可用）。
func whereExe(t *testing.T) string {
	t.Helper()
	root := os.Getenv("SystemRoot")
	if root == "" {
		t.Skip("SystemRoot 不可得，无法构造回退目标")
	}
	p := filepath.Join(root, "System32", "where.exe")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("where.exe 不可用: %v", err)
	}
	return p
}

func TestRunnerPathStaleFallback(t *testing.T) {
	if _, err := exec.LookPath("usbipd"); err == nil {
		t.Skip("本机已安装 usbipd（PATH 命中），测不了「PATH 失效」分支")
	}
	old := findKnownExe
	t.Cleanup(func() { findKnownExe = old })

	t.Run("命中固定落位改挂绝对路径并粘住", func(t *testing.T) {
		target := whereExe(t)
		probes := 0
		findKnownExe = func() string { probes++; return target }
		r := NewRunner()
		_, err := r.Version(context.Background())
		var ee *ExecError
		if !errors.As(err, &ee) {
			t.Fatalf("回退命中后应暴露目标命令自己的失败（而非未安装哨兵）: %v", err)
		}
		if r.currentExe() != target {
			t.Errorf("命中后应粘住绝对路径: %q", r.currentExe())
		}
		if _, err := r.Version(context.Background()); err == nil {
			t.Fatal("where.exe 对 --version 应非零退出")
		}
		if probes != 1 {
			t.Errorf("粘住后不得反复回查固定落位: probes=%d", probes)
		}
	})

	t.Run("未命中如实回未安装且每轮可再查", func(t *testing.T) {
		probes := 0
		findKnownExe = func() string { probes++; return "" }
		r := NewRunner()
		if _, err := r.Version(context.Background()); !errors.Is(err, ErrNotInstalled) {
			t.Fatalf("双通道都没命中必须是 ErrNotInstalled: %v", err)
		}
		if _, err := r.Version(context.Background()); !errors.Is(err, ErrNotInstalled) {
			t.Fatal(err)
		}
		if probes != 2 {
			t.Errorf("未命中不落粘滞锁——运行期间外部装好后下一轮探测要能自救: probes=%d", probes)
		}
		if r.currentExe() != "usbipd" {
			t.Errorf("未命中不得改写解析目标: %q", r.currentExe())
		}
	})
}
