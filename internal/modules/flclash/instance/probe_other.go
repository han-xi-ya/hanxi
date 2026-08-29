//go:build !windows

package instance

import "time"

type noopProbe struct{}

// NewFlClashProbe 非 Windows 平台无进程枚举概念（Hanxi 实际仅在 Windows 运行，
// 保持跨平台可编译）：恒不存活。
func NewFlClashProbe() FlClashProbe { return &noopProbe{} }

func (p *noopProbe) FindPIDs() []uint32              { return nil }
func (p *noopProbe) IsRunning() bool                 { return false }
func (p *noopProbe) WaitForReady(time.Duration) bool { return false }
func (p *noopProbe) IsMainWindowOpen([]uint32) bool  { return false }
