//go:build !windows

package instance

import "time"

type noopProbe struct{}

// NewRufusProbe 非 Windows 平台占位实现（Rufus 是 Win32 独占工具，
// Hanxi 实际仅在 Windows 运行，此分支只保证跨平台编译通过）。
func NewRufusProbe() RufusProbe { return noopProbe{} }

func (noopProbe) FindPIDs() []uint32              { return nil }
func (noopProbe) IsRunning() bool                 { return false }
func (noopProbe) WaitForReady(time.Duration) bool { return false }
func (noopProbe) IsMainWindowOpen([]uint32) bool  { return false }
