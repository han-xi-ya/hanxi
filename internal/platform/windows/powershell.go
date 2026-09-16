//go:build windows

package windows

import "strings"

// PsQuote 将字符串包装为 PowerShell 单引号字面量（内部单引号成对转义）。
// 用单引号而非双引号：路径含 $ 或反引号时不会被 PowerShell 插值。
func PsQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
