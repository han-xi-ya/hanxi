//go:build windows

package ompofficial

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// official_windows.go：官方安装通道的 Windows 原语落地——用户 PATH 注册表读写
// （HKCU\Environment，RRF_NOEXPAND 保原文回写，不把 %USERPROFILE% 烧成展开串）、
// WM_SETTINGCHANGE 广播、官方 install.ps1 执行、装件版本实探。单测走接缝假件。

var (
	advapi32                = syscall.NewLazyDLL("advapi32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procRegGetValueW        = advapi32.NewProc("RegGetValueW")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
)

const (
	rrfRtRegExpandSz     = 0x1 // RRF_RT_REG_EXPAND_SZ
	rrfNoExpand          = 0x4
	wmSettingChange      = 0x001A
	hwndBroadcast        = 0xffff
	smtoAbortIfHung      = 0x2
	broadcastWaitMs      = 3000
	officialRunTimeout   = 20 * time.Minute // 244MB 下载预算（官方脚本自带校验）
	officialProbeTimeout = 15 * time.Second // omp --version 秒回；富余防首跑解压慢
)

// readUserPathRaw 读取用户 PATH 原始值（不展开环境引用）；键/值不存在返回空串。
func readUserPathRaw() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && (errno == 2 || errno == 3) {
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

// writeUserPathRaw 以 REG_EXPAND_SZ 原文回写（保持类型与系统约定一致）。
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

// broadcastEnvChange 广播 WM_SETTINGCHANGE("Environment")；尽力而为，
// 慢窗口阻塞时限时放弃（新终端重启后照样生效，仅即时性受损）。
func broadcastEnvChange() {
	param, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0,
			uintptr(unsafe.Pointer(param)), smtoAbortIfHung, broadcastWaitMs, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// runOfficialScriptImpl 执行官方安装/更新脚本（等价机主手敲
// `irm https://omp.sh/install.ps1 | iex` 的 -Binary 形态；无窗创建防黑框扫桌面）。
func runOfficialScriptImpl(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, officialRunTimeout)
	defer cancel()
	// 前置 TLS 协商强化：PS 5.1 的 irm 默认跟随系统旧协商栈，omp.sh→raw 的
	// 302 链路对 TLS1.0/1.1 已拒服——显式钉 Tls12/13 消除一类 reset（2026-11-04 实测）。
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", "[Net.ServicePointManager]::SecurityProtocol='Tls12,Tls13'; "+officialScriptCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	text := tailOutput(string(out), 600)
	if ctx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("官方脚本超时（%v），输出尾段：%s", officialRunTimeout, text)
	}
	if err != nil {
		return text, fmt.Errorf("官方脚本执行失败: %w，输出尾段：%s", err, text)
	}
	return text, nil
}

// probeVersionImpl 实跑 `<exe> --version` 取真实版本（"omp/18.16.0" → 原样尾段）。
// 失败返回空串（GetStatus 如实展示"版本未知"，不谎报）。
func probeVersionImpl(exe string) string {
	ctx, cancel := context.WithTimeout(context.Background(), officialProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// tailOutput 保留输出尾段至多 n 字节（脚本头部噪音多，结论在尾部）。
func tailOutput(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// New 组装真实 Windows 官方安装通道编排器。
func New() *Manager {
	return &Manager{
		dir:       officialDir(),
		readPath:  readUserPathRaw,
		writePath: writeUserPathRaw,
		broadcast: broadcastEnvChange,
		runScript: runOfficialScriptImpl,
		probeVer:  probeVersionImpl,
	}
}
