//go:build windows

// mousetrap_pure_test.go 默认档纯逻辑用例：只驱动泵线程决策函数
// （normalized/onThreshold/endGesture/deliver/notifyClick/Stop 兜底），
// 不安装真实钩子、不触碰输入链（分档纪律见包测试头注释）。
package mousetrap

import (
	"testing"
	"time"
	"unsafe"
)

func TestConfigNormalized(t *testing.T) {
	cases := []struct {
		name string
		in   Config
		want Config
	}{
		{"零值回落出厂", Config{}, Config{MinHold: defaultMinHold, MaxMove: defaultMaxMove}},
		{"负值同样回落", Config{MinHold: -time.Second, MaxMove: -8}, Config{MinHold: defaultMinHold, MaxMove: defaultMaxMove}},
		{"合法值原样保留", Config{MinHold: 200 * time.Millisecond, MaxMove: 64}, Config{MinHold: 200 * time.Millisecond, MaxMove: 64}},
		{"超大值不截断（钳域是上游 quickmenu 的职责）", Config{MinHold: time.Hour, MaxMove: 99999}, Config{MinHold: time.Hour, MaxMove: 99999}},
		{"只缺一项补一项", Config{MinHold: 300 * time.Millisecond}, Config{MinHold: 300 * time.Millisecond, MaxMove: defaultMaxMove}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.normalized(); got != c.want {
				t.Errorf("normalized(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

// newTestTrap 构造未挂钩子的纯状态机夹具：通道容量与 Start 完全一致，
// 保证投递/丢弃行为测的是真实容量。
func newTestTrap(cfg Config) (*Trap, *trapState) {
	t := &Trap{
		ch:     make(chan Trigger, 1),
		clicks: make(chan ButtonEvent, 8),
		done:   make(chan struct{}),
	}
	return t, &trapState{trap: t, cfg: cfg.normalized()}
}

func TestEndGestureClearsArmedSoThresholdNeverFires(t *testing.T) {
	tr, st := newTestTrap(Config{})
	st.armed = true
	st.suppressed = false
	st.revealed = false
	st.t0 = time.Now()
	st.timer = time.AfterFunc(time.Millisecond, func() {}) // 表已挂：endGesture 必须 Stop 它
	st.endGesture()

	if st.armed || st.suppressed || st.revealed {
		t.Fatalf("endGesture 后判定标志未复位: %+v", st)
	}
	onThreshold(st) // 迟到的阈值请求（AfterFunc 竞态收口场景）
	select {
	case trg := <-tr.C():
		t.Errorf("手势已终结却仍投递触发: %+v", trg)
	default:
	}
}

func TestOnThresholdFiresOnceWhenArmed(t *testing.T) {
	// 判据：GetCursorPos 是 onThreshold 投递前的真实依赖（非交互窗口站返回失败
	// = 本用例无法在该环境构造）；成功即桌面在场，正常断言。
	var probe point
	if r, _, errno := procGetCursorPos.Call(uintptr(unsafe.Pointer(&probe))); r == 0 {
		t.Skipf("GetCursorPos 失败(%v)：无交互桌面，onThreshold 的取坐标投递无法构造", errno)
	}
	tr, st := newTestTrap(Config{MinHold: 20 * time.Millisecond})
	st.armed = true
	st.t0 = time.Now().Add(-2 * time.Second) // 按住 2s（单调钟：Since 精确）

	onThreshold(st)
	if !st.suppressed {
		t.Fatal("达阈值必须标记 suppressed（抬手吞净的前提）")
	}
	select {
	case trg := <-tr.C():
		if trg.Hold < 2*time.Second {
			t.Errorf("Hold=%v，应 ≥ 实际按住 2s（单调时钟可精确断言）", trg.Hold)
		}
	default:
		t.Fatal("armed 达阈值必须投递一次触发")
	}

	// 重复阈值请求（定时器多拍/竞态重投）：不得二次投递
	onThreshold(st)
	if len(tr.C()) != 0 {
		t.Errorf("suppressed 后重复阈值仍投递触发: %+v", <-tr.C())
	}
}

func TestOnThresholdIgnoresNonArmedStates(t *testing.T) {
	for _, c := range []struct {
		name                        string
		armed, revealed, suppressed bool
	}{
		{"未武装（无在手手势）", false, false, false},
		{"拖拽已让位", true, true, false},
		{"已弹窗抑制中", true, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr, st := newTestTrap(Config{})
			st.armed, st.revealed, st.suppressed = c.armed, c.revealed, c.suppressed
			st.t0 = time.Now().Add(-time.Second)
			onThreshold(st)
			if len(tr.C()) != 0 {
				t.Errorf("非法态不该投递触发: %+v", <-tr.C())
			}
			if c.name == "未武装（无在手手势）" && st.suppressed {
				t.Error("未武装时不得被标记为已抑制（污染新手势判定）")
			}
		})
	}
}

// runWithin 在限定时限内执行 fn：投递原语一旦回归为阻塞式（背压卡输入链），
// 用例限时失败而非拖死整个测试二进制。
func runWithin(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("投递调用阻塞（承诺为非阻塞丢弃）")
	}
}

func TestDeliverDropsWhenChannelFull(t *testing.T) {
	tr, st := newTestTrap(Config{})
	runWithin(t, time.Second, func() {
		deliver(st, Trigger{X: 1, Y: 1})
		deliver(st, Trigger{X: 2, Y: 2}) // 消费端积压：静默丢弃
	})
	if len(tr.C()) != 1 {
		t.Fatalf("容量 1 的触发通道积压后仍应只有 1 条，实际 %d", len(tr.C()))
	}
	if trg := <-tr.C(); trg.X != 1 {
		t.Errorf("首条不得被覆盖: %+v", trg)
	}
}

func TestNotifyClickBroadcastCapAndCoords(t *testing.T) {
	tr, st := newTestTrap(Config{})
	info := &msllHookStruct{X: 1234, Y: -567}
	runWithin(t, time.Second, func() {
		for i := 0; i < 20; i++ {
			notifyClick(st, info)
		}
	})
	if len(tr.Buttons()) != 8 {
		t.Fatalf("观察通道容量应为 8，实际 %d", len(tr.Buttons()))
	}
	ev := <-tr.Buttons()
	if ev.X != 1234 || ev.Y != -567 {
		t.Errorf("广播坐标应原样取自事件: %+v", ev)
	}
}

func TestStopNilTrapIsSafe(t *testing.T) {
	var tr *Trap
	if err := tr.Stop(); err != nil {
		t.Errorf("nil Trap 的 Stop 应幂等无错: %v", err)
	}
	// 无活动识别器时 Start 前置清理不得炸
	activeTrap.Store(nil)
	stopActive()
}
