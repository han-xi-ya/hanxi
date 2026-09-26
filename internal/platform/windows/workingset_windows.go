//go:build windows

package windows

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modKernel32WS = syscall.NewLazyDLL("kernel32.dll")
	// procK32EmptyWorkingSet PSAPI_VERSION≥2 时 EmptyWorkingSet 由 kernel32 直接
	// 导出（实证：MSDN "EmptyWorkingSet function (psapi.h)" Requirements——
	// Library Kernel32.lib / DLL Kernel32.dll，Win7+ 走 K32 前缀导出名）。
	procK32EmptyWorkingSet = modKernel32WS.NewProc("K32EmptyWorkingSet")
)

// WorkingSetResult 一次"清空全部进程工作集"动作的结果与前后账目。
type WorkingSetResult struct {
	Emptied int          // K32EmptyWorkingSet 调用成功的进程数
	Skipped int          // 打开失败/调用失败/红线跳过的进程数（受保护进程静默计入，不报错）
	Before  MemoryLedger // 动作前账目快照
	After   MemoryLedger // 动作后账目快照（工作集页会转入待机/修改，可用量未必上升）
}

// EmptyWorkingSets 遍历全机进程逐一调用 K32EmptyWorkingSet（RAMMap
// "Empty Working Sets" 同款语义）。句柄权限按 MSDN 文档取
// PROCESS_QUERY_LIMITED_INFORMATION|PROCESS_SET_QUOTA；先启用
// SeProfileSingleProcessPrivilege（与待机清理同一权限、同一 enablePrivilege
// 纪律），跨用户/受保护进程打开失败一律静默跳过并计数，绝不中断整体。
// 注意：清空工作集只是把页压回待机列表，不是"释放内存"——账目如实反映。
// capture 为账目采集注入位；emptyOne 为单进程动作注入位（测试替身）。
func EmptyWorkingSets(capture func() (MemoryLedger, error)) (WorkingSetResult, error) {
	return emptyWorkingSets(capture, emptyWorkingSetForPID)
}

// emptyWorkingSetForPID 对单个 PID 执行一次工作集清空；任何失败（含
// OpenProcess 访问被拒）返回 false，由调用方计入 Skipped。
func emptyWorkingSetForPID(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_SET_QUOTA, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	r, _, _ := procK32EmptyWorkingSet.Call(uintptr(h))
	return r != 0
}

func emptyWorkingSets(capture func() (MemoryLedger, error), emptyOne func(pid uint32) bool) (WorkingSetResult, error) {
	var res WorkingSetResult
	before, err := capture()
	if err != nil {
		return res, fmt.Errorf("读取清空工作集前内存账目失败: %w", err)
	}
	res.Before = before
	if err := enablePrivilege(seProfileSingleProcessPrivilege); err != nil {
		return res, err
	}
	pids, err := snapshotProcessIDs()
	if err != nil {
		return res, fmt.Errorf("枚举进程快照失败: %w", err)
	}
	res.Emptied, res.Skipped = applyEmptyWorkingSets(pids, emptyOne)
	after, err := capture()
	if err != nil {
		return res, fmt.Errorf("读取清空工作集后内存账目失败: %w", err)
	}
	res.After = after
	return res, nil
}

// applyEmptyWorkingSets 对 PID 快照逐一执行 emptyOne 并计数。
// 红线：Idle(0)/System(4) 内核工作集不由本工具触碰（其余受保护进程由
// OpenProcess 拒绝对齐"失败静默跳过"语义，不在此处猜名单）。
func applyEmptyWorkingSets(pids []uint32, emptyOne func(pid uint32) bool) (emptied, skipped int) {
	for _, pid := range pids {
		if pid == 0 || pid == 4 {
			skipped++
			continue
		}
		if emptyOne(pid) {
			emptied++
		} else {
			skipped++
		}
	}
	return
}

// snapshotProcessIDs CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS) 枚举全机
// 进程 PID（文档化 PSAPI 快照，只读）。
func snapshotProcessIDs() ([]uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	// MSDN（Toolhelp32Snapshot 家族）：调用方必须先置 dwSize，否则首查即败。
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return nil, nil
		}
		return nil, err
	}
	var pids []uint32
	for {
		pids = append(pids, entry.ProcessID)
		if err := windows.Process32Next(snap, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, err
		}
	}
	return pids, nil
}
