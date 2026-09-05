//go:build windows

package version

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

// runInstallerSilent 以 electron-builder NSIS 语义静默安装：
//
//	instaler.exe /S /D=<targetDir>
//
// 两个 NSIS 硬约束（与"普通程序传参"直觉相反，改动前先读注释）：
//  1. /D= 必须是命令行最后一个参数、路径不得带引号——Go 默认 argv 拼接会给含
//     空格的路径整体加引号，NSIS 取 "/D= 之后的整段" 会把尾引号算进目录名；
//     因此显式给 SysProcAttr.CmdLine 组原始命令行绕开引号化。
//  2. oneClick 安装器启动时按 HKCU 卸载注册表指向的 InstallLocation 静默卸载
//     旧实例——若注册表指向的是用户自己装的 Recordly（不在 Hanxi 托管目录下），
//     会连带删掉用户安装（%APPDATA%\Recordly 配置与录像不受影响，但程序目录消失）。
//     故安装前必须经 foreignInstallLocation 卫兵拦截。
func runInstallerSilent(installer, targetDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// CommandContext 而非裸 Command：超时才真正生效（杀进程）。此前 ctx 无人
	// watch，装死在隐藏对话框里的安装器会永久吊死下载 goroutine，
	// "超时（15 分钟）"文案是不可达的死代码。
	cmd := exec.CommandContext(ctx, installer)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + installer + `" /S /D=` + targetDir,
	}
	cmd.Dir = filepath.Dir(installer)
	if err := cmd.Run(); err != nil {
		// ctx 判定必须在 ExitError 之前：超时被杀后 Run 同样返回 *ExitError，
		// 顺序颠倒会把超时误报成"异常退出码"。
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("NSIS 静默安装超时（15 分钟）：安装器可能卡在隐藏对话框，已强制终止，重试安装即可（NSIS 覆盖重装托管目录）")
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code := ee.ExitCode()
			return fmt.Errorf("NSIS 安装器退出码 %d：%s", code, decodeInstallerExit(code))
		}
		return err
	}
	return nil
}

// decodeInstallerExit 为 NSIS 异常退出码给出面向用户的语义分类。
// NSIS 契约：0 = 成功；非零码分两族，处置指引完全不同，不可合并成一句猜测——
//   - 小的正数是 Win32 错误码（1223=用户取消 UAC、5=拒绝访问等）；
//   - 高位为 1 的大数（≥0xC0000000）是 NTSTATUS 异常：其中 3221225477
//     （0xC0000005 访问违例）= 安装器进程崩溃，与"文件占用导致安装中止"
//     的典型猜测无关：Recordly 安装器未签名（instance 包注释实证），真实
//     常见诱因是杀软注入扫描干扰。包完整性此时已由 sha256 四层校验背书，
//     可直接排除"文件坏了"。
func decodeInstallerExit(code int) string {
	switch code {
	case 1223: // ERROR_CANCELLED：UAC 提示点了"否"
		return "提权请求被取消：重试安装并在 UAC 弹窗中点击\"是\""
	case 5: // ERROR_ACCESS_DENIED
		return "拒绝访问：目标目录或注册表键被权限/安全策略拦截，请检查安全软件与目录权限后重试"
	case 0xC0000005:
		return "安装器进程崩溃（0xC0000005 访问违例），并非文件占用：安装包已通过 sha256 校验，而 Recordly 安装器未数字签名，最典型诱因是杀毒软件注入扫描干扰——暂时关闭实时防护或添加信任后重试；托管目录可能已写入一半，直接重新安装即可（NSIS 覆盖重装）"
	}
	if code >= 0xC0000000 {
		return fmt.Sprintf("安装器进程被系统异常终止（NTSTATUS 0x%08X），属崩溃而非安装逻辑失败：先重试一次，持续复现请排查安全软件注入", uint32(code))
	}
	return "安装中止（NSIS 非零返回）：最常见是正在运行的 Recordly 实例锁住目标文件，请先退出全部 Recordly 再重试"
}

// foreignInstallLocation 扫描 HKCU 卸载注册表，返回非托管位置的 Recordly 安装目录。
// 命中条件：DisplayName 为 Recordly 且 InstallLocation 真实存在且不在 Hanxi 托管目录内。
// 读失败一律按"无外部安装"放行（卫兵只拦确定证据，注册表异常不阻断正常下载流）。
func foreignInstallLocation(versionsDir string) (string, bool) {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Uninstall`, registry.READ)
	if err != nil {
		return "", false
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return "", false
	}
	for _, name := range names {
		sub, err := registry.OpenKey(key, name, registry.READ)
		if err != nil {
			continue
		}
		display, _, dnErr := sub.GetStringValue("DisplayName")
		loc, _, lcErr := sub.GetStringValue("InstallLocation")
		sub.Close()
		if dnErr != nil || !strings.EqualFold(strings.TrimSpace(display), "Recordly") {
			continue
		}
		loc = strings.TrimSpace(strings.Trim(loc, `"`))
		if loc == "" || lcErr != nil {
			continue
		}
		if fi, statErr := os.Stat(loc); statErr != nil || !fi.IsDir() {
			continue // 注册表残留指向已消失目录：无害陈旧项
		}
		if isUnderDir(loc, versionsDir) {
			continue // Hanxi 自己的托管目录（NSIS 正常覆盖升级路径）
		}
		return loc, true
	}
	return "", false
}

// cleanupShortcuts 静默安装会顺手创建桌面/开始菜单快捷方式（指向托管目录、
// 可绕过 Hanxi 启停）。托管模式装完后删掉，"独立运行"语义由 Hanxi 启动开关控制。
// 全部 best-effort：OneDrive 重定向桌面等拿不到的路径静默跳过。
func cleanupShortcuts(versionsDir, desktopDir string) {
	var desktops []string
	if strings.TrimSpace(desktopDir) != "" {
		desktops = append(desktops, desktopDir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		desktops = append(desktops, filepath.Join(home, "Desktop"))
	}
	if pub := os.Getenv("PUBLIC"); pub != "" {
		desktops = append(desktops, filepath.Join(pub, "Desktop"))
	}
	for _, d := range desktops {
		_ = os.Remove(filepath.Join(d, "Recordly.lnk"))
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		startMenu := filepath.Join(appdata, `Microsoft\Windows\Start Menu\Programs`)
		// electron-builder 按 productName 建目录；个别版本平铺命名 lnk——两种形态都清
		_ = os.RemoveAll(filepath.Join(startMenu, "Recordly"))
		_ = os.Remove(filepath.Join(startMenu, "Recordly.lnk"))
	}
}
