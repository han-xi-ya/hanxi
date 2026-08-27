//go:build windows

package windows

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"hubkit/internal/platform"
)

type JobImpl struct{}

func NewJobAPI() platform.JobAPI {
	return &JobImpl{}
}

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

type windowsJob struct {
	hJob windows.Handle
	mu   sync.Mutex
}

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
