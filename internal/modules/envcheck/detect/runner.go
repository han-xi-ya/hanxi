package detect

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	createNoWindow = 0x08000000 // CREATE_NO_WINDOW：防止探测时弹出 cmd 黑框
	probeTimeout   = 5 * time.Second
)

// runVersionCommand 执行一次性版本探测命令并返回合并输出。
// CombinedOutput 而非 Output：java 的 -version 输出打在 stderr 上，必须合并兜底。
// Windows 下 npm/pnpm 等经 npm.cmd 分发，CreateProcess 无法直接启动批处理文件，
// 须包装为 cmd /C 执行（与项目 instance 测试的 cmd.exe 习语一致）。
func runVersionCommand(ctx context.Context, exe string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch strings.ToLower(filepath.Ext(exe)) {
	case ".cmd", ".bat":
		full := append([]string{"/C", exe}, args...)
		cmd = exec.CommandContext(ctx, "cmd", full...)
	default:
		cmd = exec.CommandContext(ctx, exe, args...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("执行超时（%s）", probeTimeout)
		}
		return "", fmt.Errorf("执行失败: %w", err)
	}
	return string(out), nil
}
