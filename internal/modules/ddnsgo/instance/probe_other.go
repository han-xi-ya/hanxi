//go:build !windows

package instance

// otherProbe 非 Windows 平台：进程名扫描未实现（本模块目标平台为 Windows，
// 保留交叉编译能力即可），IsRunning 恒 false——外部感知在非 Windows 上不生效。
type otherProbe struct {
	netPortProbe
}

// NewProbe 非 Windows 桩实现。
func NewProbe() Probe { return &otherProbe{} }

func (p *otherProbe) FindPIDs() []uint32 { return nil }
func (p *otherProbe) IsRunning() bool    { return false }
