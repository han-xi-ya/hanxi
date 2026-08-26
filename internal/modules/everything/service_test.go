//go:build windows

package everything

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	evinstance "hubkit/internal/modules/everything/instance"
	evversion "hubkit/internal/modules/everything/version"
	"hubkit/internal/platform/windows"
)

// newTestService 构造临时 versions 目录的 service（引擎用真实平台原语但测试不启动进程）。
func newTestService(t *testing.T) (*EverythingService, string) {
	t.Helper()
	plat, err := windows.New()
	if err != nil {
		t.Fatalf("windows.New: %v", err)
	}
	versionsDir := t.TempDir()

	s := &EverythingService{
		plat:    plat,
		store:   newEverythingStore(t.TempDir()),
		manager: evversion.NewManager(versionsDir),
		esDir:   filepath.Join(t.TempDir(), "everything", "es"),
	}
	s.engine = evinstance.NewEngine(plat.Job(), evinstance.NewEverythingProbe(), evinstance.Callbacks{
		OnState: s.emitInstanceState,
	})
	return s, versionsDir
}

// mkVersion 在 versions 目录构造一个已安装版本（假 exe 即可，resolve 只查存在性）。
func mkVersion(t *testing.T, versionsDir, version string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "everything_v"+version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "Everything.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	return exe
}

// TestShouldIdleQuit 空闲退出判定穷举：仅"自有 running + 窗口未开 + 超阈值"才退出。
func TestShouldIdleQuit(t *testing.T) {
	running := evinstance.Snapshot{State: evinstance.StateRunning, External: false}
	runningExt := evinstance.Snapshot{State: evinstance.StateRunning, External: true}
	stopped := evinstance.Snapshot{State: evinstance.StateStopped}

	cases := []struct {
		name       string
		snap       evinstance.Snapshot
		windowOpen bool
		idle       time.Duration
		want       bool
	}{
		{"超阈值纯后台", running, false, idleQuitAfter + time.Minute, true},
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

// TestResolveActiveVersionFallback activeVersion 优先级与失效自愈：
//  1. 未设定 → 回退已装最新（数值比较，1.5.0.1422b > 1.4.1.1032，尾字母参与排序）；
//  2. 设定存在 → 用设定值；
//  3. 设定失效（目录已删）→ 清空自愈并回退最新；
//  4. 全部无 → 明确错误。
func TestResolveActiveVersionFallback(t *testing.T) {
	s, versionsDir := newTestService(t)

	// 4. 空场报错
	if _, _, err := s.resolveActiveVersion(); err == nil {
		t.Fatal("无已装版本应报错")
	}

	mkVersion(t, versionsDir, "1.4.1.1032")
	mkVersion(t, versionsDir, "1.5.0.1422b")

	// 1. 未设定 → 最新
	v, exe, err := s.resolveActiveVersion()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if v != "1.5.0.1422b" || !filepath.IsAbs(exe) {
		t.Fatalf("未设定时应用最新版本，实际 %q %q", v, exe)
	}

	// 2. 设定存在 → 用设定值
	if err := s.store.SetActive("1.4.1.1032"); err != nil {
		t.Fatal(err)
	}
	v, _, _ = s.resolveActiveVersion()
	if v != "1.4.1.1032" {
		t.Fatalf("应用设定版本，实际 %q", v)
	}

	// 3. 设定失效 → 自愈回退最新
	if err := os.RemoveAll(filepath.Join(versionsDir, "everything_v1.4.1.1032")); err != nil {
		t.Fatal(err)
	}
	v, _, err = s.resolveActiveVersion()
	if err != nil {
		t.Fatalf("失效自愈失败: %v", err)
	}
	if v != "1.5.0.1422b" {
		t.Fatalf("自愈后应用最新版本，实际 %q", v)
	}
	if s.store.GetActive() != "" {
		t.Fatalf("失效版本应从 store 清空，实际 %q", s.store.GetActive())
	}
}