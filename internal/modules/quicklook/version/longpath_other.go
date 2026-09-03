//go:build !windows

package version

// longPath 非 Windows 平台恒等返回：POSIX 无 MAX_PATH 的 \\?\ 前缀语义，
// 且 Hanxi 仅在 Windows 分发，解压深层路径问题在目标平台由 Windows 实现解决。
func longPath(p string) string { return p }
