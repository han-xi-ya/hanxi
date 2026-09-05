package npmtool

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// withManagerSeams 替换 lookNpm / runNpm 两个包级 seam，测试结束自动还原。
func withManagerSeams(t *testing.T, npmOK bool, run func(context.Context, []string, func(string)) (string, error)) {
	t.Helper()
	oldLook, oldRun := lookNpm, runNpm
	lookNpm = func(string) (string, error) {
		if npmOK {
			return `C:\Program Files\nodejs\npm.cmd`, nil
		}
		return "", errors.New("npm not found")
	}
	if run != nil {
		runNpm = run
	}
	t.Cleanup(func() { lookNpm, runNpm = oldLook, oldRun })
}

// waitIdle 等待后台操作收尾（finishOperation 清锁），避免用例间锁状态泄漏。
func waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for ActiveOperation() != nil {
		if time.Now().After(deadline) {
			t.Fatal("operation did not settle in time")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestManagerRejectsUnknownID(t *testing.T) {
	withManagerSeams(t, true, nil)
	// 安全红线：非目录 ID（含恶意注入串）必须同步拒绝，不得进入执行层。
	for _, id := range []string{"nope", "@evil; rm -rf"} {
		if _, err := Install(id); err == nil {
			t.Fatalf("Install(%q) should error", id)
		}
	}
}

func TestManagerRejectsWhenNpmMissing(t *testing.T) {
	withManagerSeams(t, false, nil)
	if _, err := Install("claude"); err == nil || !strings.Contains(err.Error(), "Node.js") {
		t.Fatalf("expected npm-missing error, got %v", err)
	}
}

func TestManagerLockConflict(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	fire := func() { once.Do(func() { close(release) }) }
	t.Cleanup(fire) // 仅兜底：若断言提前 t.Fatal 也要放行，避免 goroutine 泄漏占锁
	withManagerSeams(t, true, func(_ context.Context, _ []string, _ func(string)) (string, error) {
		<-release // 占住全局锁直到本用例结束
		return "", nil
	})
	if _, err := Install("claude"); err != nil {
		t.Fatalf("first install should be accepted: %v", err)
	}
	// 第二个操作必须因 npm 全局树互斥被同步拒绝。
	second, err := Uninstall("codex")
	if err == nil || !strings.Contains(err.Error(), "同一时刻") {
		t.Fatalf("expected lock conflict, got accepted=%#v err=%v", second, err)
	}
	// 在 seam 仍生效时释放并等待锁清空，防止后台 goroutine 把互斥态带进下一个用例
	// （若留到 cleanup 阶段，runNpm 已被还原，goroutine 可能读到真实实现）。
	fire()
	waitIdle(t)
}

func TestManagerSuccessTerminal(t *testing.T) {
	var gotArgs []string
	withManagerSeams(t, true, func(_ context.Context, args []string, onLine func(string)) (string, error) {
		gotArgs = args
		onLine("added 1 package")
		return "", nil
	})
	accepted, err := Install("claude")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Kind != kindInstall || accepted.OperationID == "" {
		t.Fatalf("accepted=%#v", accepted)
	}
	waitIdle(t)
	if strings.Join(gotArgs, " ") != "install -g @anthropic-ai/claude-code@latest" {
		t.Fatalf("args=%v", gotArgs)
	}
}

func TestManagerFailureTerminal(t *testing.T) {
	withManagerSeams(t, true, func(_ context.Context, _ []string, _ func(string)) (string, error) {
		return "npm ERR! 404 Not Found", errors.New("npm uninstall -g x executed failed: exit status 1")
	})
	if _, err := Uninstall("codex"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t) // 失败路径同样必须收尾清锁（defer finishOperation）
}
