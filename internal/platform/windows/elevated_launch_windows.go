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
	// Start-Process 自身同步等待 UAC 决策；不加 -Wait，目标启动后立即返回。
	// 脚本构造收敛到 elevatedStartScript 单件（RestartElevated 复用本函数后
	// 全仓提权启动只有这一种脚本形态）。
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		elevatedStartScript(exe, workingDir, args))
	HideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return ElevatedRunResult{Started: true}, nil
	}
	if IsUACCancelled(string(out)) {
		return ElevatedRunResult{Cancelled: true}, nil
	}
	return ElevatedRunResult{}, fmt.Errorf("提权启动失败: %w %s", err, strings.TrimSpace(string(out)))
}

// elevatedStartScript 构造 Start-Process 提权脚本（包内唯一脚本件，纯函数供测试断言拼接）。
//
// 审查 P0#2：空 args 必须**整段省略** -ArgumentList——PowerShell 对
// `-ArgumentList  -Verb` 形态报 Missing an argument（实测 UAC 根本不弹，
// RAMMap A 路必挂）；参数段与工作目录段同法条件拼接。
// 全部字面量过 PsQuote 单引号形态：双引号会让 PowerShell 插值含 $ /反引号的
// 路径（旧 RestartElevated 自建脚本手拼引号踩过的教训，勿再回退）。
func elevatedStartScript(exe, workingDir string, args []string) string {
	argPart := ""
	if len(args) > 0 {
		quoted := make([]string, 0, len(args))
		for _, a := range args {
			quoted = append(quoted, PsQuote(a))
		}
		argPart = " -ArgumentList " + strings.Join(quoted, ", ")
	}
	work := ""
	if workingDir != "" {
		work = " -WorkingDirectory " + PsQuote(workingDir)
	}
	return fmt.Sprintf("Start-Process -FilePath %s%s -Verb RunAs%s", PsQuote(exe), argPart, work)
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

// IsUACCancelled 判定提权 PowerShell 命令的输出是否为"用户在 UAC 上点了否"。
// 全仓唯一取消判定实现（审查 #7 后导出，portkill 等业务包一律委托，不得再
// 自建宽松式匹配）：只锚定强特征——裸 "1223"/泛 "已取消" 会把任何含该字样的
// 普通失败（尺寸、PID、超时提示）误判成用户取消并吞掉错误。
func IsUACCancelled(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "canceled by the user") ||
		strings.Contains(lower, "user cancelled") ||
		strings.Contains(lower, "0x800704c7") ||
		strings.Contains(out, "已被用户取消")
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
