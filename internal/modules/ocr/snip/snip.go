// Package snip 提供「框选截屏识别」所需的 Windows 原语：唤起系统截屏
// （ms-screenclip: 协议）、剪贴板读写（文本快照/清空/取图/写文本）、游标查询，
// 以及平台无关的 DIB→PNG 解码与轮询状态机（可单测）。
//
// 架构口径与 quickmenu/mousetrap 同构：模块内平台原语自成子包，
// 不挤进 internal/platform 全局契约；非 Windows 编译通过但一律中文报错。
package snip

import "time"

// Clip 剪贴板操作集合（接口化以便服务层单测打桩）。
type Clip interface {
	// SnapshotText 读取当前 CF_UNICODETEXT（无文本返回 ok=false）。
	SnapshotText() (text string, ok bool)
	// Empty 清空剪贴板（原生截屏以剪贴板交付，先清空才能无歧义等待新图）。
	Empty() error
	// WriteText 以 UTF-16 文本占住剪贴板（取消时回写快照 / 识别后自动复制）。
	WriteText(text string) error
	// GrabImage 尝试取剪贴板图像并转 PNG 字节；found=false 表示当前无图像。
	GrabImage() (png []byte, found bool, err error)
}

// Snipper 汇总截屏流程依赖的系统能力。
type Snipper interface {
	Clip
	// InvokeOverlay 拉起系统截屏覆盖层（异步返回，选区由用户完成）。
	InvokeOverlay() error
	// CursorPos 当前游标物理像素坐标（悬浮卡定位用）。
	CursorPos() (x, y int, err error)
}

// WaitFor 轮询剪贴板直至出现图像（截屏工具完成写剪贴板的瞬间命中）。
// 返回 found=false 且 err=nil 表示超时——语义等同用户取消选区。
func WaitFor(c Clip, timeout time.Duration) ([]byte, bool, error) {
	return waitFor(c.GrabImage, time.Now().Add(timeout), 200*time.Millisecond, time.Now, time.Sleep)
}

// waitFor 通用轮询状态机：每 tick 调一次 grab，命中（found=true）即返回；
// 到 deadline 返回未命中。注入 sleep/now 便于单测免真实等待。
func waitFor(grab func() ([]byte, bool, error), deadline time.Time, tick time.Duration, now func() time.Time, sleep func(time.Duration)) ([]byte, bool, error) {
	for {
		png, found, err := grab()
		if err != nil || found {
			return png, found, err
		}
		if !now().Before(deadline) {
			return nil, false, nil
		}
		if left := deadline.Sub(now()); left < tick {
			sleep(left)
		} else {
			sleep(tick)
		}
	}
}
