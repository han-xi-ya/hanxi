//go:build !windows

package instance

import "time"

type nullViewProbe struct{}

// NewViewProbe 非 Windows 平台占位实现（Hanxi 实际仅在 Windows 运行）：
// 恒报告未运行，所有生命周期操作退化为纯进程语义。
func NewViewProbe() ViewProbe { return &nullViewProbe{} }

func (p *nullViewProbe) IsRunning() bool                 { return false }
func (p *nullViewProbe) WaitForReady(time.Duration) bool { return false }
func (p *nullViewProbe) FocusMainWindow(uint32) bool     { return false }
func (p *nullViewProbe) FocusAnyWindow() bool            { return false }
