//go:build windows

package keyops

// 重解析点判定（Windows 分支）。
//
// 为什么必须有这段、而且不能只靠 filepath.EvalSymlinks：本机实测（见 fence_test.go 的
// TestFenceRejectsWindowsJunction）Go 的 EvalSymlinks **不解析 junction**——它按段 Lstat，
// 只有被标成 ModeSymlink 的段才去读链接内容，而 junction 目录在 Lstat 里就是普通目录。
// 于是 `keys/junction/id_victim`（junction 指向 keys 之外）在 EvalSymlinks 之后**原样**
// 返回，字面看着还在围栏内，跟着它删却删到了外面——正是本仓踩过的那颗雷（构建 worktree
// 的 junction 连坐删除）。FILE_ATTRIBUTE_REPARSE_POINT 这个属性位对软链接与 junction
// 都置位，所以逐段查它能覆盖两种形态。

import (
	"os"
	"syscall"
)

// reparsePoint 判一个 Lstat 结果是不是重解析点（软链接、junction、以及其它 reparse 形态）。
func reparsePoint(fi os.FileInfo) bool {
	fa, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		// 极少数文件系统给不出 Win32 属性结构。退回 Mode 位判软链接——
		// 判不准的那部分由"最终组件必须普通文件"那道判定兜住，不会静默放行。
		return fi.Mode()&os.ModeSymlink != 0
	}
	return fa.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
