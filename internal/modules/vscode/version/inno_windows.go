//go:build windows

package version

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// runInstallerSilent 以 Inno Setup（User Installer）语义静默安装：
//
//	VSCodeUserSetup-x64-<ver>.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
//	    /MERGETASKS="!runcode,!addtopath,!associatewithfiles,!desktopicon,..."
//
// 契约要点（上游 build/win32/code.iss 实证）：
//  1. Inno 用 CommandLineToArgvW 风格解析，参数可安全走 Go argv 拼接（无 NSIS
//     /D= 那种"最后参数不得带引号"怪癖——recordly 先例的 CmdLine 手工组装在此不需要）；
//  2. 不带 /UPDATE 参数：{param:update} 是 VS Code 应用内后台更新专用通道
//     （跳过文件类任务），托管的是完整安装；
//  3. 任务白名单取反：addtopath / associatewithfiles 在 [Tasks] 中默认勾选，
//     静默装会静默改用户 PATH 与文件关联——托管不越权，全部 ! 掉；
//     runcode 在静默态恒勾选（Check: WizardSilent），不 ! 掉装完会直接弹出 VS Code；
//  4. 不带 /DIR：安装位置跟随本机既有安装（Inno 升级语义），用户自定义目录不被改写；
//  5. AppMutex=vscode + CloseApplications=force：运行中的 VS Code（包括用户
//     日常实例）会被安装器强杀——service 层 InstallVersion 必须先向用户要确认。
func runInstallerSilent(installer, version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, installer,
		"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-",
		"/MERGETASKS=!runcode,!addtopath,!associatewithfiles,!addcontextmenufiles,!addcontextmenufolders,!desktopicon",
	)
	cmd.Dir = filepath.Dir(installer)
	if err := cmd.Run(); err != nil {
		// ctx 判定必须在 ExitError 之前：超时被杀后 Run 同样返回 *ExitError，
		// 顺序颠倒会把超时误报成"异常退出码"（recordly nsis_windows.go 同款教训）。
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("Inno 静默安装超时（20 分钟）：安装器可能卡在隐藏对话框，已强制终止，重试安装即可（Inno 覆盖重装）")
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code := ee.ExitCode()
			if code == 0 {
				return nil // 理论上不可达：Run 成功不返回错误
			}
			return fmt.Errorf("VS Code %s 安装器退出码 %d：%s", version, code, decodeInstallerExit(code))
		}
		return err
	}
	return nil
}

// decodeInstallerExit 为 Inno Setup 异常退出码给出面向用户的语义分类。
// Inno 官方退出码契约（Appendix E）：0 成功；1 用户取消；2 内部错误；
// 3 Windows API 调用失败；4 向导页显示前的初始化函数返回 False——对 VS Code
// 即 InitializeSetup 的架构冲突/互斥体拦截；≥0xC0000000 族是 NTSTATUS 进程异常。
func decodeInstallerExit(code int) string {
	switch code {
	case 1:
		return "安装被取消：静默安装理论上无取消入口，多为安全软件弹窗拦截后选择了拒绝"
	case 2:
		return "安装器内部错误（Inno Setup bug 级失败）：重试一次，持续复现请改用便携版托管"
	case 3:
		return "系统调用失败：最常见诱因是运行中的 VS Code 锁住了目标文件（安装器已尽力强关，仍可能被杀软注入拦下），退出全部 VS Code 实例后重试"
	case 4:
		return "安装初始化被拒：本机可能已存在另一架构（x64/ARM64）的 VS Code 安装，Inno 拒绝架构混装；也可能是安装器互斥体检测失败，请到「设置→应用」卸载既有的不匹配版本后重试"
	case 0xC0000005:
		return "安装器进程崩溃（0xC0000005 访问违例），并非文件占用：安装包完整性已校验，最典型诱因是杀毒软件注入扫描干扰——暂时关闭实时防护或添加信任后重试"
	}
	if code >= 0xC0000000 {
		return fmt.Sprintf("安装器进程被系统异常终止（NTSTATUS 0x%08X），属崩溃而非安装逻辑失败：先重试一次，持续复现请排查安全软件注入", uint32(code))
	}
	return fmt.Sprintf("安装中止（Inno 非零返回 %d）：先重试一次；若本机正在运行 VS Code，请先退出全部实例（含未保存工作的窗口）再试", code)
}
