package wsl

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	platformwin "hanxi/internal/platform/windows"
)

// opTimeout 覆盖「一键开启」「MSI 卸载向导」等全程；
// 提权控制台窗口保持可见，用户能实时看到进度，本方法同步等待其结束。
const opTimeout = 30 * time.Minute

// buildElevatedPS 拼装提权执行脚本。-PassThru 拿到目标进程对象并把其退出码
// 显式传播为本脚本的退出码：Start-Process 本身从不设置 $LASTEXITCODE，
// 裸 -Wait 时目标程序失败也一律返回 0——wsl --install -d 失败被误报"执行完毕"
// 的实机事故所系（#36 分号链吞码的同族病灶，那是 DISM 链、这是裸提权通道）。
func buildElevatedPS(file string, args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, psQuote(a))
	}
	return fmt.Sprintf(
		"$p = Start-Process -FilePath %s -ArgumentList %s -Verb RunAs -WindowStyle Normal -Wait -PassThru; "+
			"if ($null -ne $p -and $p.ExitCode -ne 0) { exit $p.ExitCode }",
		psQuote(file), strings.Join(quoted, ","),
	)
}

// runElevatedProcess 以 UAC 提权运行固定程序与参数（portkill KillProcessElevated 同款通道）。
// file/args 一律来自后端常量白名单与严格校验值，逐一经单引号转义后交 Start-Process；
// 提权窗口保持可见（-WindowStyle Normal），完成后 -Wait 同步返回且退出码如实上报。
func runElevatedProcess(ctx context.Context, file string, args ...string) (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", buildElevatedPS(file, args...))
	// 隐藏的是承载 Start-Process 的 powershell，不是提权后的目标窗口
	platformwin.HideConsole(cmd)
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
		// 目标程序退出码经 buildElevatedPS 传播到此：非零即失败，点名退出码，
		// 绝不再报"执行完毕"（窗口可见，红字详情在提权窗口里）。
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// %w 挂上类型化退出码：调用方（如 InstallDistroTo 的分段哨兵归因）
			// 用 errors.As 取码，不再从错误文案里抠数字。exec.ExitError 的
			// ExitCode() 依赖 ProcessState、单测伪造不了，故自带轻量类型。
			return OperationOutcome{}, fmt.Errorf("提权执行 %s 失败：命令以退出码 %d 结束（具体报错见提权窗口，窗口关闭过快时重跑一次留意红字）: %w",
				file, ee.ExitCode(), &elevatedExitError{code: ee.ExitCode()})
		}
		return OperationOutcome{}, fmt.Errorf("提权执行 %s 失败: %w %s", file, err, strings.TrimSpace(string(out)))
	}
	return OperationOutcome{Success: true, Message: "操作已执行完毕，状态已按最新结果刷新"}, nil
}

// elevatedExitError 提权链非零退出的类型化凭证：外层隐藏 powershell 的
// Start-Process -PassThru 把内层退出码传播到这里，供分段哨兵（如装完即迁链
// 的 install/move 两段归因）errors.As 判码。
type elevatedExitError struct{ code int }

func (e *elevatedExitError) Error() string {
	return fmt.Sprintf("提权命令以退出码 %d 结束", e.code)
}

// exitCode 提取错误链上的提权退出码；无类型凭证时返回 -1。
func exitCode(err error) int {
	var ee *elevatedExitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return -1
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
	platformwin.HideConsole(cmd)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
