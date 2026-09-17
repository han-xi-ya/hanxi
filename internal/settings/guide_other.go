//go:build !windows

package settings

import (
	"log/slog"
	"os"
)

// ExitWithBindingGuide 非 Windows 编译兜底（产品仅发行 Windows 形态）：
// stderr 留痕 + 非零退出，同样绝不静默回退用户目录。
func ExitWithBindingGuide(err error) {
	if err == nil {
		return
	}
	slog.Error("fatal: hanxi data root unavailable, exiting", "err", err)
	os.Exit(1)
}
