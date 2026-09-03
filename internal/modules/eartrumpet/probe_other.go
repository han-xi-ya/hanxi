//go:build !windows

package eartrumpet

import "hanxi/internal/platform"

// findProcessesUnder 非 Windows 平台兜底（应用实际 Windows-only，仅保编译）。
func findProcessesUnder(string, platform.ProcessAPI) []platform.ProcInfo { return nil }
