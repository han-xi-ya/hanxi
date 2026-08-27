package flclash

import (
	"testing"
	"time"

	"hubkit/internal/modules/flclash/instance"
)

// TestShouldIdleQuit 空闲退出判定穷举：仅"自有 running + 主窗口未开 + 超阈值"才退出。
// 最小化到任务栏（窗口不可见）不豁免——无人操作 3 分钟退出是明确需求。
func TestShouldIdleQuit(t *testing.T) {
	running := instance.Snapshot{State: instance.StateRunning, External: false}
	runningExt := instance.Snapshot{State: instance.StateRunning, External: true}
	stopped := instance.Snapshot{State: instance.StateStopped}

	cases := []struct {
		name       string
		snap       instance.Snapshot
		windowOpen bool
		idle       time.Duration
		want       bool
	}{
		{"超阈值无窗口", running, false, idleQuitAfter + time.Minute, true},
		{"恰好阈值", running, false, idleQuitAfter, true},
		{"未达阈值", running, false, idleQuitAfter - time.Minute, false},
		{"窗口开着豁免", running, true, idleQuitAfter + time.Minute, false},
		{"外部实例不碰", runningExt, false, idleQuitAfter + time.Minute, false},
		{"未运行不退出", stopped, false, idleQuitAfter + time.Minute, false},
		{"零空闲", running, false, 0, false},
	}
	for _, c := range cases {
		if got := shouldIdleQuit(c.snap, c.windowOpen, c.idle); got != c.want {
			t.Errorf("%s: shouldIdleQuit = %v, want %v", c.name, got, c.want)
		}
	}
}
