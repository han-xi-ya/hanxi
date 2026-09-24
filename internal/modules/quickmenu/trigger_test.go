package quickmenu

import (
	"testing"
	"time"
)

// N5-C2 触发参数钳制表测：出域值一律钳回合法域（0=出厂默认语义），
// 绝不让坏配置武装鼠标钩子。
func TestEffectiveTriggerClamp(t *testing.T) {
	cases := []struct {
		holdIn, moveIn int
		wantHold       time.Duration
		wantMove       int
	}{
		{0, 0, 450 * time.Millisecond, 16},        // 未设置=出厂
		{300, 10, 300 * time.Millisecond, 10},     // 合法域原样
		{50, 2, 200 * time.Millisecond, 4},        // 双下限钳
		{99999, 999, 1500 * time.Millisecond, 64}, // 双上限钳
		{-7, -1, 450 * time.Millisecond, 16},      // 负值=默认（非下限）
	}
	for _, c := range cases {
		h, m := effectiveTrigger(c.holdIn, c.moveIn)
		if h != c.wantHold || m != c.wantMove {
			t.Errorf("(%d,%d) → (%v,%d), want (%v,%d)", c.holdIn, c.moveIn, h, m, c.wantHold, c.wantMove)
		}
	}
}
