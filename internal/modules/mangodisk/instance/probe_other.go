//go:build !windows

package instance

import "time"

// noopProbe 非 Windows 平台的探测占位：全部返回"不存在"，保证代码可编译（Hanxi 实际仅跑 Windows）。
type noopProbe struct{}

// NewMangoDiskProbe 按构建平台返回探测实现。
func NewMangoDiskProbe() MangoDiskProbe { return &noopProbe{} }

// 以下占位方法实现 Probe 接口：恒报告"不存在/未就绪/失败"，仅保跨平台编译，语义见 prober.go 接口注释。
func (p *noopProbe) IsRunning() bool                 { return false }
func (p *noopProbe) WaitForReady(time.Duration) bool { return false }
func (p *noopProbe) SignalPID() (uint32, bool)       { return 0, false }
