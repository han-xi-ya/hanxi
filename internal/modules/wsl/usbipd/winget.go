package usbipd

// winget 代装面（N31 方案 B：真·一键安装 usbipd-win）。
//
// 纪律与 usbipd 命令面同源：包 ID 与全部参数都是后端固定字面量，本通道
// 零入参、前端无拼接面；winget 机器级安装自带 UAC（MSI 提权），这里不再
// 套 PowerShell RunAs——双重提权只会把授权语义搅浑（两道弹窗谁取消的说不清）。
// 探测/安装的非零退出、取消与失败一律归因成可读中文如实上报，绝不谎报成功
// （#37 退出码传播红线同族）。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	platformwin "hanxi/internal/platform/windows"
)

// WingetPackageID usbipd-win 在 winget 仓库的固定包 ID（白名单唯一值，
// 不接受任何外部输入替换）。
const WingetPackageID = "dorssel.usbipd-win"

// 代装结果状态（服务层据此分流回执文案）。
const (
	InstallDone      = "done"      // 本次 winget 安装成功收口
	InstallAlready   = "already"   // winget 显示已装（不重复执行安装）
	InstallCancelled = "cancelled" // UAC/安装授权被取消——绝不映射为成功
	InstallFailed    = "failed"    // winget 在跑但安装失败（Detail 带归因）
)

// ErrWingetMissing 系统找不到 winget（App Installer 缺席或不在 PATH）的哨兵：
// 属环境前提不满足，服务层转成指引文案而非"安装失败"。
var ErrWingetMissing = errors.New("winget 不可用")

// InstallResult winget 安装通道的一次执行小结。
type InstallResult struct {
	State  string
	Detail string // 失败的补充归因（winget 输出尾部，已中文化）
}

// WingetRunFunc winget.exe 调用面注入点（与 RunFunc 同构，单测替换后全离线）。
type WingetRunFunc func(ctx context.Context, args ...string) (stdout string, stderr string, err error)

// WingetInstaller usbipd-win 的 winget 安装胶水：前置探测 → 安装 → 归因收口。
type WingetInstaller struct {
	run WingetRunFunc
}

// NewWingetInstaller 以真实 exec 通道构造。
func NewWingetInstaller() *WingetInstaller { return &WingetInstaller{run: runWinget} }

// NewWingetInstallerWith 注入自定义调用通道（单测/替身用）。
func NewWingetInstallerWith(run WingetRunFunc) *WingetInstaller { return &WingetInstaller{run: run} }

// WingetInstallArgs 安装命令全字面量：钉死包 ID + 精确匹配 + 双协议接受 +
// 禁交互——隐藏窗口下 winget 绝不能停在任何"等待输入"上挂死。
func WingetInstallArgs() []string {
	return []string{
		"install", "--id", WingetPackageID, "-e",
		"--accept-source-agreements", "--accept-package-agreements",
		"--disable-interactivity",
	}
}

// WingetListArgs 存在性探测命令全字面量（命中判据见 wingetReportsInstalled）。
func WingetListArgs() []string {
	return []string{"list", "--id", WingetPackageID, "-e", "--accept-source-agreements"}
}

// runWinget 隐藏控制台执行 winget（用户态启动；机器级安装时 winget 自行请求 UAC，
// 授权窗走安全桌面，与隐藏控制台互不相干）。
func runWinget(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "winget", args...)
	platformwin.HideConsole(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return decodeMaybeUTF16(stdout.Bytes()), decodeMaybeUTF16(stderr.Bytes()), err
}

// Install 前置探测（winget list）→ 安装（winget install）。
// winget 本体缺席回 ErrWingetMissing；其余结局（已装/成功/取消/失败）都以
// InstallResult 如实返回，交由服务层复验与文案分流。
func (w *WingetInstaller) Install(ctx context.Context) (InstallResult, error) {
	stdout, stderr, err := w.run(ctx, WingetListArgs()...)
	switch {
	case err == nil && wingetReportsInstalled(stdout+"\n"+stderr):
		return InstallResult{State: InstallAlready}, nil
	case err != nil && notInstalled(err):
		return InstallResult{}, fmt.Errorf("%w: 探测时找不到 winget：%v", ErrWingetMissing, err)
	}
	// 探测非零退出多半是"没装"（winget 对未命中回专属错误码）；源故障之类的
	// 真实 winget 问题会在紧随其后的 install 里以同一面目暴露，这里不猜。
	out, errOut, ierr := w.run(ctx, WingetInstallArgs()...)
	combined := out + "\n" + errOut
	if ierr != nil && notInstalled(ierr) {
		return InstallResult{}, fmt.Errorf("%w: 安装时找不到 winget：%v", ErrWingetMissing, ierr)
	}
	code := exitCode(ierr)
	switch {
	case ierr == nil:
		return InstallResult{State: InstallDone}, nil
	case isCancelledExit(code, combined):
		return InstallResult{State: InstallCancelled}, nil
	default:
		return InstallResult{State: InstallFailed, Detail: wingetFailZh(combined, code)}, nil
	}
}

// wingetReportsInstalled list 命中判据：输出里原样出现被钉死的包 ID——
// 表格本地化换语言不换 ID；未命中的"找不到"话术不含 ID。
func wingetReportsInstalled(out string) bool {
	return strings.Contains(strings.ToLower(out), strings.ToLower(WingetPackageID))
}

// isCancelledExit 取消语义归一：ERROR_CANCELLED(1223) 与 HRESULT 0x800704C7
// 的无符号/有符号两种形态（Windows 退出码经 Go int 呈现不一），外加
// winget/MSI 常见取消文案（中英文环境）。
func isCancelledExit(code int, out string) bool {
	switch code {
	case 1223, -2147023673, 2147943623: // 1223 / 0x800704C7（有/无符号）
		return true
	}
	low := strings.ToLower(out)
	return strings.Contains(low, "0x800704c7") ||
		strings.Contains(low, "canceled by the user") ||
		strings.Contains(low, "user cancelled") ||
		strings.Contains(out, "已被用户取消") ||
		strings.Contains(out, "用户已取消")
}

// wingetFailZh winget 失败输出 → 中文可读归因；识别不出的照抄输出尾部，绝不吞信息。
func wingetFailZh(out string, code int) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "no packages were found matching"), strings.Contains(low, "no packages found"):
		return fmt.Sprintf("winget 源中找不到包 %s（软件源过旧或异常——先试「打开官方发布页」走 MSI）", WingetPackageID)
	case strings.Contains(low, "internet"), strings.Contains(low, "0x80072f"), strings.Contains(low, "0x80072ee"):
		return "winget 连不上软件源——需要联网（代理环境注意 winget 不走系统代理的情形）"
	}
	if tail := tailLines(out, 3); tail != "" {
		if code >= 0 {
			return fmt.Sprintf("%s（winget 退出码 %d）", tail, code)
		}
		return tail
	}
	if code >= 0 {
		return fmt.Sprintf("winget 以退出码 %d 失败（无输出可归因）", code)
	}
	return "winget 进程异常终止（无退出码与输出，可能被系统回收）"
}

// tailLines 输出尾部至多 n 个非空行（保序、单行化），供失败归因兜底展示。
func tailLines(out string, n int) string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if l := strings.TrimSpace(raw); l != "" {
			lines = append(lines, strings.ReplaceAll(l, "\n", " "))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}
