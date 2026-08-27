package bcu

import (
	"testing"
	"time"

	"hubkit/internal/modules/bcu/instance"
)

// TestShouldIdleQuitDisabled 固化 BCU 不因空闲自动退出的产品约束。
func TestShouldIdleQuitDisabled(t *testing.T) {
	cases := []struct {
		name       string
		snap       instance.Snapshot
		windowOpen bool
		idle       time.Duration
	}{
		{"自有运行实例", instance.Snapshot{State: instance.StateRunning}, false, 24 * time.Hour},
		{"窗口已打开", instance.Snapshot{State: instance.StateRunning}, true, 24 * time.Hour},
		{"外部运行实例", instance.Snapshot{State: instance.StateRunning, External: true}, false, 24 * time.Hour},
		{"已停止实例", instance.Snapshot{State: instance.StateStopped}, false, 24 * time.Hour},
	}
	for _, c := range cases {
		if shouldIdleQuit(c.snap, c.windowOpen, c.idle) {
			t.Errorf("%s: BCU 不应因空闲自动退出", c.name)
		}
	}
}
