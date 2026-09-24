//go:build windows

package windows

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

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
	if err := windows.AdjustTokenPrivileges(token, false, &state, 0, nil, nil); err != nil {
		return fmt.Errorf("启用 %s 权限失败: %w", name, err)
	}
	if err := windows.GetLastError(); err != nil {
		return fmt.Errorf("启用 %s 权限未获分配: %w", name, err)
	}
	return nil
}
