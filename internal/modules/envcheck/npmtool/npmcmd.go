package npmtool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	createNoWindow      = 0x08000000 // CREATE_NO_WINDOW：防止执行时弹出 cmd 黑框
	npmOperationTimeout = 10 * time.Minute
	npmQuickTimeout     = 10 * time.Second
	maxStreamedLines    = 2000
	tailKeepBytes       = 2048
	tailReturnBytes     = 1200
)

// lookNpm / runNpm 为包级 seam，单测替换即可覆盖 manager 全状态机而无需真实 npm。
var (
	lookNpm = exec.LookPath
	runNpm  = defaultRunNpm
)

// defaultRunNpm 执行一次 npm 命令并逐行回调合并输出（stdout+stderr）。
// 与 detect/runner.go 同款 Windows 习语，但语义不同（探测是一次性 5s 合并输出，
// 操作是 10min 流式管道），故刻意不复用其私有函数：
//   - npm 经 npm.cmd 分发，CreateProcess 无法直接启动批处理 → cmd /C 包装；
//   - CREATE_NO_WINDOW + HideWindow 防止弹出控制台黑框；
//   - 关闭进度条与赞助提示降噪，保留 audit 默认行为（安全扫描不屏蔽）；
//   - 逐行流式回调，超 maxStreamedLines 只回摘要；失败返回输出尾部供错误文案。
func defaultRunNpm(ctx context.Context, args []string, onLine func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, npmOperationTimeout)
	defer cancel()

	npm, err := lookNpm("npm")
	if err != nil {
		return "", fmt.Errorf("未在 PATH 中找到 npm，请先安装 Node.js")
	}
	cmd := buildNpmCommand(ctx, npm, args)
	cmd.Env = append(os.Environ(), "npm_config_progress=false", "npm_config_fund=false")

	// 用一条 os.Pipe 同时接 stdout/stderr 以合并流式输出；npm.cmd → node 生命周期
	// 结束后两端写句柄随进程退出关闭，读端自然 EOF。
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("创建输出管道失败: %w", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return "", fmt.Errorf("启动 npm 失败: %w", err)
	}
	// 父进程立即关闭写端副本，避免与子进程句柄引用互持导致读端永不 EOF。
	_ = pw.Close()

	tail, lines := streamOutput(pr, onLine)
	_ = pr.Close()
	waitErr := cmd.Wait()

	if ctx.Err() == context.DeadlineExceeded {
		return tail, fmt.Errorf("npm %s 超时（最长 %s，共 %d 行输出）", strings.Join(args, " "), npmOperationTimeout, lines)
	}
	if waitErr != nil {
		return tail, fmt.Errorf("npm %s 执行失败: %w", strings.Join(args, " "), waitErr)
	}
	return tail, nil
}

// buildNpmCommand 组装 exec.Cmd，.cmd/.bat 经 cmd /C 包装并隐藏窗口。
func buildNpmCommand(ctx context.Context, npm string, args []string) *exec.Cmd {
	var cmd *exec.Cmd
	switch strings.ToLower(filepath.Ext(npm)) {
	case ".cmd", ".bat":
		cmd = exec.CommandContext(ctx, "cmd", append([]string{"/C", npm}, args...)...)
	default:
		cmd = exec.CommandContext(ctx, npm, args...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

// streamOutput 逐行读取合并输出，回调有效行并保留末尾片段供错误文案。
func streamOutput(pr *os.File, onLine func(string)) (string, int) {
	var tail strings.Builder
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 64<<10), 64<<10)
	lines := 0
	for scanner.Scan() {
		line := scanner.Text()
		lines++
		switch {
		case lines <= maxStreamedLines:
			if onLine != nil {
				onLine(line)
			}
		case lines == maxStreamedLines+1:
			if onLine != nil {
				onLine("…输出行数过多，后续行已省略…")
			}
		}
		appendTail(&tail, line)
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		if onLine != nil {
			onLine(fmt.Sprintf("…读取 npm 输出中断: %v…", err))
		}
	}
	combined := tail.String()
	if len(combined) > tailReturnBytes {
		combined = combined[len(combined)-tailReturnBytes:]
	}
	return combined, lines
}

func appendTail(tail *strings.Builder, line string) {
	tail.WriteString(line)
	tail.WriteByte('\n')
	if tail.Len() > tailKeepBytes*2 {
		trimmed := tail.String()[tail.Len()-tailKeepBytes:]
		tail.Reset()
		tail.WriteString(trimmed)
	}
}

// npmGlobalBinDir 执行 npm config get prefix 取全局 bin 目录（Windows 下 npm 全局
// shim 直接落 prefix 目录本身）。仅返回字符串供展示层比较，绝不据此执行命令。
var npmGlobalBinDir = defaultNpmGlobalBinDir

func defaultNpmGlobalBinDir(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, npmQuickTimeout)
	defer cancel()
	npm, err := lookNpm("npm")
	if err != nil {
		return "", fmt.Errorf("未找到 npm")
	}
	cmd := buildNpmCommand(ctx, npm, []string{"config", "get", "prefix"})
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("查询 npm 全局目录失败: %w", err)
	}
	prefix := strings.TrimSpace(string(out))
	if prefix == "" {
		return "", fmt.Errorf("npm 未返回全局 prefix")
	}
	return prefix, nil
}
