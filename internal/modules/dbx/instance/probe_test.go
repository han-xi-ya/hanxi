//go:build windows

package instance

import (
	"os"
	"testing"
	"time"
)

// 探针分治决策表（mutex seam 全覆盖，零真实 DBX/零真实互斥体触碰）：
// 互斥体三分裁决（held/absent/refused）× 进程枚举 × 主窗判据 →
// IsRunning / WaitForReady / IsMainWindowOpen 三个面的期望值。

func newFakeProbeDBX(verdict mutexVerdict, pids []uint32, win bool) *windowsDBXProbe {
	return &windowsDBXProbe{
		mutex:        func() mutexVerdict { return verdict },
		findPIDs:     func() []uint32 { return pids },
		mainWindowUp: func([]uint32) bool { return win },
	}
}

func TestProbeIsRunningDecisionTable(t *testing.T) {
	cases := []struct {
		name    string
		verdict mutexVerdict
		pids    []uint32
		window  bool
		want    bool
	}{
		{"互斥体在场即存活（信使短命进程不惊扰判据）", mutexHeld, nil, false, true},
		{"互斥体在场且进程枚举可得", mutexHeld, []uint32{1}, true, true},
		{"确证缺席（ERROR_FILE_NOT_FOUND）判死", mutexAbsent, nil, false, false},
		{"确证缺席时即便有游离进程窗也不翻案（互斥体主判据优先）", mutexAbsent, []uint32{9}, true, false},
		{"拒探兜底：进程名命中即存活（elevated 外部实例场）", mutexRefused, []uint32{4242}, false, true},
		{"拒探兜底：无进程在场判死", mutexRefused, nil, false, false},
	}
	for _, c := range cases {
		got := newFakeProbeDBX(c.verdict, c.pids, c.window).IsRunning()
		if got != c.want {
			t.Errorf("%s: IsRunning = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestProbeWaitForReadyDecision(t *testing.T) {
	// timeout=0：首轮判据 + 末位复探各一次即收口，不引入 100ms 睡眠轮次。
	if !newFakeProbeDBX(mutexHeld, nil, false).WaitForReady(0) {
		t.Error("互斥体在场应立即就绪（ccswitch 同判据）")
	}
	if !newFakeProbeDBX(mutexRefused, []uint32{7}, true).WaitForReady(0) {
		t.Error("拒探场可见主窗在场应判就绪（进程在场≠就绪，主窗才是）")
	}
	if newFakeProbeDBX(mutexRefused, []uint32{7}, false).WaitForReady(0) {
		t.Error("拒探场仅进程在场不应判就绪")
	}
	if newFakeProbeDBX(mutexAbsent, []uint32{7}, true).WaitForReady(0) {
		t.Error("互斥体确证缺席不应判就绪")
	}
}

func TestProbeIsMainWindowOpenDelegates(t *testing.T) {
	var gotPids []uint32
	p := &windowsDBXProbe{
		mutex:    func() mutexVerdict { return mutexAbsent },
		findPIDs: func() []uint32 { return []uint32{1, 2} },
		mainWindowUp: func(pids []uint32) bool {
			gotPids = pids
			return true
		},
	}
	if !p.IsMainWindowOpen() {
		t.Fatal("主窗判据应透传 true")
	}
	if len(gotPids) != 2 {
		t.Fatalf("判据应以进程枚举 PID 集合为过滤面, got %v", gotPids)
	}
}

// TestProbeFindPIDsRealImplSmoke 真实 Toolhelp32 枚举冒烟：本测试进程之外
// 无 DBX 在场（CI/开发机常态），返回集必须不含自身 PID 且调用不 panic；
// 真机装有 DBX 时返回非空亦属正常，断言只锁"不自指、不报错"两条底线。
func TestProbeFindPIDsRealImplSmoke(t *testing.T) {
	p := NewDBXProbe()
	pids := p.FindPIDs()
	for _, pid := range pids {
		if pid == uint32(os.Getpid()) {
			t.Fatalf("进程枚举混入自身 PID %d（DBX.exe 判据失效）", pid)
		}
	}
	_ = p.IsRunning()
	done := make(chan struct{})
	go func() { defer close(done); _ = p.WaitForReady(150 * time.Millisecond) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("WaitForReady 未按超时收口")
	}
}
