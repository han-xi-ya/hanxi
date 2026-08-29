//go:build windows

package windows

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"hanxi/internal/platform"
)

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
)

type ProcessImpl struct{}

func NewProcessAPI() platform.ProcessAPI {
	return &ProcessImpl{}
}

// IsProtected 判断是否为系统保护/红线进程 (PID 0, PID 4, Hanxi 本身)
func (p *ProcessImpl) IsProtected(pid uint32, info platform.ProcInfo) bool {
	if pid == 0 || pid == 4 {
		return true
	}
	if pid == uint32(os.Getpid()) {
		return true
	}
	// 基础系统关键进程保护
	lowName := strings.ToLower(info.Name)
	if lowName == "csrss.exe" || lowName == "wininit.exe" || lowName == "services.exe" ||
		lowName == "lsass.exe" || lowName == "smss.exe" || lowName == "svchost.exe" {
		return true
	}
	return false
}

// Query 查询进程详细信息
func (p *ProcessImpl) Query(pid uint32) (platform.ProcInfo, error) {
	info := platform.ProcInfo{PID: pid}

	if pid == 0 {
		info.Name = "System Idle Process"
		return info, nil
	}
	if pid == 4 {
		info.Name = "System"
		return info, nil
	}

	// 尝试以 QUERY_LIMITED_INFORMATION 打开句柄
	hProc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return info, fmt.Errorf("OpenProcess failed for PID %d: %w", pid, err)
	}
	defer windows.CloseHandle(hProc)

	// 获取启动时间
	var creationTime, exitTime, kernelTime, userTime windows.Filetime
	if err := windows.GetProcessTimes(hProc, &creationTime, &exitTime, &kernelTime, &userTime); err == nil {
		info.StartedAt = time.Unix(0, creationTime.Nanoseconds())
	}

	// 获取完整可执行文件路径
	var buf [windows.MAX_LONG_PATH]uint16
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(
		uintptr(hProc),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r != 0 && size > 0 {
		info.ExePath = syscall.UTF16ToString(buf[:size])
		info.Name = filepath.Base(info.ExePath)
	}

	return info, nil
}

// KillVerified 安全复核令牌后终止进程
func (p *ProcessImpl) KillVerified(ctx context.Context, token platform.VerifyToken, force bool) error {
	// 1. 系统保护红线检查
	if token.PID == 0 || token.PID == 4 || token.PID == uint32(os.Getpid()) {
		return platform.ErrProtectedProcess
	}

	// 2. 查杀前复核进程指纹 (PID + StartTime + ExePath)
	current, err := p.Query(token.PID)
	if err != nil {
		return platform.ErrProcessNotFound
	}

	if p.IsProtected(token.PID, current) {
		return platform.ErrProtectedProcess
	}

	// 若提供了期望的 ExePath 或 StartTime，做精确比对防 PID 复用
	if token.ExePath != "" && current.ExePath != "" && !strings.EqualFold(token.ExePath, current.ExePath) {
		return platform.ErrTokenMismatch
	}
	if !token.StartedAt.IsZero() && !current.StartedAt.IsZero() {
		// 允许 1 秒以内的精度误差
		diff := token.StartedAt.Sub(current.StartedAt)
		if diff < -time.Second || diff > time.Second {
			return platform.ErrTokenMismatch
		}
	}

	// 3. 打开带 TERMINATE 权限的句柄
	hProc, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, token.PID)
	if err != nil {
		if errorsIsAccessDenied(err) {
			return platform.ErrAccessDenied
		}
		return fmt.Errorf("open process terminate permission failed: %w", err)
	}
	defer windows.CloseHandle(hProc)

	// 4. 终止进程
	if err := windows.TerminateProcess(hProc, 1); err != nil {
		if errorsIsAccessDenied(err) {
			return platform.ErrAccessDenied
		}
		return fmt.Errorf("TerminateProcess failed: %w", err)
	}

	return nil
}

func errorsIsAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "access is denied") ||
		strings.Contains(err.Error(), "5")
}
