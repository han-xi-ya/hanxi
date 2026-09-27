//go:build windows

package instance

import (
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"hanxi/internal/platform"
)

// mutexName 上游单实例互斥体名（Common/constants.hpp MUTEX_GUID，main.cpp 以
// CreateMutexW 裸名创建 = Local 命名空间；2026.2 便携 exe 二进制字符串实证）。
// 跨版本恒定：GUID 字面量写死在源码，非包名派生。
const mutexName = "344635E9-9AE4-4E60-B128-D53E25AB70A7"

type windowsTBProbe struct{}

// NewTBProbe Windows 实现：以 OpenMutex 探测单实例互斥体存在性，得到句柄即证明存活。
// 刻意只申请 SYNCHRONIZE 权限——存在性探测所需的最小权限，
// 避免 MUTEX_ALL_ACCESS 在特殊 ACL 场景下触发 access denied 误判。
func NewTBProbe() TBProbe { return &windowsTBProbe{} }

func (p *windowsTBProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *windowsTBProbe) WaitForReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsRunning() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.IsRunning()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// FindInstances Toolhelp32 快照按映像名枚举 TranslucentTB 实例，再逐个以
// PROCESS_QUERY_LIMITED_INFORMATION 最小权限读取完整镜像路径与启动时刻
// （QueryFullProcessImageName 对打包版返回 WindowsApps 部署位置——P1-8 归属
// 判定的事实源）。单进程读取失败（退出竞态/权限）跳过该进程；整体枚举失败
// 回空切片——互斥体仍是存活权威，本面只负责充实 PID/路径细节。
func (p *windowsTBProbe) FindInstances() []platform.ProcInfo {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry)) // MSDN（Toolhelp32 家族）：必须先置 dwSize，否则首查即败
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil
	}
	var out []platform.ProcInfo
	for {
		if strings.EqualFold(syscall.UTF16ToString(entry.ExeFile[:]), exeName) {
			if info, ok := queryInstanceImage(entry.ProcessID); ok {
				out = append(out, info)
			}
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	// 启动时刻升序：主实例（锁的持有者）恒早于其后信使化自退的次实例
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// queryInstanceImage 读取单进程镜像全路径与创建时刻；打包版目录 ACL 只拦
// "从外部直接执行"，不拦按 PID 的查询句柄（实证：close_windows 属主校验同款
// 查询在两种形态下均可用）。
func queryInstanceImage(pid uint32) (platform.ProcInfo, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return platform.ProcInfo{}, false
	}
	defer windows.CloseHandle(h)

	info := platform.ProcInfo{PID: pid, Name: exeName}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err == nil && creation.Nanoseconds() != 0 {
		info.StartedAt = time.Unix(0, creation.Nanoseconds())
	}
	buf := make([]uint16, 32*syscall.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return platform.ProcInfo{}, false
	}
	info.ExePath = syscall.UTF16ToString(buf[:size])
	return info, true
}
