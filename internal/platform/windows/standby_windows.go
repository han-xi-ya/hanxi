//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modAdvapi32SP = syscall.NewLazyDLL("advapi32.dll")
	modKernel32ML = syscall.NewLazyDLL("kernel32.dll")

	procGlobalMemoryStatusExML = modKernel32ML.NewProc("GlobalMemoryStatusEx")
	procGetSystemInfoML        = modKernel32ML.NewProc("GetSystemInfo")
)

const (
	// SeProfileSingleProcessPrivilege 是 NtSetSystemInformation 清理待机列表
	// 所需权限名称。MemoryPurgeStandbyList=4 与 Windows 内核
	// SYSTEM_MEMORY_LIST_COMMAND 约定一致（实证：phnt ntexapi.h 枚举按序取值，
	// 本仓库真机探针同步验证类 80 查询可用）；这是未文档化 Native API，
	// 调用方必须把失败当真实失败，不得把 nil/零差值包装成“释放成功”。
	seProfileSingleProcessPrivilege = "SeProfileSingleProcessPrivilege"
	memoryPurgeStandbyList          = 4
)

// systemMemoryListInfo SYSTEM_MEMORY_LIST_INFORMATION ——
// NtQuerySystemInformation(SystemMemoryListInformation /*80*/) 的查询布局。
// 实证依据：phnt（github.com/processhacker/phnt）ntexapi.h 该结构逐字段定义，
// 且 SystemMemoryListInformation 枚举注释 "q: SYSTEM_MEMORY_LIST_INFORMATION ... // 80"；
// golang.org/x/sys v0.47.0 同名常量实测为 80。本结构由本包按 phnt 布局
// 本地声明（SIZE_T→uintptr，全字段帧计数），字段含义不掺推测：
// PageCountByPriority[8] 为待机（standby）页按 8 级队列的帧数合计源，
// ModifiedPageCount/ModifiedNoWritePageCount 为修改页两态帧数。
type systemMemoryListInfo struct {
	ZeroPageCount             uintptr
	FreePageCount             uintptr
	ModifiedPageCount         uintptr
	ModifiedNoWritePageCount  uintptr
	BadPageCount              uintptr
	PageCountByPriority       [8]uintptr
	RepurposedPagesByPriority [8]uintptr
	ModifiedPageCountPageFile uintptr
}

// memoryStatusExML MEMORYSTATUSEX（GlobalMemoryStatusEx 要求先置 dwLength）。
type memoryStatusExML struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// systemInfoML SYSTEM_INFO（仅消费 dwPageSize，其余占位保证布局）。
type systemInfoML struct {
	ArchReserved          uint32
	PageSize              uint32
	MinApplicationAddress uintptr
	MaxApplicationAddress uintptr
	ActiveProcessorMask   uintptr
	NumberOfProcessors    uint32
	ProcessorType         uint32
	AllocationGranularity uint32
	ProcessorLevel        uint16
	ProcessorRevision     uint16
}

// MemoryLedger 一次内存账目快照：总量/可用来自文档化 API（必得），
// 待机/修改页来自类 80 查询（拿不到时 PagesMeasured=false 如实降级，绝不编数）。
// JSON 标签同时服务 helper 握手载荷（PurgeResultFile 内嵌）。
type MemoryLedger struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	StandbyBytes   uint64 `json:"standbyBytes"`
	ModifiedBytes  uint64 `json:"modifiedBytes"`
	PagesMeasured  bool   `json:"pagesMeasured"`
}

// CaptureMemoryLedger 采集一次账目快照。GlobalMemoryStatusEx 失败才报错；
// 类 80 页列表查询失败（权限/系统版本）降级为 PagesMeasured=false，不是错误。
func CaptureMemoryLedger() (MemoryLedger, error) {
	var st memoryStatusExML
	st.Length = uint32(unsafe.Sizeof(st))
	if r, _, e := procGlobalMemoryStatusExML.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return MemoryLedger{}, fmt.Errorf("GlobalMemoryStatusEx: %w", e)
	}
	ledger := MemoryLedger{TotalBytes: st.TotalPhys, AvailableBytes: st.AvailPhys}
	var si systemInfoML
	procGetSystemInfoML.Call(uintptr(unsafe.Pointer(&si))) // 文档化 API，无失败路径；防御 0 值
	if si.PageSize == 0 {
		return ledger, nil
	}
	var li systemMemoryListInfo
	if err := windows.NtQuerySystemInformation(windows.SystemMemoryListInformation, unsafe.Pointer(&li), uint32(unsafe.Sizeof(li)), nil); err != nil {
		return ledger, nil // 降级：待机/修改页拿不到实测，账目如实缺项
	}
	var standby uintptr
	for _, n := range li.PageCountByPriority {
		standby += n
	}
	// ModifiedPageCountPageFile 语义无可实证出处，不计入账目（禁编数纪律）。
	modified := li.ModifiedPageCount + li.ModifiedNoWritePageCount
	ledger.StandbyBytes = uint64(standby) * uint64(si.PageSize)
	ledger.ModifiedBytes = uint64(modified) * uint64(si.PageSize)
	ledger.PagesMeasured = true
	return ledger, nil
}

// StandbyResult 低层清理调用结果；Before/After 是动作前后账目快照，
// AvailableBytes 差只是观测值，不是精确待机页释放量。
type StandbyResult struct {
	Before MemoryLedger
	After  MemoryLedger
}

// PurgeStandbyList 启用一次性权限并请求内核清理待机列表，前后各采一次账目。
// 它不执行外部程序、不关闭任何用户进程；当前进程未提权或令牌没有权限时如实
// 返回错误。capture 为账目采集注入位（真机接线 windows.CaptureMemoryLedger）。
func PurgeStandbyList(capture func() (MemoryLedger, error)) (StandbyResult, error) {
	before, err := capture()
	if err != nil {
		return StandbyResult{}, fmt.Errorf("读取清理前内存账目失败: %w", err)
	}
	if err := enablePrivilege(seProfileSingleProcessPrivilege); err != nil {
		return StandbyResult{Before: before}, err
	}
	command := uint32(memoryPurgeStandbyList)
	if err := windows.NtSetSystemInformation(windows.SystemMemoryListInformation, unsafe.Pointer(&command), uint32(unsafe.Sizeof(command))); err != nil {
		return StandbyResult{Before: before}, fmt.Errorf("清理待机列表失败: %w", err)
	}
	after, err := capture()
	if err != nil {
		return StandbyResult{Before: before}, fmt.Errorf("读取清理后内存账目失败: %w", err)
	}
	return StandbyResult{Before: before, After: after}, nil
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
