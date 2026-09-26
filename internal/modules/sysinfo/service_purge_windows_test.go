//go:build windows

package sysinfo

import (
	"errors"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/platform/windows"
)

func newTestService() *SysInfoService {
	s := NewSysInfoService(extapi.NewLeaseHolder(ID))
	s.isElevated = func() bool { return true }
	return s
}

func fullLedger(avail, standby, modified uint64) windows.MemoryLedger {
	return windows.MemoryLedger{TotalBytes: 32 << 30, AvailableBytes: avail, StandbyBytes: standby, ModifiedBytes: modified, PagesMeasured: true}
}

// TestPurgeStandbyLedgerPassthrough 直连路径：账目逐项进回执，旧 delta 语义不变。
func TestPurgeStandbyLedgerPassthrough(t *testing.T) {
	s := newTestService()
	s.purgeFn = func(capture func() (windows.MemoryLedger, error)) (windows.StandbyResult, error) {
		return windows.StandbyResult{Before: fullLedger(4<<30, 8<<30, 512<<20), After: fullLedger(11<<30, 1<<30, 640<<20)}, nil
	}
	res, err := s.PurgeStandby()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || !res.Elevated || res.UsedHelper {
		t.Fatalf("回执状态失真: %+v", res)
	}
	if res.BeforeAvailableBytes != 4<<30 || res.AfterAvailableBytes != 11<<30 || res.AvailableDeltaBytes != 7<<30 {
		t.Fatalf("旧账目字段失真: %+v", res)
	}
	if !res.PagesMeasured || res.BeforeStandbyBytes != 8<<30 || res.AfterStandbyBytes != 1<<30 || res.BeforeModifiedBytes != 512<<20 || res.AfterModifiedBytes != 640<<20 || res.BeforeTotalBytes != 32<<30 {
		t.Fatalf("逐项链账失真: %+v", res)
	}
}

// TestPurgeStandbyDirectFailureReportsLedgerSoFar 直连失败：Success=false，
// 已采到的前账目照常送达（先量后清，量到的就是量到的）。
func TestPurgeStandbyDirectFailureReportsLedgerSoFar(t *testing.T) {
	s := newTestService()
	s.purgeFn = func(capture func() (windows.MemoryLedger, error)) (windows.StandbyResult, error) {
		return windows.StandbyResult{Before: fullLedger(4<<30, 8<<30, 1<<20)}, errors.New("清理待机列表失败: 注入")
	}
	res, err := s.PurgeStandby()
	if err != nil || res.Success || res.Message == "" || res.BeforeStandbyBytes != 8<<30 {
		t.Fatalf("失败回执必须 Success=false 且带前账目: %+v %v", res, err)
	}
}

// TestEmptyWorkingSetsLedgerAndCounts 直连路径：成功/跳过计数与账目同时送达。
func TestEmptyWorkingSetsLedgerAndCounts(t *testing.T) {
	s := newTestService()
	s.wsFn = func(capture func() (windows.MemoryLedger, error)) (windows.WorkingSetResult, error) {
		return windows.WorkingSetResult{Emptied: 97, Skipped: 12, Before: fullLedger(4<<30, 8<<30, 1<<20), After: fullLedger(5<<30, 12<<30, 2<<20)}, nil
	}
	res, err := s.EmptyWorkingSets()
	if err != nil || !res.Success || !res.Elevated {
		t.Fatalf("回执状态失真: %+v %v", res, err)
	}
	if res.ProcessesEmptied != 97 || res.ProcessesSkipped != 12 || res.AvailableDeltaBytes != 1<<30 {
		t.Fatalf("计数/账目失真: %+v", res)
	}
	if res.AfterStandbyBytes < res.BeforeStandbyBytes {
		t.Fatalf("清空工作集后待机页应上升（页被压回而非释放）: %+v", res)
	}
}

// TestHelperResultPassesLedgerAndCounts 普通权限 helper 路径：v2 载荷账目与
// 工作集计数逐项链账透传，旧 cancelled/denied 语义不动。
func TestHelperResultPassesLedgerAndCounts(t *testing.T) {
	s := NewSysInfoService(extapi.NewLeaseHolder(ID))
	s.isElevated = func() bool { return false }
	s.wsHelperFn = func(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error) {
		if requestID == "" || len(requestID) < 16 {
			t.Fatalf("request ID 形状异常: %q", requestID)
		}
		return windows.PurgeResultFile{
			Mode: windows.EmptyWorkingSetsHelperMode, State: "success",
			BeforeLedger: fullLedger(2<<30, 6<<30, 1<<20), AfterLedger: fullLedger(6<<30, 9<<30, 1<<20),
			EmptiedProcessCount: 42, SkippedProcessCount: 7, Elevated: true,
		}, nil
	}
	res, err := s.EmptyWorkingSets()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || !res.UsedHelper || !res.Elevated || res.ProcessesEmptied != 42 || res.ProcessesSkipped != 7 {
		t.Fatalf("helper 回执失真: %+v", res)
	}
	if !res.PagesMeasured || res.BeforeStandbyBytes != 6<<30 || res.AvailableDeltaBytes != 4<<30 {
		t.Fatalf("helper 账目失真: %+v", res)
	}
}

// TestHelperLaunchFailureIsReportedNotPanicked helper 启动失败给结构化回执。
func TestHelperLaunchFailureIsReportedNotPanicked(t *testing.T) {
	s := NewSysInfoService(extapi.NewLeaseHolder(ID))
	s.isElevated = func() bool { return false }
	s.helperFn = func(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error) {
		return windows.PurgeResultFile{}, errors.New("UAC 未应答")
	}
	res, err := s.PurgeStandby()
	if err != nil || res.Success || !res.UsedHelper || res.Message == "" {
		t.Fatalf("启动失败必须结构化回执: %+v %v", res, err)
	}
}

// TestZeroLedgerFallsBackToAvailableObservation 无账目载荷（全零前后）退回
// 纯可用观测：旧字段照常、PagesMeasured=false，绝不伪装成实测账目。
func TestZeroLedgerFallsBackToAvailableObservation(t *testing.T) {
	got := helperResult(windows.PurgeResultFile{
		State: "success", BeforeAvailableBytes: 3, AfterAvailableBytes: 9, Message: "ok",
	})
	if got.PagesMeasured || got.BeforeStandbyBytes != 0 || got.AvailableDeltaBytes != 6 || !got.Success {
		t.Fatalf("降级口径失真: %+v", got)
	}
}
