//go:build windows

package settings

import (
	"log/slog"
	"os"
	"syscall"
	"unsafe"
)

var (
	modUser32       = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = modUser32.NewProc("MessageBoxW")
)

// mbIconError MB_ICONERROR | MB_OK。
const mbIconError = 0x00000010

// ExitWithBindingGuide 数据根不可用时的 fail loud 出口：原生错误弹窗 + 非零退出。
// 双击启动（windows 子系统）场景 stderr 不可见，弹窗是唯一能让用户读到
// guideSuffix 处置指引（搬家 / 写 hanxi.bind 绑定）的通道；指引用尽即退出，
// 绝无静默回退用户目录一说（F6 裁定②，%APPDATA% 兜底已死）。
// 日志文件此时尚未初始化（InitLogger 在装配更后置），slog 回落 stderr 留痕。
func ExitWithBindingGuide(err error) {
	if err == nil {
		return
	}
	slog.Error("fatal: hanxi data root unavailable, exiting with guide", "err", err)
	text, _ := syscall.UTF16PtrFromString("Hanxi 无法准备可用的数据目录。\n\n" + err.Error())
	title, _ := syscall.UTF16PtrFromString("Hanxi 启动失败 · 数据目录不可用")
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError)
	os.Exit(1)
}
