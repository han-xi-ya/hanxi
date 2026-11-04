//go:build windows

package omp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// launcher_windows.go：命令入口的 Windows 落地件——用户 PATH 注册表读写
// （HKCU\Environment，RRF_NOEXPAND 保原文回写，别把 %USERPROFILE% 烧成展开串）
// 与 WM_SETTINGCHANGE 广播（新窗口即拿新值，免注销）。官方 install.ps1 的
// [Environment]::SetEnvironmentVariable 同款效果，但由 hanxi 幂等维护单一入口目录。
//
// 装配经 newWindowsLauncher()（service 构造注入，airsnip postCmd 同款）；
// 单测以假 launcher 驱动，绝不写机主真实注册表。

var (
	advapi32                = syscall.NewLazyDLL("advapi32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procRegGetValueW        = advapi32.NewProc("RegGetValueW")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
)

const (
	// RRF_RT_REG_EXPAND_SZ | RRF_NOEXPAND：取未展开原文（只允许 EXPAND_SZ 类型）。
	rrfRtRegExpandSz = 0x1
	rrfNoExpand      = 0x40
	// WM_SETTINGCHANGE + HWND_BROADCAST：环境变量变更广播（shell 收到后刷新新进程环境）。
	wmSettingChange      = 0x001A
	hwndBroadcast        = 0xffff
	broadcastTimeoutMs   = 3000
	envChangeEnvironment = "Environment"
)

// readUserPathRaw 读取用户 PATH 原始值（不展开环境引用）；键/值不存在返回空串。
func readUserPathRaw() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && (errno == 2 || errno == 3) { // ERROR_FILE/KEY_NOT_FOUND
			return "", nil
		}
		return "", fmt.Errorf("打开 HKCU\\Environment 失败: %w", err)
	}
	defer k.Close()

	wide, err := syscall.UTF16PtrFromString(`Path`)
	if err != nil {
		return "", err
	}
	var sz uint32
	// 先探长度
	r1, _, _ := procRegGetValueW.Call(
		uintptr(syscall.HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(wide)),
		uintptr(rrfRtRegExpandSz|rrfNoExpand),
		0, 0, uintptr(unsafe.Pointer(&sz)), 0)
	if r1 == 2 { // ERROR_FILE_NOT_FOUND：无 Path 值
		return "", nil
	}
	if r1 != 0 {
		return "", fmt.Errorf("RegGetValueW 探长失败: %w", syscall.Errno(r1))
	}
	buf := make([]uint16, sz/2+1)
	r1, _, errno := procRegGetValueW.Call(
		uintptr(syscall.HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(wide)),
		uintptr(rrfRtRegExpandSz|rrfNoExpand),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&sz)), 0)
	if r1 != 0 {
		return "", fmt.Errorf("RegGetValueW 读取失败: %w (errno %v)", syscall.Errno(r1), errno)
	}
	return syscall.UTF16ToString(buf[:sz/2]), nil
}

// writeUserPathRaw 以 REG_EXPAND_SZ 原文回写（保持类型，与系统约定一致）。
func writeUserPathRaw(value string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 HKCU\\Environment 写句柄失败: %w", err)
	}
	defer k.Close()
	if err := k.SetExpandStringValue("Path", value); err != nil {
		return fmt.Errorf("写入用户 PATH 失败: %w", err)
	}
	return nil
}

// broadcastEnvChange 向所有顶层窗口广播 WM_SETTINGCHANGE("Environment")。
// 尽力而为：广播失败不改写结果（新终端重启后照样生效，仅即时性受损）。
func broadcastEnvChange() {
	param, err := syscall.UTF16PtrFromString(envChangeEnvironment)
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() { // HWND_BROADCAST 同步派发可能等慢窗口，限时等一次不阻塞 RPC
		procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0,
			uintptr(unsafe.Pointer(param)), 0x0002 /*SMTO_ABORTIFHUNG*/, broadcastTimeoutMs, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// linkOrCopy 优先硬链接（同卷零拷贝零空间），失败回退复制。
func linkOrCopy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("替换入口文件失败: %w", err)
		}
	}
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return nil
}

// splitPathValue 注册表串 → 片段列表（保空段交纯函数清理）。
func splitPathValue(v string) []string { return strings.Split(v, ";") }

// joinPathValue 片段列表 → 注册表串。
func joinPathValue(parts []string) string { return strings.Join(parts, ";") }

// newWindowsLauncher 组装真实 Windows 入口装配器（注册表 + 广播 + 硬链接）。
func newWindowsLauncher() *launcher {
	return &launcher{
		readPath:  readUserPathRaw,
		writePath: writeUserPathRaw,
		broadcast: broadcastEnvChange,
		link:      linkOrCopy,
	}
}
