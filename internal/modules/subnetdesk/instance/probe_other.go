//go:build !windows

package instance

type noopProbe struct{}

// NewSubnetDeskProbe 非 Windows 平台无进程快照/提取目录概念（Hanxi 实际仅在
// Windows 运行，保持跨平台可编译）：恒不存活。
func NewSubnetDeskProbe() SubnetDeskProbe { return &noopProbe{} }

func (p *noopProbe) FindInstancePIDs() []uint32     { return nil }
func (p *noopProbe) FindOwnPIDs([]uint32) []uint32  { return nil }
func (p *noopProbe) IsRunning() bool                { return false }
func (p *noopProbe) HasVisibleWindow([]uint32) bool { return false }
func (p *noopProbe) FocusWindows([]uint32) int      { return 0 }
