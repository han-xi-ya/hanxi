//go:build !windows

package instance

import "time"

type nullQuickLookProbe struct{}

// NewQuickLookProbe 非 Windows 平台占位实现（Hanxi 实际仅在 Windows 运行）：
// 恒报告未运行，所有生命周期操作退化为纯进程语义。
func NewQuickLookProbe() QuickLookProbe { return &nullQuickLookProbe{} }

func (p *nullQuickLookProbe) IsRunning() bool                 { return false }
func (p *nullQuickLookProbe) WaitForReady(time.Duration) bool { return false }
