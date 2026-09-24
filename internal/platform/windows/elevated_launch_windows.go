//go:build windows

package windows

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ElevatedRunResult 表示一次 UAC 提权启动的外层结果。
// Started 仅代表目标进程已被 Windows 接受创建，不代表目标 GUI 持续运行；
// A 路调用方不得据此宣称目标已进入 Hanxi JobObject。
type ElevatedRunResult struct {
	Started   bool
	Cancelled bool
}

// RunElevatedDetached 以 UAC runas 启动一个固定目标，不等待目标退出。
//
// 目标路径与参数必须由后端构造，调用方不得传 shell 字符串；PowerShell 只作为
// ShellExecuteEx 的兼容实现，所有字面量经过 PsQuote。该函数只报告 UAC/启动
// 阶段，不取得目标 PID、不接入 Job、不承诺后续退出治理（适用于 N22 A 路）。
func RunElevatedDetached(exe, workingDir string, args []string) (ElevatedRunResult, error) {
	if err := validateElevatedTarget(exe); err != nil {
		return ElevatedRunResult{}, err
	}
	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, PsQuote(arg))
	}
	argList := strings.Join(quotedArgs, ", ")
	work := ""
	if workingDir != "" {
		work = " -WorkingDirectory " + PsQuote(workingDir)
	}
	// Start-Process 自身同步等待 UAC 决策；不加 -Wait，目标启动后立即返回。
	script := fmt.Sprintf("Start-Process -FilePath %s -ArgumentList %s -Verb RunAs%s", PsQuote(exe), argList, work)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	HideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return ElevatedRunResult{Started: true}, nil
	}
	if isUACCancelled(string(out)) {
		return ElevatedRunResult{Cancelled: true}, nil
	}
	return ElevatedRunResult{}, fmt.Errorf("提权启动失败: %w %s", err, strings.TrimSpace(string(out)))
}

func validateElevatedTarget(exe string) error {
	if strings.TrimSpace(exe) == "" {
		return errors.New("提权目标路径不能为空")
	}
	st, err := os.Stat(exe)
	if err != nil {
		return fmt.Errorf("提权目标不存在: %w", err)
	}
	if st.IsDir() {
		return errors.New("提权目标不能是目录")
	}
	return nil
}

func isUACCancelled(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "canceled by the user") ||
		strings.Contains(lower, "user cancelled") ||
		strings.Contains(lower, "0x800704c7") ||
		strings.Contains(lower, "1223") || strings.Contains(out, "已被用户取消") ||
		strings.Contains(out, "已取消")
}

// ElevatedRunExitCode 从等待型 PowerShell helper 的 exec 错误中取退出码。
// 共享给 N23 一次性 helper，避免业务包复制 errors.As 样板。
func ElevatedRunExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
