//go:build windows

package eartrumpet

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"hanxi/internal/platform"
)

// findProcessesUnder 枚举进程名为 EarTrumpet.exe 且可执行路径位于指定包安装
// 目录下的进程——用 WindowsApps 安装目录精确区分 Store/直装两个渠道身份，
// 避免按进程名一刀切误伤其他渠道或非托管的 loose 实例。
func findProcessesUnder(installLocation string, procs platform.ProcessAPI) []platform.ProcInfo {
	if procs == nil || strings.TrimSpace(installLocation) == "" {
		return nil
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []platform.ProcInfo
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exeName) {
			continue
		}
		info, err := procs.Query(entry.ProcessID)
		if err != nil || !isUnderDir(info.ExePath, installLocation) {
			continue
		}
		out = append(out, info)
	}
	return out
}
