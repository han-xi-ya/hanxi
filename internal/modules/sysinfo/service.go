package sysinfo

import (
	"log/slog"
	"os"
	"sync"

	"github.com/google/uuid"

	"hanxi/internal/extapi"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// SysInfoService 面向前端的系统档案服务：单次 RPC 返回全量快照。
// 全部采集为注册表/WinAPI 只读调用（毫秒级），无缓存、无常驻资源、
// 不产生 journal 事务——与托管家族的写事务通道正交，仅经统一调用门
// （停用/阻止态拒调用）。
type SysInfoService struct {
	holder   *extapi.LeaseHolder
	purgeMu  sync.Mutex
	purgeFn  func(before, after func() (uint64, error)) (windows.StandbyResult, error)
	helperFn func(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error)
}

// NewSysInfoService 构造（无 IO 副作用）。
func NewSysInfoService(holder *extapi.LeaseHolder) *SysInfoService {
	return &SysInfoService{holder: holder, purgeFn: windows.PurgeStandbyList, helperFn: LaunchPurgeHelperElevated}
}

// PurgeResult 可用内存变化快照；delta 不是精确待机页释放量，只是前后观测差。
type PurgeResult struct {
	BeforeAvailableBytes uint64 `json:"beforeAvailableBytes"`
	AfterAvailableBytes  uint64 `json:"afterAvailableBytes"`
	AvailableDeltaBytes  uint64 `json:"availableDeltaBytes"`
	Success              bool   `json:"success"`
	UsedHelper           bool   `json:"usedHelper"`
	Elevated             bool   `json:"elevated"`
	Message              string `json:"message"`
}

// PurgeStandby 执行一次可回收待机列表清理。首版先支持已提权宿主的直接路径；
// 普通权限的 shared helper 接线在下一原子提交完成。MCP 不引用此方法。
func (s *SysInfoService) PurgeStandby() (PurgeResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return PurgeResult{}, gateErr
	}
	defer release()
	s.purgeMu.Lock()
	defer s.purgeMu.Unlock()
	fn := s.purgeFn
	if fn == nil {
		fn = windows.PurgeStandbyList
	}
	before := func() (uint64, error) { m, err := collectMemory(); return m.AvailableBytes, err }
	if !windows.IsElevated() {
		helper := s.helperFn
		if helper == nil {
			helper = LaunchPurgeHelperElevated
		}
		requestID := "purge-" + uuid.NewString()
		exe, err := os.Executable()
		if err != nil {
			return PurgeResult{Elevated: false, Message: "无法定位 Hanxi 程序，未提权清理未执行"}, nil
		}
		out, err := helper(settings.GetPaths().RuntimeDir(), exe, requestID)
		if err != nil {
			return PurgeResult{Elevated: false, UsedHelper: true, Message: err.Error()}, nil
		}
		return helperResult(out), nil
	}
	out, err := fn(before, availablePhysicalBytes)
	if err != nil {
		return PurgeResult{BeforeAvailableBytes: out.BeforeAvailableBytes, Elevated: true, Message: err.Error()}, nil
	}
	return resultFromSnapshot(out), nil
}

func helperResult(out windows.PurgeResultFile) PurgeResult {
	res := resultFromSnapshot(windows.StandbyResult{BeforeAvailableBytes: out.BeforeAvailableBytes, AfterAvailableBytes: out.AfterAvailableBytes})
	res.UsedHelper = true
	res.Elevated = out.Elevated
	res.Success = out.State == "success"
	if !res.Success {
		res.Message = out.Message
	}
	return res
}

func resultFromSnapshot(out windows.StandbyResult) PurgeResult {
	delta := uint64(0)
	if out.AfterAvailableBytes > out.BeforeAvailableBytes {
		delta = out.AfterAvailableBytes - out.BeforeAvailableBytes
	}
	return PurgeResult{BeforeAvailableBytes: out.BeforeAvailableBytes, AfterAvailableBytes: out.AfterAvailableBytes, AvailableDeltaBytes: delta, Success: true, Elevated: true, Message: "已完成清理，可用内存变化仅作前后快照参考（不会关闭程序或删除数据）"}
}

// GetReport 采集一次本机软硬件档案。段级失败如实记入 Errors（前端告警条
// 呈现），成功段照常送达——绝不为"表观全绿"隐藏采集边界。
func (s *SysInfoService) GetReport() (Report, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Report{}, gateErr
	}
	defer release()

	var rep Report
	collect := func(name string, fn func() error) {
		if err := fn(); err != nil {
			slog.Debug("sysinfo 段采集失败", "section", name, "err", err)
			rep.Errors = append(rep.Errors, name+": "+err.Error())
		}
	}
	collect("machine", func() error {
		v, err := collectMachine()
		rep.Machine = v
		return err
	})
	collect("os", func() error {
		v, err := collectOS()
		rep.OS = v
		return err
	})
	collect("cpu", func() error {
		v, err := collectCPU()
		rep.CPU = v
		return err
	})
	collect("memory", func() error {
		v, err := collectMemory()
		rep.Memory = v
		return err
	})
	collect("gpu", func() error {
		v, err := collectGPUs()
		rep.GPUs = v
		return err
	})
	collect("displays", func() error {
		v, err := collectDisplays()
		rep.Displays = v
		return err
	})
	collect("volumes", func() error {
		v, err := collectVolumes()
		rep.Volumes = v
		return err
	})
	collect("network", func() error {
		v, err := collectNetwork()
		rep.Network = v
		return err
	})
	return rep, nil
}
