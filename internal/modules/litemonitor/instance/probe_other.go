//go:build !windows

package instance

import "time"

type noopProbe struct{}

// NewLiteMonitorProbe 非 Windows 平台无进程枚举概念（Hanxi 实际仅在 Windows 运行，
// 保持跨平台可编译）：恒不存活。
func NewLiteMonitorProbe() LiteMonitorProbe { return &noopProbe{} }

// 以下占位方法实现 Probe 接口：恒报告"不存在/未就绪/失败"，仅保跨平台编译，语义见 prober.go 接口注释。
func (p *noopProbe) FindPIDs() []uint32              { return nil }
func (p *noopProbe) IsRunning() bool                 { return false }
func (p *noopProbe) WaitForReady(time.Duration) bool { return false }
func (p *noopProbe) IsMainWindowOpen([]uint32) bool  { return false }
