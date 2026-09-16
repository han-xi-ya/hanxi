//go:build !windows

package snip

import "fmt"

type unsupported struct{}

// New 返回当前平台的截屏/剪贴板原语实现（非 Windows 全部中文报错）。
func New() Snipper { return unsupported{} }

func (unsupported) InvokeOverlay() error {
	return fmt.Errorf("框选截屏识别仅支持 Windows")
}

func (unsupported) SnapshotText() (string, bool) { return "", false }
func (unsupported) Empty() error                 { return fmt.Errorf("框选截屏识别仅支持 Windows") }
func (unsupported) WriteText(string) error       { return fmt.Errorf("框选截屏识别仅支持 Windows") }
func (unsupported) GrabImage() ([]byte, bool, error) {
	return nil, false, fmt.Errorf("框选截屏识别仅支持 Windows")
}
func (unsupported) CursorPos() (int, int, error) {
	return 0, 0, fmt.Errorf("框选截屏识别仅支持 Windows")
}

