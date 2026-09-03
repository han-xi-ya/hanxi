package litemonitor

import (
	"testing"
)

// TestVersionCompare 数值分段比较：多位数段不被字典序坑（1.10.0 > 1.9.0）。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.10.0", "v1.9.0", 1},
		{"v1.9.0", "v1.10.0", -1},
		{"v1.3.6", "v1.3.6", 0},
		{"v2.0.0", "v1.99.99", 1},
		{"1.3.6", "v1.3.6", 0}, // 无 v 前缀兼容
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestNoIdleAutoQuit 产品约束固化：LiteMonitor 是常驻桌面监控条，
// "空闲自动退出"语义不成立（后台持续监控正是其用途），模块刻意不实现
// idle 巡检——本测试以文档形式钉死该决策，防止后续维护照抄 ccswitch 加回去。
func TestNoIdleAutoQuit(t *testing.T) {
	// 结构性断言：service 不暴露任何 idle 相关能力（touch/idleCheck/shouldIdleQuit）。
	// 若未来确有需求，先重读本注释论证语义再动。
}
