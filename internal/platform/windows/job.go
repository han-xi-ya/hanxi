//go:build windows

package windows

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"hanxi/internal/platform"
)

// JobImpl platform.JobAPI 的 Windows 实现（无状态，仅作工厂载体）。
type JobImpl struct{}

// NewJobAPI 返回 Job Object 管理 API 实例。
func NewJobAPI() platform.JobAPI {
	return &JobImpl{}
}

// Create 创建匿名 Job Object 并默认启用 KILL_ON_JOB_CLOSE，
// 保证 Hanxi 崩溃或被杀时托管子进程不残留。失败时不泄漏已创建句柄。
func (j *JobImpl) Create() (platform.Job, error) {
	// 创建无名的 Job Object
	hJob, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("CreateJobObject failed: %w", err)
	}

	// 配置限制：主进程或 Job 句柄关闭时，强杀 Job 内的所有关联子进程 (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	_, err = windows.SetInformationJobObject(
		hJob,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		windows.CloseHandle(hJob)
		return nil, fmt.Errorf("SetInformationJobObject failed: %w", err)
	}

	return &windowsJob{
		hJob: hJob,
	}, nil
}

// windowsJob 持有单个 Job Object 原生句柄。
// mu 保护 hJob 的置零关闭与所有句柄操作，Close 后各方法须安全退化而非 panic。
type windowsJob struct {
	hJob windows.Handle
	mu   sync.Mutex
}

// Assign 以 SET_QUOTA|TERMINATE 权限打开目标进程并挂入本 Job。
// 注意 Windows 约束：已属于其他 Job 的进程（如被 Job 启动的子进程）在 Win8+ 可嵌套挂入。
func (wj *windowsJob) Assign(pid uint32) error {
	wj.mu.Lock()
	defer wj.mu.Unlock()

	if wj.hJob == 0 {
		return fmt.Errorf("job is already closed")
	}

	hProc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return fmt.Errorf("open process for job assign failed: %w", err)
	}
	defer windows.CloseHandle(hProc)

	if err := windows.AssignProcessToJobObject(wj.hJob, hProc); err != nil {
		return fmt.Errorf("AssignProcessToJobObject failed for PID %d: %w", pid, err)
	}
	return nil
}

// Close 幂等关闭句柄（重复调用返回 nil）；KILL_ON_JOB_CLOSE 开启时内核连带杀组内进程。
func (wj *windowsJob) Close() error {
	wj.mu.Lock()
	defer wj.mu.Unlock()

	if wj.hJob != 0 {
		err := windows.CloseHandle(wj.hJob)
		wj.hJob = 0
		return err
	}
	return nil
}

// Terminate 杀死组内全部进程但保留句柄；已关闭（hJob==0）时视为已完成，返回 nil。
func (wj *windowsJob) Terminate(exitCode uint32) error {
	wj.mu.Lock()
	defer wj.mu.Unlock()

	if wj.hJob != 0 {
		return windows.TerminateJobObject(wj.hJob, exitCode)
	}
	return nil
}

// SetAllowKillOnClose 动态切换 KILL_ON_JOB_CLOSE（见 platform.Job 接口注释）。
// 对已 Assign 的进程依旧生效：SetInformationJobObject 可随时重设限制位。
func (wj *windowsJob) SetAllowKillOnClose(enabled bool) error {
	wj.mu.Lock()
	defer wj.mu.Unlock()

	if wj.hJob == 0 {
		return fmt.Errorf("job is already closed")
	}
	var flags uint32
	if enabled {
		flags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: flags,
		},
	}
	_, err := windows.SetInformationJobObject(
		wj.hJob,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return fmt.Errorf("SetInformationJobObject failed: %w", err)
	}
	return nil
}
