//go:build !windows

package instance

import "time"

// noopEverythingProbe 非 Windows 平台桩实现——一切家校相关信号仅存在于 Windows。
// 保持跨平台可编译（与 markeron probe_other.go 同目的）。
type noopEverythingProbe struct{}

func NewEverythingProbe() EverythingProbe { return &noopEverythingProbe{} }

// 以下占位方法实现 Probe 接口：恒报告"不存在/未就绪/失败"，仅保跨平台编译，语义见 prober.go 接口注释。
func (p *noopEverythingProbe) IsEverythingRunning() bool { return false }

func (p *noopEverythingProbe) WaitForEverythingReady(timeout time.Duration) bool { return false }

func (p *noopEverythingProbe) IsSearchWindowOpen() bool { return false }
