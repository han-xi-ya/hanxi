//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var modAdvapi32SP = syscall.NewLazyDLL("advapi32.dll")

const (
	// SeProfileSingleProcessPrivilege 是 NtSetSystemInformation 清理待机列表
	// 所需权限名称。MemoryPurgeStandbyList=4 与 Windows 内核
	// SYSTEM_MEMORY_LIST_COMMAND 约定一致；这是未文档化 Native API，调用方
	// 必须把失败当真实失败，不得把 nil/零差值包装成“释放成功”。
	seProfileSingleProcessPrivilege = "SeProfileSingleProcessPrivilege"
	memoryPurgeStandbyList          = 4
)

// StandbyResult 低层清理调用结果；Available* 是动作前后快照，不是精确清理量。
type StandbyResult struct {
	BeforeAvailableBytes uint64
	AfterAvailableBytes  uint64
}

// PurgeStandbyList 启用一次性权限并请求内核清理待机列表。它不执行外部程序、
// 不关闭任何用户进程；当前进程未提权或令牌没有权限时如实返回错误。
func PurgeStandbyList(before, after func() (uint64, error)) (StandbyResult, error) {
	beforeBytes, err := before()
	if err != nil {
		return StandbyResult{}, fmt.Errorf("读取清理前可用内存失败: %w", err)
	}
	if err := enablePrivilege(seProfileSingleProcessPrivilege); err != nil {
		return StandbyResult{BeforeAvailableBytes: beforeBytes}, err
	}
	command := uint32(memoryPurgeStandbyList)
	if err := windows.NtSetSystemInformation(windows.SystemMemoryListInformation, unsafe.Pointer(&command), uint32(unsafe.Sizeof(command))); err != nil {
		return StandbyResult{BeforeAvailableBytes: beforeBytes}, fmt.Errorf("清理待机列表失败: %w", err)
	}
	afterBytes, err := after()
	if err != nil {
		return StandbyResult{BeforeAvailableBytes: beforeBytes}, fmt.Errorf("读取清理后可用内存失败: %w", err)
	}
	return StandbyResult{BeforeAvailableBytes: beforeBytes, AfterAvailableBytes: afterBytes}, nil
}

const errorNotAllAssigned syscall.Errno = 1300 // ERROR_NOT_ALL_ASSIGNED

// enablePrivilege 启用具名权限。审查 P1#3 修正：Windows 上
// AdjustTokenPrivileges 对"部分权限未分配"**照常返回 TRUE**，唯一信号是
// 紧随其后的 GetLastError()==1300；而 x/sys 包装器的 err 只覆盖调用本身，
// 且 Go 运行时在**每次** syscall 前 SetLastError(0)——调用完再独立取
// GetLastError 恒为 0（实测 `<nil>`），是死分支。唯一可信取法：本函数
// 直连 proc 拿 SyscallN 第三返回值（同调用栈内抓 lasterr，不被清零）。
func enablePrivilege(name string) error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("打开当前进程权限令牌失败: %w", err)
	}
	defer token.Close()
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return fmt.Errorf("准备权限名称失败: %w", err)
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, namePtr, &luid); err != nil {
		return fmt.Errorf("查询 %s 权限失败: %w", name, err)
	}
	state := windows.Tokenprivileges{PrivilegeCount: 1}
	state.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	proc := modAdvapi32SP.NewProc("AdjustTokenPrivileges")
	r1, _, e1 := syscall.SyscallN(proc.Addr(), uintptr(token), 0, uintptr(unsafe.Pointer(&state)), 0, 0, 0)
	if r1 == 0 {
		return fmt.Errorf("启用 %s 权限失败: %v", name, e1)
	}
	if e1 == errorNotAllAssigned {
		return fmt.Errorf("当前令牌未持有 %s 权限（组策略/完整性等级限制）", name)
	}
	return nil
}
