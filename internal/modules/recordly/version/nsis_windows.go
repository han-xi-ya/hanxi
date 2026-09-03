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

	cmd := exec.Command(installer)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + installer + `" /S /D=` + targetDir,
	}
	cmd.Dir = filepath.Dir(installer)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("NSIS 安装器退出码 %d（安装中止？请确认没有正在运行的 Recordly 实例）", ee.ExitCode())
		}
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("NSIS 静默安装超时（15 分钟）")
		}
		return err
	}
	return nil
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
