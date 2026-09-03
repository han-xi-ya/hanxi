//go:build !windows

package instance

import "time"

type noopProbe struct{}

// NewPaperProbe 非 Windows 平台无单实例互斥体概念（Hanxi 实际仅在 Windows 运行，
// 保持跨平台可编译）：恒不存活。
func NewPaperProbe() PaperProbe { return &noopProbe{} }

func (p *noopProbe) IsRunning() bool                 { return false }
func (p *noopProbe) WaitForReady(time.Duration) bool { return false }
