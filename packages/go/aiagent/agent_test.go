package aiagent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// aiagent 的用例面。核心是**用注入的假 runner 覆盖各条失败路径**——
// 这些分支若不注入就得依赖真装一个 CLI 并有网络/登录态，CI 上必然飘。

// fakeCall 记下假 runner 收到的东西。argsText 用拼接形态存：这里要断言的是
// "内容有没有混进命令行"，逐个元素比对反而模糊。
type fakeCall struct {
	bin      string
	argsText string
	stdin    string
}

func fakeRunner(call *fakeCall, stdout, stderr []byte, err error) Runner {
	return func(_ context.Context, bin string, args []string, stdin []byte) ([]byte, []byte, error) {
		call.bin, call.argsText, call.stdin = bin, strings.Join(args, " "), string(stdin)
		return stdout, stderr, err
	}
}

func agentWith(r Runner) *Agent {
	return &Agent{bin: "fake-claude", run: r, argsFn: defaultArgs}
}

// TestInputNeverReachesArgv 是这里最该锁的一条：待处理内容只走 stdin。
// 进命令行参数的两种死法都在眼前——撞上系统命令行长度上限（日志动辄几百 KB），
// 以及本机进程列表里明文躺着用户数据。
func TestInputNeverReachesArgv(t *testing.T) {
	var call fakeCall
	secret := "MARKER-用户日志内容-9007199254740993"
	a := agentWith(fakeRunner(&call, []byte("ok"), nil, nil))

	if _, err := a.Ask(context.Background(), Request{
		Instruction: "提取 JSON",
		Input:       secret,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(call.stdin, secret) {
		t.Error("内容应从 stdin 进入")
	}
	if strings.Contains(call.argsText, secret) {
		t.Errorf("内容泄进了命令行参数：%s", call.argsText)
	}
	if !strings.Contains(call.stdin, "---INPUT-BEGIN---") {
		t.Error("说明与内容之间要有固定分隔标记（消费方据此定位）")
	}
	if !strings.Contains(call.stdin, "提取 JSON") {
		t.Error("任务说明也要一起进 stdin")
	}
}

// TestStderrNoiseNeverEntersResult 对应实测事实：本机 CLI 会往 stderr 喷权限告警
// 与模型名告警。若图省事用 CombinedOutput，这些就直接混进 JSON 里了。
func TestStderrNoiseNeverEntersResult(t *testing.T) {
	var call fakeCall
	out := []byte("{\n  \"a\": 1\n}\n")
	noise := []byte("Permission allow rule ... unrecognized_model ...")
	a := agentWith(fakeRunner(&call, out, noise, nil))

	res, err := a.Ask(context.Background(), Request{Instruction: "x", Input: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "Permission") || strings.Contains(res.Text, "unrecognized") {
		t.Errorf("stderr 噪音混进了结果：%q", res.Text)
	}
	if !strings.Contains(res.Text, `"a": 1`) {
		t.Errorf("结果不对：%q", res.Text)
	}
	if !strings.Contains(res.StderrNote, "Permission") {
		t.Errorf("stderr 仍需留作诊断线索：%q", res.StderrNote)
	}
}

// TestTimeoutIsDistinguishableFromFailure 超时要单独认出来：提示加长预算是有用信息，
// 而"AI 调用失败"会让人去查网络或登录态。
func TestTimeoutIsDistinguishableFromFailure(t *testing.T) {
	r := func(ctx context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	a := agentWith(r)
	_, err := a.Ask(context.Background(), Request{Instruction: "x", Input: "y", Timeout: 40 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("应归为超时，实得 %v", err)
	}
	if !strings.Contains(err.Error(), "预算") {
		t.Errorf("要给出可执行的下一步：%v", err)
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name  string
		stdin string
		out   []byte
		err   error
		want  error
	}{
		{"空内容不算调用", "", nil, nil, ErrNoInput},
		{"空返回要报错", "y", []byte("  \n"), nil, ErrEmpty},
		{"超限要拒绝而非截断", "y", []byte(strings.Repeat("z", 40)), nil, ErrTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var call fakeCall
			a := agentWith(fakeRunner(&call, c.out, nil, c.err))
			_, err := a.Ask(context.Background(), Request{Instruction: "i", Input: c.stdin, MaxOutput: 16})
			if !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
}

// TestCLIHintGoesIntoError 退出码非 0 时，CLI 自己的提示要出现在错误里：
// "未登录"和"参数不对"用户能自己修，只回一句 exit status 1 就等于让人去猜。
func TestCLIHintGoesIntoError(t *testing.T) {
	var call fakeCall
	a := agentWith(fakeRunner(&call, nil, []byte("Not logged in · please run /login"), errors.New("exit status 1")))
	_, err := a.Ask(context.Background(), Request{Instruction: "i", Input: "y"})
	if err == nil {
		t.Fatal("应报错")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("CLI 提示应进错误文本：%v", err)
	}
}

func TestNewReportsMissingCLI(t *testing.T) {
	t.Setenv("PATH", string(os.PathListSeparator)) // 清空可执行搜索路径
	if _, err := New(); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("找不到 CLI 时应报 ErrNotInstalled，实得 %v", err)
	}
}

// TestLiveCLIRoundTrip 真机夹具：默认 skip，经环境变量显式开启。
// 沿本仓 HANXI_LIVE_PROXY / HANXI_HEU_RAR 的先例——真实外部依赖不进 CI，
// 但机主一条命令就能验"这套壳到底能不能驱动本机 CLI"。
func TestLiveCLIRoundTrip(t *testing.T) {
	if os.Getenv("HANXI_AI_LIVE") == "" {
		t.Skip("未设 HANXI_AI_LIVE=1：跳过真机 CLI 往返")
	}
	a, err := New()
	if err != nil {
		t.Fatalf("发现 CLI 失败: %v", err)
	}
	res, err := a.Ask(context.Background(), Request{
		Instruction: "把下面这段文本原样大写输出，不要任何解释、不要加引号。",
		Input:       "hello world",
		Timeout:     90 * time.Second,
	})
	if err != nil {
		// Ask 的所有错误分支都返回 nil Result——想在这里取 res.StderrNote 就是
		// 拿 nil 解引用，真机一旦失败反而看不到原因（夹具的唯一用途就是报出原因）。
		// 需要 stderr 的话，只能靠错误文本自己带上（Ask 已经把 CLI 提示拼进去了）。
		t.Fatalf("真机往返失败: %v", err)
	}
	if !strings.Contains(strings.ToUpper(res.Text), "HELLO WORLD") {
		t.Errorf("返回不含预期内容：%q", res.Text)
	}
	t.Logf("真机往返成功：%dms，出 %d 字节", res.ElapsedMs, res.BytesOut)
}
