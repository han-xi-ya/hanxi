package piclite

import (
	"testing"
	"time"

	"hanxi/internal/modules/piclite/instance"
)

// TestShouldIdleQuit 空闲退出判定穷举：仅 running 且非 external、无可见窗口、
// 空闲时长达标才退。
func TestShouldIdleQuit(t *testing.T) {
	running := instance.Snapshot{State: instance.StateRunning}
	external := instance.Snapshot{State: instance.StateExternal, External: true}
	stopped := instance.Snapshot{State: instance.StateStopped}
	long := idleQuitAfter + time.Minute
	short := idleQuitAfter - time.Minute

	cases := []struct {
		name string
		snap instance.Snapshot
		win  bool
		idle time.Duration
		want bool
	}{
		{"running+窗口开+久空闲→豁免", running, true, long, false},
		{"running+窗口关+久空闲→退出", running, false, long, true},
		{"running+窗口关+短空闲→不退", running, false, short, false},
		{"external→永不接管退出", external, false, long, false},
		{"stopped→不退", stopped, false, long, false},
	}
	for _, c := range cases {
		if got := shouldIdleQuit(c.snap, c.win, c.idle); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// TestVersionCompare 数值分段比较：多位数段不被字典序坑。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.4.1", "v1.4.0", 1},
		{"v1.9.0", "v1.10.0", -1}, // 字典序会判反
		{"v1.4.1", "v1.4.1", 0},
		{"v2.0.0", "v1.99.99", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
