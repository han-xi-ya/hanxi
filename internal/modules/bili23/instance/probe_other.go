//go:build !windows

package instance

import "time"

type noopProbe struct{}

// NewBili23Probe 非 Windows 平台无命名互斥体概念（Hanxi 实际仅在 Windows 运行，
// 保持跨平台可编译）：恒不存活。
func NewBili23Probe() Bili23Probe { return &noopProbe{} }

func (p *noopProbe) IsRunning() bool                 { return false }
func (p *noopProbe) WaitForReady(time.Duration) bool { return false }
func (p *noopProbe) HasVisibleWindow(uint32) bool    { return false }
