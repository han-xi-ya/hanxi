package aiagent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// execRunner 这一层此前**一个用例都没有**：超时/超限/空返回全走注入的假 runner，
// 而包注释那两条承诺（"两路必须分开读""超时由 ctx 强制，不留悬着进程"）恰恰只在
// 真进程上才兑现——假 runner 全绿等于给它们盖了个"已测"的空章。
//
// 子进程用测试二进制自重（-test.run 指到 helper 上）：跨平台、不赌目标机上有没有
// cat/sleep/timeout，也不依赖网络与登录态。

const (
	echoEnv    = "HANXI_AI_PROC_HELPER_ECHO"
	holdEnv    = "HANXI_AI_PROC_HELPER_HOLD"
	grandEnv   = "HANXI_AI_PROC_HELPER_GRAND"
	sleepEnv   = "HANXI_AI_PROC_HELPER_SLEEP"
	stderrMark = "AIAGENT-TEST-STDERR-MARKER"
)

// TestHelperEchoStdin 子进程形态：stdin 原样抄到 stdout，另往 stderr 写一枚标记。
// 父进程里（未置环境变量时）它只是一个 skip。
func TestHelperEchoStdin(t *testing.T) {
	if os.Getenv(echoEnv) != "1" {
		t.Skip("非子进程形态：跳过")
	}
	// 测试框架自己会往 stdout 打 PASS，所以父侧断言一律用 contains 而不是相等
	if _, err := os.Stdout.Write([]byte("ECHO:" + os.Getenv("HANXI_AI_PAYLOAD"))); err != nil {
		t.Fatal(err)
	}
	os.Stderr.WriteString(stderrMark + "\n")
}

// TestHelperHoldsPipeOpen 子进程形态：再起一个孙进程占住 stdout 管道，自己立刻退出。
// 这正是 claude 的真实形状——.cmd → cmd.exe → node.exe，杀掉头一层不等于管道关闭。
// 没有 WaitDelay 的话，父侧的 Run 会一直等 EOF 而永远不返回。
func TestHelperHoldsPipeOpen(t *testing.T) {
	if os.Getenv(holdEnv) != "1" {
		t.Skip("非子进程形态：跳过")
	}
	// 用**独立的**环境变量标记孙进程形态：先前复用 holdEnv 传 "grand" 时，父侧
	// t.Setenv 留下的 holdEnv=1 排在前面、把追加的那个顶掉了，孙进程直接走 skip
	// 分支退出——管道没人占，父侧 0.01s 就返回，这条用例于是变成空转还报绿。
	grand := exec.Command(os.Args[0], "-test.run=^TestHelperSleepsHoldingStdout$")
	grand.Env = append(os.Environ(), grandEnv+"=1")
	// 必须**显式**把 os.Stdout 交出去：留 nil 时 Go 走"继承父进程标准句柄"的路径，
	// 实测那条路不把管道写端 dup 给孙进程（父进程一退，EOF 立刻到，WaitDelay 根本没机会
	// 出场——表现就是用例 13ms 报绿）。显式赋 *os.File 才走句柄继承，孙进程真占住管道。
	grand.Stdout = os.Stdout
	grand.Stderr = os.Stderr
	if err := grand.Start(); err != nil {
		t.Fatal(err)
	}
	// 先写一段"成果"，再退场：用来验输出不会被收口动作赔掉
	os.Stdout.WriteString("PARTIAL-RESULT\n")
	// 不 Wait、不 Close：立刻退出，把已继承的 stdout 管道留给孙进程持有
}

// TestHelperSleepsHoldingStdout 孙进程：不写、不关，只是占着管道睡一会儿。
func TestHelperSleepsHoldingStdout(t *testing.T) {
	if os.Getenv(grandEnv) != "1" {
		t.Skip("非孙进程形态：跳过")
	}
	time.Sleep(30 * time.Second)
}

// TestExecRunnerRealProcessSeparatesStreams 真进程验"stdout 与 stderr 必须分开收"：
// 混流的话 stderr 告警会直接喂进 JSON 解析（这是包注释里的第一条实测约束）。
func TestExecRunnerRealProcessSeparatesStreams(t *testing.T) {
	t.Setenv(echoEnv, "1")
	t.Setenv("HANXI_AI_PAYLOAD", "MARKER-用户内容-9007199254740993")

	stdout, stderr, err := execRunner(context.Background(), os.Args[0],
		[]string{"-test.run=^TestHelperEchoStdin$"}, []byte("ignored"))
	if err != nil {
		t.Fatalf("真进程跑不起来（环境不满足，非功能缺陷）: %v", err)
	}
	out := string(stdout)
	errs := string(stderr)
	if !strings.Contains(out, "MARKER-用户内容-9007199254740993") {
		t.Errorf("stdout 应含子进程输出，实得 %q", out)
	}
	if strings.Contains(out, stderrMark) {
		t.Errorf("stderr 混进了 stdout（CombinedOutput 式错误）：%q", out)
	}
	if !strings.Contains(errs, stderrMark) {
		t.Errorf("stderr 应另路收到，实得 %q", errs)
	}
}

// TestExecRunnerReturnsDespiteOrphanHoldingPipe 是本文件存在的理由：
// 孙进程攥着 stdout 不放时，Run 也必须返回。这条由 Go 工具链文档逐字预告
// （exec.go：WaitDelay 为零时管道会读到 EOF 为止，而 EOF 可能要等孤儿进程也关掉句柄），
// 此前只是注释里的承诺，实测在这里第一次落成真凭据。
func TestExecRunnerReturnsDespiteOrphanHoldingPipe(t *testing.T) {
	t.Setenv(holdEnv, "1")
	start := time.Now()
	type outcome struct {
		stdout []byte
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		so, _, err := execRunner(context.Background(), os.Args[0],
			[]string{"-test.run=^TestHelperHoldsPipeOpen$"}, nil)
		done <- outcome{so, err}
	}()
	var got outcome
	select {
	case got = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("WaitDelay 没有生效：孙进程占住管道时 Run 悬挂（用户界面会永久停在「AI 提取中」）")
	}
	el := time.Since(start)
	// 这一层测的是**机制**：等满 WaitDelay 才收口。两端都要卡——上限证明不是等孙进程
	// 睡完才回，**下限证明孙进程真的占住了管道**。没有下限，夹具一失效就会 0.01s 报绿，
	// 我第一次跑正是被骗成空转（那等于给 WaitDelay 发了张没验过的合格证）。
	if el < waitDelay-2*time.Second {
		t.Errorf("返回得太快（%v < WaitDelay %v）：孙进程没有占住管道，这条用例是空转", el, waitDelay)
	}
	if el > 12*time.Second {
		t.Errorf("返回得太慢（%v），说明等待是靠孙进程睡完才结束的，不是 WaitDelay 收的", el)
	}
	// 机制的如实形状：Go 会把它报成 ErrWaitDelay（不是"命令失败"，也不是零错误）；
	// 怎么处理这个错误是 Ask 层的契约，见下一条用例。
	if !errors.Is(got.err, exec.ErrWaitDelay) {
		t.Errorf("应报 exec.ErrWaitDelay，实得 %v", got.err)
	}
	if !strings.Contains(string(got.stdout), "PARTIAL-RESULT") {
		t.Errorf("收口动作不该丢掉已收到的 stdout，实得 %q", got.stdout)
	}
}

// TestAskKeepsResultWhenPipeClosedByWaitDelay 契约层：残留进程攥着管道时，
// 一次已经答完的调用**不得**被赔给收口动作——实测一次提取要 66 秒，
// 因为一个不影响结果的句柄没关就报"AI 调用失败"，等于让用户白等。
// 同时不能静默：这一笔要留在诊断位上。
func TestAskKeepsResultWhenPipeClosedByWaitDelay(t *testing.T) {
	t.Setenv(holdEnv, "1")
	a := &Agent{bin: os.Args[0], run: execRunner, argsFn: func(string) []string {
		return []string{"-test.run=^TestHelperHoldsPipeOpen$"}
	}}
	res, err := a.Ask(context.Background(), Request{Instruction: "i", Input: "y", Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("输出已到手时不该报失败（这是几十秒的成果）: %v", err)
	}
	if !strings.Contains(res.Text, "PARTIAL-RESULT") {
		t.Errorf("结果文本应含子进程输出，实得 %q", res.Text)
	}
	if !strings.Contains(res.StderrNote, "WaitDelay") {
		t.Errorf("收口这一笔必须留在诊断位，不能静默：%q", res.StderrNote)
	}
}

// TestHelperSleeps 子进程形态：什么都不写，睡到被杀。用来验超时回收。
func TestHelperSleeps(t *testing.T) {
	if os.Getenv(sleepEnv) != "1" {
		t.Skip("非子进程形态：跳过")
	}
	time.Sleep(30 * time.Second)
}

// TestAskRealProcessTimeoutIsBounded 端到端（Ask 层，走真 execRunner）：
// 子进程不起时，超时必须真的把调用收回来，而不是把租约挂死。
// 断言的是**外部事实**：调用在"预算 + WaitDelay + 一点余量"之内返回，且归为超时。
func TestAskRealProcessTimeoutIsBounded(t *testing.T) {
	t.Setenv(sleepEnv, "1")
	a := &Agent{bin: os.Args[0], run: execRunner, argsFn: func(string) []string {
		return []string{"-test.run=^TestHelperSleeps$"}
	}}
	start := time.Now()
	_, err := a.Ask(context.Background(), Request{Instruction: "x", Input: "y", Timeout: time.Second})
	el := time.Since(start)
	if !strings.Contains(err.Error(), "超时") {
		t.Errorf("应归为超时，实得 %v", err)
	}
	// 预算 1s + WaitDelay 5s，留 4s 调度余量：超出去就是"超时了却没把进程与管道收掉"
	if el > 10*time.Second {
		t.Errorf("超时后没有把调用收回来（耗时 %v，预期在预算+WaitDelay 内返回）", el)
	}
}

// TestAskInputCapRejectsBeforeSpawn 内核层的输入闸：超限必须报错，
// 而不是把一份巨型文本送出去再等失败。
func TestAskInputCapRejectsBeforeSpawn(t *testing.T) {
	var call fakeCall
	a := agentWith(fakeRunner(&call, []byte("ok"), nil, nil))
	_, err := a.Ask(context.Background(), Request{Instruction: "i", Input: strings.Repeat("x", DefaultMaxInput+1)})
	if err == nil || !strings.Contains(err.Error(), "输入") {
		t.Fatalf("超限输入应在发之前拒，实得 %v", err)
	}
	if call.stdin != "" {
		t.Error("拒收时不得真的把内容交给子进程")
	}
}
