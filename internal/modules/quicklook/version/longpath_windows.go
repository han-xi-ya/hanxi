//go:build windows

package version

import (
	"strings"
)

// longPath 给驱动器号绝对路径加 "\\?\" 扩展长度前缀，突破 Win32 MAX_PATH(260)。
//
// QuickLook 是 .NET 便携多文件包，QuickLook.Plugin\...\runtimes\<rid>\lib\<tfm>\*.dll
// 这类深层树在 %APPDATA%\Hanxi\versions\quicklook_X.Y.Z 基目录下极易超过 260，
// 解压/导入时 CreateFileW、Mkdir 会以"系统找不到指定的路径"失败。前缀 \\?\ 后
// 内核按最长 ~32767 处理（Go 的 os.Create/MkdirAll 透传该路径不经规范化）。
//
// 前提：传入 p 须为绝对、已 Clean、反斜杠分隔（filepath.Join 产物满足）；
// 已带前缀者原样返回；UNC 需 \\?\UNC\ 特殊形式，本模块目标恒为本地盘不涉及。
func longPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	// 仅对 "X:\" 形式的驱动器绝对路径加前缀
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) {
		return `\\?\` + p
	}
	return p
}
