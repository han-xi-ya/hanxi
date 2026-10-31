//go:build !windows

// Package conpty 的非 Windows 桩：本仓产品目标是 Windows，但类型必须可跨平台
// 编译（CI Linux 的 go vet/test 全仓跑不能因构建标签炸包）。与 aiagent 的
// child_other.go 同姿势——平台差异只收在这对最小文件里，主流程不见构建标签。
// 注意：Linux 全仓 vet 能否走到这里受 wails beta.10 自身 cgo 依赖限制，
// 本包的跨平台由**按包** `GOOS=linux go vet ./packages/go/conpty` 保证。
package conpty

// Options 与 Windows 侧同形（见 conpty_windows.go）。
type Options struct {
	Cols, Rows uint16
	Dir        string
	Env        []string
}

// Pty 非 Windows 恒为不可用。
type Pty struct{}

// Spawn 直接拒绝。
func Spawn(string, []string, Options) (*Pty, error) { return nil, ErrUnsupportedPlatform }

// Read 桩。
func (p *Pty) Read(b []byte) (int, error) { return 0, ErrUnsupportedPlatform }

// Write 桩。
func (p *Pty) Write(b []byte) (int, error) { return 0, ErrUnsupportedPlatform }

// Resize 桩。
func (p *Pty) Resize(cols, rows uint16) error { return ErrUnsupportedPlatform }

// Pid 桩。
func (p *Pty) Pid() uint32 { return 0 }

// Done 返回永不关闭的通道（与"永远不会成功 spawn"的语义一致）。
func (p *Pty) Done() <-chan struct{} { return make(chan struct{}) }

// Err 桩。
func (p *Pty) Err() error { return nil }

// Close 桩。
func (p *Pty) Close() error { return nil }
