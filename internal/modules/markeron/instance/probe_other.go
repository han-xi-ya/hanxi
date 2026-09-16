//go:build !windows

package instance

import "time"

// noopMarkerProbe 非 Windows 平台桩实现——MarkerOn 单实例互斥体仅存在于 Windows。
// 保持跨平台可编译（与 frpc child_other.go 同目的）。
type noopMarkerProbe struct{}

func NewMarkerProbe() MarkerProbe { return &noopMarkerProbe{} }

// 以下占位方法实现 Probe 接口：恒报告"不存在/未就绪/失败"，仅保跨平台编译，语义见 prober.go 接口注释。
func (p *noopMarkerProbe) IsMarkerOnRunning() bool { return false }

func (p *noopMarkerProbe) WaitForMarkerOnReady(timeout time.Duration) bool { return false }
