package wsl

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// opTimeout 覆盖「一键开启」「MSI 卸载向导」等全程；
// 提权控制台窗口保持可见，用户能实时看到进度，本方法同步等待其结束。
const opTimeout = 30 * time.Minute

// runElevatedProcess 以 UAC 提权运行固定程序与参数（portkill KillProcessElevated 同款通道）。
// file/args 一律来自后端常量白名单与严格校验值，逐一经单引号转义后交 Start-Process；
// 提权窗口保持可见（-WindowStyle Normal），完成后 -Wait 同步返回。
func runElevatedProcess(ctx context.Context, file string, args ...string) (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, "'"+strings.ReplaceAll(a, "'", "''")+"'")
	}
	ps := fmt.Sprintf(
		"Start-Process -FilePath '%s' -ArgumentList %s -Verb RunAs -WindowStyle Normal -Wait",
		strings.ReplaceAll(file, "'", "''"), strings.Join(quoted, ","),
	)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} // 隐藏的是承载 Start-Process 的 powershell，不是提权后的目标窗口
	out, err := cmd.CombinedOutput()
	if err != nil {
		s := strings.ToLower(string(out))
		if strings.Contains(s, "canceled by the user") || strings.Contains(s, "user cancelled") || strings.Contains(s, "1223") || strings.Contains(string(out), "已取消") {
			return OperationOutcome{Message: "已取消 UAC 授权，操作未执行"}, nil
		}
		if ctx.Err() == context.DeadlineExceeded {
			return OperationOutcome{}, fmt.Errorf("提权操作超时未完成: %w", ctx.Err())
		}
		// DISM 等工具的失败信息提纯：进度条噪音里捞出最后一条错误行，
		// 让用户看得到真实失败原因而不是几十行百分号。
		if line := lastErrorLine(string(out)); line != "" {
			return OperationOutcome{}, fmt.Errorf("提权执行 %s 失败: %s", file, line)
		}
		return OperationOutcome{}, fmt.Errorf("提权执行 %s 失败: %w %s", file, err, strings.TrimSpace(string(out)))
	}
	return OperationOutcome{Success: true, Message: "操作已执行完毕，状态已按最新结果刷新"}, nil
}

// runElevatedWsl 提权执行固定参数的 wsl.exe。
func runElevatedWsl(ctx context.Context, args ...string) (OperationOutcome, error) {
	return runElevatedProcess(ctx, "wsl.exe", args...)
}

// lastErrorLine 从 DISM/工具输出中提取最后一条错误行（中英语境皆覆盖）；无则空串。
func lastErrorLine(out string) string {
	found := ""
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "错误:") || strings.HasPrefix(line, "错误 [") ||
			strings.HasPrefix(strings.ToLower(line), "error:") || strings.HasPrefix(strings.ToLower(line), "error 0x") {
			found = line
		}
	}
	return found
}

// runLocalPS 以普通权限隐藏窗口执行固定 PowerShell 脚本并回传 trim 后的输出。
func runLocalPS(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
