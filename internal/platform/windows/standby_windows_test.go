//go:build windows

package windows

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestSystemMemoryListInformationConstant 锁死类 80 常量实证：phnt ntexapi.h
// 注释 "// 80" 与 x/sys v0.47.0 一致；若未来换库导致值漂移，此处先红。
func TestSystemMemoryListInformationConstant(t *testing.T) {
	if int(windows.SystemMemoryListInformation) != 80 {
		t.Fatalf("SystemMemoryListInformation 实证值应为 80（phnt），got %d", windows.SystemMemoryListInformation)
	}
}

// TestCaptureMemoryLedgerOnHost 真机冒烟：总量/可用必得；页列表若可测则须
// 落在物理内存之内（待机+修改 > 总量即为布局失真，必须红）。
func TestCaptureMemoryLedgerOnHost(t *testing.T) {
	l, err := CaptureMemoryLedger()
	if err != nil {
		t.Fatal(err)
	}
	if l.TotalBytes == 0 || l.AvailableBytes == 0 || l.AvailableBytes > l.TotalBytes {
		t.Fatalf("基础账目异常: %+v", l)
	}
	if !l.PagesMeasured {
		t.Skipf("本机类 80 查询不可得（权限/系统边界），降级路径已生效: %+v", l)
	}
	if l.StandbyBytes+l.ModifiedBytes > l.TotalBytes {
		t.Fatalf("页列表账目超出物理总量，布局或页大小实证失真: %+v", l)
	}
}

// TestPurgeStandbyListFailingCaptureShortCircuits 账目采集失败必须在任何
// 内核动作前短路（不在测试宿主上真清待机列表，故只测失败侧）。
func TestPurgeStandbyListFailingCaptureShortCircuits(t *testing.T) {
	calls := 0
	_, err := PurgeStandbyList(func() (MemoryLedger, error) {
		calls++
		return MemoryLedger{}, &testError{}
	})
	if err == nil || !strings.Contains(err.Error(), "读取清理前内存账目失败") {
		t.Fatalf("采集失败必须短路报错，got %v", err)
	}
	if calls != 1 {
		t.Fatalf("采集失败后不得再读第二次，calls=%d", calls)
	}
}

type testError struct{}

func (*testError) Error() string { return "注入的采集故障" }
