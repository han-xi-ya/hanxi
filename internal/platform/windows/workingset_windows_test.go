//go:build windows

package windows

import (
	"os"
	"strings"
	"testing"
)

// TestApplyEmptyWorkingSetsCountsAndRedlines 锁定计数语义：Idle(0)/System(4)
// 红线静默跳过，emptyOne 成败分别入 Emptied/Skipped，绝不中断遍历。
func TestApplyEmptyWorkingSetsCountsAndRedlines(t *testing.T) {
	var seen []uint32
	emptied, skipped := applyEmptyWorkingSets([]uint32{0, 4, 100, 101, 102}, func(pid uint32) bool {
		seen = append(seen, pid)
		return pid == 101
	})
	if emptied != 1 || skipped != 4 {
		t.Fatalf("计数失真: emptied=%d skipped=%d", emptied, skipped)
	}
	for _, pid := range seen {
		if pid == 0 || pid == 4 {
			t.Fatalf("红线 PID %d 不得进入动作回调", pid)
		}
	}
}

// TestSnapshotProcessIDsOnHost 真机枚举冒烟（只读）：必须看到当前进程。
func TestSnapshotProcessIDsOnHost(t *testing.T) {
	pids, err := snapshotProcessIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) < 5 {
		t.Fatalf("进程快照数量不合常理: %d", len(pids))
	}
	self := uint32(os.Getpid())
	var found bool
	for _, p := range pids {
		if p == self {
			found = true
		}
	}
	if !found {
		t.Fatalf("快照必须包含当前进程 %d", self)
	}
}

// TestEmptyWorkingSetsCaptureFailureShortCircuits 采集失败必须在启用权限与
// 遍历任何进程之前短路（测试宿主绝不允许真的清空全机工作集）。
func TestEmptyWorkingSetsCaptureFailureShortCircuits(t *testing.T) {
	_, err := emptyWorkingSets(func() (MemoryLedger, error) {
		return MemoryLedger{}, &testError{}
	}, func(uint32) bool {
		t.Fatal("采集失败后不得调用任何进程动作")
		return false
	})
	if err == nil || !strings.Contains(err.Error(), "读取清空工作集前内存账目失败") {
		t.Fatalf("采集失败必须短路报错，got %v", err)
	}
}
