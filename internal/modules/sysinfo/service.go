package sysinfo

import (
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/google/uuid"

	"hanxi/internal/extapi"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// SysInfoService 面向前端的系统信息页服务：静态档案快照（GetReport，只读）
// 与两个谨慎的内存回收动作（PurgeStandby / EmptyWorkingSets）。档案采集全部为
// 注册表/WinAPI 只读调用（毫秒级），无缓存、无常驻资源、不产生 journal 事务——
// 与托管家族的写事务通道正交，仅经统一调用门（停用/阻止态拒调用）。
type SysInfoService struct {
	holder  *extapi.LeaseHolder
	purgeMu sync.Mutex // 两清理动作共享 single-flight：同一时刻至多一种在飞
	// 注入位（真机默认实现见 NewSysInfoService；测试以替身锁定账目透传语义）：
	purgeFn    func(capture func() (windows.MemoryLedger, error)) (windows.StandbyResult, error)
	wsFn       func(capture func() (windows.MemoryLedger, error)) (windows.WorkingSetResult, error)
	helperFn   func(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error)
	wsHelperFn func(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error)
	isElevated func() bool
}

// NewSysInfoService 构造（无 IO 副作用）。
func NewSysInfoService(holder *extapi.LeaseHolder) *SysInfoService {
	return &SysInfoService{
		holder:     holder,
		purgeFn:    windows.PurgeStandbyList,
		wsFn:       windows.EmptyWorkingSets,
		helperFn:   LaunchPurgeHelperElevated,
		wsHelperFn: LaunchEmptyWorkingSetsHelperElevated,
		isElevated: windows.IsElevated,
	}
}

// PurgeResult 内存回收动作回执。Before/After/AvailableDelta 三个旧字段语义不变
// （可用内存前后快照与观测差，不是精确释放量）；新字段是"先量后清再对账"的
// 逐项链账——Total 来自 GlobalMemoryStatusEx（必得），Standby/Modified 来自
// NtQuerySystemInformation 类 80 页列表查询，拿不到实测时 PagesMeasured=false
// 且对应字节数为 0（前端如实降级，禁编数）。Processes* 仅清空工作集动作填充。
type PurgeResult struct {
	BeforeAvailableBytes uint64 `json:"beforeAvailableBytes"`
	AfterAvailableBytes  uint64 `json:"afterAvailableBytes"`
	AvailableDeltaBytes  uint64 `json:"availableDeltaBytes"`
	Success              bool   `json:"success"`
	UsedHelper           bool   `json:"usedHelper"`
	Elevated             bool   `json:"elevated"`
	Message              string `json:"message"`

	BeforeTotalBytes    uint64 `json:"beforeTotalBytes"`
	PagesMeasured       bool   `json:"pagesMeasured"`
	BeforeStandbyBytes  uint64 `json:"beforeStandbyBytes"`
	AfterStandbyBytes   uint64 `json:"afterStandbyBytes"`
	BeforeModifiedBytes uint64 `json:"beforeModifiedBytes"`
	AfterModifiedBytes  uint64 `json:"afterModifiedBytes"`
	ProcessesEmptied    int    `json:"processesEmptied"`
	ProcessesSkipped    int    `json:"processesSkipped"`
}

// PurgeStandby 执行一次可回收待机列表清理（只动 Windows 的 standby 页列表，
// 不关程序、不删文件；压缩存储与进程工作集不在本动作射程）。已提权宿主走直接
// 路径，普通权限经一次性 UAC helper；两路径都带回前后账目。MCP 不引用此方法。
func (s *SysInfoService) PurgeStandby() (PurgeResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return PurgeResult{}, gateErr
	}
	defer release()
	s.purgeMu.Lock()
	defer s.purgeMu.Unlock()
	if !s.isElevated() {
		out, err := s.runHelper(s.helperFn, "purge-")
		if err != nil {
			return PurgeResult{Elevated: false, UsedHelper: true, Message: err.Error()}, nil
		}
		return helperResult(*out), nil
	}
	fn := s.purgeFn
	if fn == nil {
		fn = windows.PurgeStandbyList
	}
	out, err := fn(windows.CaptureMemoryLedger)
	if err != nil {
		res := ledgerResult(out.Before, windows.MemoryLedger{})
		res.Success = false
		res.Elevated = true
		res.Message = err.Error()
		return res, nil
	}
	res := ledgerResult(out.Before, out.After)
	res.Elevated = true
	res.Message = "已完成清理，可用内存变化仅作前后快照参考（不会关闭程序或删除数据）"
	return res, nil
}

// EmptyWorkingSets 执行一次"清空全部进程工作集"（RAMMap Empty Working Sets
// 同款谨慎语义）：把各进程正在占用的页强行压回待机列表，短暂普遍变卡属预期，
// Windows 随后自动管理回补。与 PurgeStandby 共享 purgeMu 单飞，同一时刻至多
// 一种清理在飞。MCP 不引用此方法。
func (s *SysInfoService) EmptyWorkingSets() (PurgeResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return PurgeResult{}, gateErr
	}
	defer release()
	s.purgeMu.Lock()
	defer s.purgeMu.Unlock()
	if !s.isElevated() {
		out, err := s.runHelper(s.wsHelperFn, "ews-")
		if err != nil {
			return PurgeResult{Elevated: false, UsedHelper: true, Message: err.Error()}, nil
		}
		return helperResult(*out), nil
	}
	fn := s.wsFn
	if fn == nil {
		fn = windows.EmptyWorkingSets
	}
	out, err := fn(windows.CaptureMemoryLedger)
	if err != nil {
		res := ledgerResult(out.Before, windows.MemoryLedger{})
		res.Success = false
		res.Elevated = true
		res.ProcessesEmptied = out.Emptied
		res.ProcessesSkipped = out.Skipped
		res.Message = err.Error()
		return res, nil
	}
	res := ledgerResult(out.Before, out.After)
	res.Elevated = true
	res.ProcessesEmptied = out.Emptied
	res.ProcessesSkipped = out.Skipped
	res.Message = "已清空各进程工作集（页被压回待机而非释放，随后几秒普遍变卡属预期）"
	return res, nil
}

// runHelper 拉起一次性提权 helper 并带回结果；返回 err 仅限"没拿到有效载荷"
// （启动/协议失败），denied/cancelled 属正常回执走 helperResult。
func (s *SysInfoService) runHelper(helper func(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error), idPrefix string) (*windows.PurgeResultFile, error) {
	if helper == nil {
		helper = LaunchPurgeHelperElevated
	}
	requestID := idPrefix + uuid.NewString()
	exe, err := os.Executable()
	if err != nil {
		return nil, errors.New("无法定位 Hanxi 程序，未提权清理未执行")
	}
	out, err := helper(settings.GetPaths().RuntimeDir(), exe, requestID)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// helperResult 把 helper 载荷映射为前端回执。旧语义保留：Success 只看 state，
// delta 只由可用内存观测差算出；新账目字段逐项链账透传（含工作集计数）。
func helperResult(out windows.PurgeResultFile) PurgeResult {
	res := ledgerResult(out.BeforeLedger, out.AfterLedger)
	if out.BeforeLedger == (windows.MemoryLedger{}) && out.AfterLedger == (windows.MemoryLedger{}) {
		// v2 前老账或缺账载荷：退回纯可用观测（不编账目）
		res = ledgerResult(windows.MemoryLedger{AvailableBytes: out.BeforeAvailableBytes}, windows.MemoryLedger{AvailableBytes: out.AfterAvailableBytes})
	}
	res.UsedHelper = true
	res.Elevated = out.Elevated
	res.Success = out.State == "success"
	res.ProcessesEmptied = out.EmptiedProcessCount
	res.ProcessesSkipped = out.SkippedProcessCount
	if !res.Success || res.Message == "" {
		res.Message = out.Message
	}
	return res
}

// ledgerResult 由前后账目算出回执：delta 语义与历史一致（可用观测差），
// 账目不可得项保持零值 + PagesMeasured=false，由前端如实降级。
func ledgerResult(before, after windows.MemoryLedger) PurgeResult {
	delta := uint64(0)
	if after.AvailableBytes > before.AvailableBytes {
		delta = after.AvailableBytes - before.AvailableBytes
	}
	return PurgeResult{
		BeforeAvailableBytes: before.AvailableBytes,
		AfterAvailableBytes:  after.AvailableBytes,
		AvailableDeltaBytes:  delta,
		Success:              true,
		BeforeTotalBytes:     before.TotalBytes,
		PagesMeasured:        before.PagesMeasured && after.PagesMeasured,
		BeforeStandbyBytes:   before.StandbyBytes,
		AfterStandbyBytes:    after.StandbyBytes,
		BeforeModifiedBytes:  before.ModifiedBytes,
		AfterModifiedBytes:   after.ModifiedBytes,
	}
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
