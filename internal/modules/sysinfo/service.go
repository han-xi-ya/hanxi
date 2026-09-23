package sysinfo

import (
	"log/slog"

	"hanxi/internal/extapi"
)

// SysInfoService 面向前端的系统档案服务：单次 RPC 返回全量快照。
// 全部采集为注册表/WinAPI 只读调用（毫秒级），无缓存、无常驻资源、
// 不产生 journal 事务——与托管家族的写事务通道正交，仅经统一调用门
// （停用/阻止态拒调用）。
type SysInfoService struct {
	holder *extapi.LeaseHolder
}

// NewSysInfoService 构造（无 IO 副作用）。
func NewSysInfoService(holder *extapi.LeaseHolder) *SysInfoService {
	return &SysInfoService{holder: holder}
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
