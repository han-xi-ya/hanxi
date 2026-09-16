//go:build !windows

package instance

import "time"

type noopProbe struct{}

// NewPaseoProbe 非 Windows 平台无进程快照/窗口枚举概念
// （Hanxi 实际仅在 Windows 运行，保持跨平台可编译）：恒不存活。
func NewPaseoProbe() PaseoProbe { return &noopProbe{} }

// 以下占位方法实现 Probe 接口：恒报告"不存在/未就绪/失败"，仅保跨平台编译，语义见 prober.go 接口注释。
func (p *noopProbe) IsRunning() bool                 { return false }
func (p *noopProbe) WaitForReady(time.Duration) bool { return false }
func (p *noopProbe) IsWindowOpen() bool              { return false }
func (p *noopProbe) FocusWindow() bool               { return false }
