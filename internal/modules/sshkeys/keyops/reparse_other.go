//go:build !windows

package keyops

// 重解析点判定（非 Windows 分支）。
//
// POSIX 上只有符号链接这一种形态，且 Go 的 EvalSymlinks 会真的解析它（与 junction 不同）。
// 这里的判定因此是**第二道**：主判定靠 EvalSymlinks 把落点解析出来再比围栏，这道用来
// 把"路径中途有链接"这种形态明明白白拒掉——改别人的目录结构不是本包的动作范围。

import (
	"os"
)

// reparsePoint 判一个 Lstat 结果是不是符号链接。
func reparsePoint(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}
