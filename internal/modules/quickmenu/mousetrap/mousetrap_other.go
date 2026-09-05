//go:build !windows

package mousetrap

import "errors"

// Trap 非 Windows 平台占位类型（Hanxi 以 Windows 为主平台，接口保持可编译）。
type Trap struct{}

// Supported 报告当前平台是否支持全局鼠标长按钩子。
func Supported() bool { return false }

// Start 非 Windows 平台恒失败。
func Start(cfg Config) (*Trap, error) {
	return nil, errors.New("mousetrap: 仅支持 Windows 全局鼠标钩子")
}

// C 返回 nil 通道（读永远阻塞，配合 Stop 语义使用不到）。
func (t *Trap) C() <-chan Trigger { return nil }

// Buttons 非 Windows 平台恒为 nil 通道。
func (t *Trap) Buttons() <-chan ButtonEvent { return nil }

// Stop 非 Windows 平台空操作。
func (t *Trap) Stop() error { return nil }
