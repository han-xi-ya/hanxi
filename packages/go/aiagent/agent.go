// Package aiagent 封装"调用本机 Claude Code CLI 的无头模式"这一件事。
//
// 为什么是公共内核、而不是各模块自己 spawn：今天用它的业务是"从脏日志里提取 JSON"，
// 后一天是翻译，再后天可能是摘要/解释。每处各写一遍 exec、超时、stdout/stderr 处理，
// 迟早出现"某处忘了分离 stderr 于是噪音混进结果""某处没设超时于是界面永久卡住"这种
// 不一致（与 N25「消灭孤例」同一口径）。
//
// 三条实测约束（不是推测，本仓跑通过）：
//   - Windows 下 LookPath 解析到 npm 的 claude.cmd，进程能正常起；
//   - stdout 只有模型输出，stderr 会喷配置告警与模型名告警——**两路必须分开读**，
//     用 CombinedOutput 等于把噪音喂进 JSON 解析；
//   - 本体是 GUI 程序（无控制台），拉 `.cmd` 必须注入 CREATE_NO_WINDOW，否则用户
//     按下按钮后先看见一个黑框立住几十秒（平台差异收在 child_windows.go / child_other.go
//     两个最小文件里，主流程不出现构建标签，与 supervisor 同姿势）；
//   - 一次往返是**十秒到几十秒**量级，不是毫秒：真机实测「原样大写输出一句 hello」
//     4.4s，而一次脏日志 JSON 提取（外发 362 字节）跑了 **66.6s**——任务越像"要动脑子"，
//     越不是一个 HTTP 请求的耗时。超时必须按这个量级给，UI 必须显式报在途，
//     否则用户只会觉得界面卡死。
package aiagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// 错误分类。调用方要能区分"没装 CLI"和"这次没答上来"——前者该引导配置，
// 后者只需要重试，混成一句"AI 调用失败"就没有可执行的下一步。
var (
	ErrNotInstalled = errors.New("本机未找到 Claude Code 命令行（claude）")
	ErrTimeout      = errors.New("AI 调用超时")
	ErrEmpty        = errors.New("AI 返回了空内容")
	ErrTooLarge     = errors.New("AI 返回内容超过上限")
	ErrNoInput      = errors.New("没有要交给 AI 的内容")
	// ErrCancelled 与超时分列：用户主动收手（关窗、停用模块）被报成"超时，可加长预算重试"
	// 会把人引向完全错误的下一步——重试一条已经被要求放弃的调用。
	ErrCancelled = errors.New("AI 调用被取消")
)

const (
	// DefaultTimeout 一次问答的预算。**不是按"一次网络请求"给的**：
	// 真机实测一句无脑照抄 4.4s，一次脏日志提取 66.6s（外发才 362 字节）。
	// 120s 曾按"秒级起步"拍板，66s 的实测值把它压到只剩 1.8 倍余量——输入再大一档
	// 就会从"慢"变成"必然超时且只报一句超时"。现给 3 分钟，超时仍由 ctx 强制，不留悬着进程。
	DefaultTimeout = 3 * time.Minute
	// DefaultMaxOutput 返回上限：提取任务的产物不会超过原文太多，
	// 但模型偶尔会"顺手解释一通"，不封顶就有把 UI 撑死的路径。
	DefaultMaxOutput = 4 << 20 // 4 MiB
	// DefaultMaxInput 交给模型的原文上限。**内核也设闸**：这是公共件，
	// 今天的调用方自己挡在 2 MiB，明天翻译/摘要的调用方忘了呢？
	// 内核无闸 = 把纪律留给调用方自觉，而"忘了设闸"的代价是外发一份巨型文本。
	DefaultMaxInput = 4 << 20 // 4 MiB
	// stderrNoteBytes 诊断信息保留的 stderr 尾部长度（只为让人看清"为什么失败"，
	// 绝不参与结果解析）。
	stderrNoteBytes = 400
	// stderrCapBytes stderr 的**缓冲**上限。诊断只用尾部，没理由让一个话痨 CLI
	// 把 stderr 全量读进内存（bytes.Buffer 只会自己长大，不设上限就是无界缓冲）。
	stderrCapBytes = 64 << 10 // 64 KiB
	// waitDelay 子进程被判定结束后，还愿意为它的管道等多久。
	// 本地 Go 工具链文档逐字写着：WaitDelay 为零时"I/O pipes will be read until EOF,
	// which might not occur until orphaned subprocesses of the command have also closed
	// their descriptors"——而 claude 正是 .cmd → cmd.exe → node.exe 的孙进程链。
	// 不设这道闸，ctx 超时杀掉 .cmd 那层之后 cmd.Run 仍可能永远不返回（EOF 等不来），
	// 于是 Ask 不返回、租约不释放、界面永久停在"AI 提取中"。
	waitDelay = 5 * time.Second
)

// Runner 执行一次外部命令。做成可注入是有意的：单测要能覆盖超时、超限、
// 空返回、进程失败这些分支，而不必先装一个真 CLI（也避免 CI 依赖网络与登录态）。
type Runner func(ctx context.Context, bin string, args []string, stdin []byte) (stdout, stderr []byte, err error)

// Agent 一次封装好的 CLI 调用器。
type Agent struct {
	bin    string
	run    Runner
	argsFn func(instruction string) []string
}

// New 自动发现 CLI 并构造 Agent。找不到就返回 ErrNotInstalled（不 panic、不降级）。
func New() (*Agent, error) {
	bin, err := exec.LookPath("claude")
	if err != nil || strings.TrimSpace(bin) == "" {
		return nil, ErrNotInstalled
	}
	return &Agent{bin: bin, run: execRunner, argsFn: defaultArgs}, nil
}

// defaultArgs 只使用实测验证过的参数：`-p` 无头 + `--output-format text`。
// 任务说明**不塞进命令行参数**（见 Ask 的说明），因此这里不需要赌 CLI 是否有
// --append-system-prompt 之类的旗标——用没验过的参数写进主链路，是远程变砖的老路。
func defaultArgs(string) []string {
	return []string{"-p", "--output-format", "text"}
}

// Request 一次问答。
type Request struct {
	// Instruction 是任务说明（"从下面文本里提取所有 JSON，原样返回…"），由各业务方给。
	Instruction string
	// Input 是待处理内容。它**只走 stdin**：拼进命令行参数会撞系统命令行长度上限，
	// 并把用户内容暴露在本机进程列表里。
	Input string
	// Timeout 留空用 DefaultTimeout。
	Timeout time.Duration
	// MaxOutput 留空用 DefaultMaxOutput。
	MaxOutput int
	// MaxInput 留空用 DefaultMaxInput。超限**在发出前**拒，不做"白外发一次再失败"。
	MaxInput int
}

// Result 一次问答的结果。
type Result struct {
	Text      string // stdout，已按上限截断校验
	BytesIn   int
	BytesOut  int
	ElapsedMs int64
	// StderrNote 是 stderr 尾部，仅供诊断（本机 CLI 会喷权限/模型告警）。
	// 它**不参与**任何结果判断——除了"退出码非 0 时给个人话线索"。
	StderrNote string
}

// Ask 跑一次调用。ctx 取消/超时都会杀掉子进程：exec.CommandContext 在 Windows 上
// 结束的是 .cmd 那层，孙进程残留属本机 CLI 生态的已知代价，这里不假装解决，
// 只在文档记下（真要收口得上 JobObject，那是托管进程那条线的内核，不该由文本问答引）。
func (a *Agent) Ask(ctx context.Context, req Request) (*Result, error) {
	if strings.TrimSpace(req.Input) == "" {
		return nil, ErrNoInput
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxOut := req.MaxOutput
	if maxOut <= 0 {
		maxOut = DefaultMaxOutput
	}
	// 钳在物理上限之内：stdout 的缓冲是封顶的（boundedBuffer），若允许调用方把
	// MaxOutput 调到比缓冲上限还大，就会拿到"被静默截断却自称完整"的结果——
	// 那比拒收恶劣得多。想收更多请改内核常量，而不是在调用方调个数字。
	if maxOut > stdoutCapHint {
		maxOut = stdoutCapHint
	}
	maxIn := req.MaxInput
	if maxIn <= 0 {
		maxIn = DefaultMaxInput
	}
	// 输入侧也要有闸，且**必须在发之前**判：这条链路的失败模式之一是"把一份本就该
	// 拒收的巨型文本送到模型那边，再在本地校验时失败"——白外发一次用户内容。
	if len(req.Input) > maxIn {
		return nil, fmt.Errorf("%w：输入 %d 字节 > 上限 %d", ErrTooLarge, len(req.Input), maxIn)
	}

	// 说明与内容拼在同一段 stdin 里，用固定分隔标记分开：CLI 只吃一路输入，
	// 而把说明塞进参数就等于依赖我没验证过的旗标。
	payload := req.Instruction + "\n\n---INPUT-BEGIN---\n" + req.Input + "\n---INPUT-END---\n"

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	stdout, stderr, runErr := a.run(cctx, a.bin, a.argsFn(req.Instruction), []byte(payload))
	res := &Result{
		BytesIn:    len(req.Input),
		ElapsedMs:  time.Since(start).Milliseconds(),
		StderrNote: tail(stderr, stderrNoteBytes),
	}
	// WaitDelay 收口但输出已经到手：这是"命令答完了、还有个残留进程攥着管道"的形态
	// （claude 是 .cmd → cmd.exe → node.exe 的孙进程链，正是它）。这种错误**不值得**
	// 赔上几十秒的成果——内容照收，只在诊断位上如实记下这一笔，不静默。
	if runErr != nil && len(stdout) > 0 && errors.Is(runErr, exec.ErrWaitDelay) {
		res.StderrNote = strings.TrimSpace(res.StderrNote + "｜" + runErr.Error() +
			"（输出已完整收取，残留进程占用的管道已按 WaitDelay 关闭）")
		runErr = nil
	}
	if runErr != nil {
		// 取消与超时先分：调用方 ctx 被取消（关窗、停用模块）报成"超时，可加长预算重试"
		// 是指错方向——重试一条已经被要求放弃的调用没有意义。
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(runErr, context.Canceled) {
			return nil, fmt.Errorf("%w（父上下文已取消，未重试）", ErrCancelled)
		}
		// 超时与"命令本身失败"要分开报：前者提示加长预算，后者往往是没登录/没装。
		if errors.Is(runErr, context.DeadlineExceeded) || cctx.Err() != nil {
			return nil, fmt.Errorf("%w（预算 %v，可在更长预算下重试）", ErrTimeout, timeout)
		}
		if res.StderrNote != "" {
			return nil, fmt.Errorf("AI 调用失败: %w｜CLI 提示：%s", runErr, res.StderrNote)
		}
		return nil, fmt.Errorf("AI 调用失败: %w", runErr)
	}
	if len(stdout) > maxOut {
		return nil, fmt.Errorf("%w（%d 字节 > 上限 %d）", ErrTooLarge, len(stdout), maxOut)
	}
	res.Text = strings.TrimRight(string(stdout), " \t\r\n")
	res.BytesOut = len(res.Text)
	if res.Text == "" {
		return nil, ErrEmpty
	}
	return res, nil
}

// execRunner 默认执行器：stdout / stderr 分开收（关键，见包注释）。
//
// bin 直接交给 exec.CommandContext，**不手工包 `cmd /C`**：Go 1.19 起 os/exec 在
// Windows 上会自动用 cmd.exe 跑 .bat/.cmd。这条不是推断——本机的 claude.cmd 就是
// 直传跑通的（真机往返 66s 那次命中的正是 claude.cmd）。仓内 npmtool/detect 仍写着
// "CreateProcess 无法直接启动批处理"并手工包装，两种写法都能工作，此处取少一层的那个。
func execRunner(ctx context.Context, bin string, args []string, stdin []byte) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	hideChildConsole(cmd) // GUI 本体拉控制台程序不得弹黑框（见 child_windows.go）
	// 孙进程把管道攥着不放时，不能让 Wait 无限等下去（见 waitDelay 注释）。
	// 设了它，exec 会在命令判定结束后最多再等这么久，然后强杀并关管道，Run 必返回。
	cmd.WaitDelay = waitDelay
	cmd.Stdin = bytes.NewReader(stdin)
	// 有界缓冲：先头的 `len(stdout) > maxOut` 检查是在**全量收完之后**才做的，
	// 那封顶的是"拒收体积"而不是"内存体积"——一个话痨 CLI 想吐多少就吐多少，
	// 先把内存吃掉再告诉用户超上限。这里让缓冲自己到量就停，尾部多吐的直接丢掉。
	so := &boundedBuffer{limit: stdoutCapHint}
	se := &boundedBuffer{limit: stderrCapBytes}
	cmd.Stdout = so
	cmd.Stderr = se
	err = cmd.Run()
	stdout, stderr = so.Bytes(), se.Bytes()
	if err != nil {
		// exec 的 *ExitError 里带退出码；stderr 单独留给调用方看，不并进 error
		// ——否则诊断信息会和错误文本搅成一坨，反而看不清是哪种失败。
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == -1 {
			// 被 ctx 杀掉：交回真实原因（超时/取消分别判），不在这里一律冒充超时
			if ctx.Err() != nil {
				return stdout, stderr, ctx.Err()
			}
			if cErr := cmd.ProcessState; cErr != nil {
				return stdout, stderr, context.DeadlineExceeded
			}
		}
	}
	return stdout, stderr, err
}

// stdoutCapHint 是 stdout 缓冲的**物理**上限，比 DefaultMaxOutput 宽一档：
// 拒收判据（DefaultMaxOutput，可被调用方收紧）与内存护栏是两件事，混成一个数
// 就会出现"调用方把上限调到 8 MiB 却仍被内核的 4 MiB 悄悄截掉"那种假结果。
// 真超限由 Ask 里的 len(stdout) > maxOut 如实报错，不靠这里静默丢。
const stdoutCapHint = 8 << 20 // 8 MiB

// boundedBuffer 写满即止：超出 limit 的字节直接吞掉并置 overflow。
// 不置错误、不 panic——它的职责只是"不再吃内存"，语义判断留给上层。
type boundedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len() >= b.limit {
		b.overflow = true
		return len(p), nil // 谎报已写：让 io.Copy 不因短写而报错，丢弃是本层的设计决定
	}
	n, err := b.buf.Write(p)
	if b.buf.Len() > b.limit {
		b.overflow = true
	}
	return n, err
}

func (b *boundedBuffer) Bytes() []byte { return b.buf.Bytes() }

func tail(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
