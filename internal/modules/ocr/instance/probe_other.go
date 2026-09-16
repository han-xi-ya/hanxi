//go:build !windows

package instance

// otherProbe 非 Windows 平台：进程名扫描未实现（hanxi-ocr 私发件本身仅支持
// Windows x64），保留交叉编译能力；契约探测与端口拨测仍可用。
type otherProbe struct {
	netPortProbe
}

// NewProbe 非 Windows 桩实现。
func NewProbe() Probe { return &otherProbe{} }

func (p *otherProbe) FindPIDs() []uint32 { return nil }
func (p *otherProbe) IsRunning() bool    { return false }
