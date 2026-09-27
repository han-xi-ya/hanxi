//go:build windows

package windows

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// 本文件沉淀"应用自身提权重启"原语，服务 requireAdministrator 托管模块
// （BCU/Rufus/LiteMonitor，TROUBLESHOOTING #17）：未提权的 Hanxi 用
// CreateProcess 拉起它们必遭 740 直拒且系统不代弹 UAC，唯一保住托管契约的
// 出路是让 Hanxi 自身升级为管理员——通道复用 portkill 已验证的
// powershell Start-Process -Verb RunAs 先例，不新造 ShellExecuteEx FFI；
// 脚本构造与取消判定再收敛到同包 RunElevatedDetached/IsUACCancelled 单件。

// IsElevated 报告当前进程令牌是否已提权（管理员完整性）。
// 查询失败按未提权处理：调用方只据此显示"以管理员身份重启"按钮，
// 误判方向的后果仅是多给一个无效入口，而 RPC 层会再次拒绝。
func IsElevated() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

// RestartElevated 经 UAC 以管理员身份拉起自身的新实例，args 原样透传。
//
// 同步语义：Start-Process -Verb RunAs 内部走 ShellExecuteEx，会阻塞到用户在
// UAC 对话框上做出决定为止——返回 nil 即代表新实例已成功创建（旧实例可以
// 放心走退出流程）；用户点"否"则返回取消错误，旧实例原地不动。
// powershell 进程用完即焚（不带 -Wait，不等新实例退出）。
//
// 实现收敛到同包 RunElevatedDetached（提权启动唯一脚本件）：旧版自建
// Start-Process 脚本破了本包两条规矩——①参数手拼双引号未过 PsQuote，
// 安装路径含 $ 或反引号时会被 PowerShell 插值/转义破坏；②只设 HideWindow
// 未叠加 CREATE_NO_WINDOW（不走 HideConsole），提权重启必闪 PowerShell
// 黑框，与"原生提权无感"定位冲突。委托后一并修复；空 args 不再渲染
// -ArgumentList（同审查 P0#2 教训），普通失败经 IsUACCancelled 严格判定
// 后如实带原始输出上抛（错误文案为"提权启动失败: …"）。
func RestartElevated(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位宿主程序路径: %w", err)
	}
	// 工作目录传空串省略 -WorkingDirectory，新实例沿用系统默认，与旧行为一致。
	res, err := RunElevatedDetached(exe, "", args)
	if err != nil {
		return err
	}
	if res.Cancelled {
		return errors.New("已在 UAC 提示中取消，未执行提权重启")
	}
	return nil
}

// WaitProcessGone 阻塞等待 pid 进程退出，超时返回 false。
// 打开进程失败（已退出/句柄权限不足）一律视为已消失返回 true——本函数用于
// 提权重启的交接窗口，等待对象是旧实例让出单实例锁，宁可放行不可空等。
func WaitProcessGone(pid uint32, timeout time.Duration) bool {
	if pid == 0 || pid == uint32(os.Getpid()) {
		return true
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return true
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
	return err == nil && event == windows.WAIT_OBJECT_0
}
