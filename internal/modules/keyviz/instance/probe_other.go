//go:build !windows

package instance

import "time"

type nullKeyvizProbe struct{}

// NewKeyvizProbe 非 Windows 平台占位实现（Hanxi 实际仅在 Windows 运行）：
// 恒报告未运行，所有生命周期操作退化为纯进程语义。
func NewKeyvizProbe() KeyvizProbe { return &nullKeyvizProbe{} }

// 以下占位方法实现 Probe 接口：恒报告"不存在/未就绪/失败"，仅保跨平台编译，语义见 prober.go 接口注释。
func (p *nullKeyvizProbe) IsRunning() bool                 { return false }
func (p *nullKeyvizProbe) WaitForReady(time.Duration) bool { return false }
