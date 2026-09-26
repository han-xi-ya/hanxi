//go:build windows

package everything

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hanxi/internal/extapi"
	evinstance "hanxi/internal/modules/everything/instance"
	evversion "hanxi/internal/modules/everything/version"
	"hanxi/internal/platform/windows"
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
		holder:  extapi.NewLeaseHolder(ID),
	}
	s.engine = evinstance.NewEngine(plat.Job(), evinstance.NewEverythingProbe(plat.Process()), evinstance.Callbacks{
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

// downloadReceipt 下载事件票据：stage/version 为 app 组件回执载荷，
// activeAtBroadcast 由探针在 emitDownload 广播位（与生产 Wails Emit 同一点位）
// 于下载 goroutine 内同步快照——即"前端此刻复刷 GetActiveVersion 会读到什么"的实况。
type downloadReceipt struct {
	stage             string
	version           string
	activeAtBroadcast string
}

// 锁死 termora 2ac9b3b 同型病灶的修复：前端共享 store 收到 done 成功回执即
// 复刷 GetActiveVersion，"首装自动设使用"的落账必须先于事件广播完成（断言取
// 广播位快照，非事后补读——事后读会与被测代码赛跑，负序回归可能漏网）。假驱动
// 经 seam 隔离网络（绝不真下载），事件时序与 journal/租约全走生产闭包实现。
func TestDownloadDoneSettlesActiveBeforeBroadcast(t *testing.T) {
	svc, _ := newTestService(t)
	svc.downloads = map[string]struct{}{} // newTestService 未预装配下载面（生产经构造函数注入）
	seen := make(chan downloadReceipt, 32)
	svc.downloadProbe = func(tk DownloadTicket) {
		if tk.Component != "app" {
			return
		}
		seen <- downloadReceipt{stage: tk.Stage, version: tk.Version, activeAtBroadcast: svc.store.GetActive()}
	}
	svc.downloadDriver = func(_ context.Context, _, targetVersion string, emit func(evversion.DownloadProgress)) error {
		emit(evversion.DownloadProgress{Version: targetVersion, Stage: "downloading", Done: 1, Total: 2})
		emit(evversion.DownloadProgress{Version: targetVersion, Stage: "verify"})
		emit(evversion.DownloadProgress{Version: targetVersion, Stage: "extract"})
		emit(evversion.DownloadProgress{Version: targetVersion, Stage: "done", Done: 100, Total: 100})
		return nil
	}

	if state, err := svc.DownloadVersion("1.4.2"); err != nil || state != "started" {
		t.Fatalf("DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitAppDoneReceipt(t, seen); rr.version != "1.4.2" {
		t.Fatalf("app done 回执版本应为 1.4.2，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "1.4.2" {
		t.Fatalf("done 成功回执广播位上 active 应已落账为 1.4.2，实得 %q", rr.activeAtBroadcast)
	}

	// 已有使用版本时不得被新下载覆盖（落账条件仅"未设使用"，语义随迁移保持不变）。
	if state, err := svc.DownloadVersion("1.4.3"); err != nil || state != "started" {
		t.Fatalf("第二次 DownloadVersion: state=%q err=%v", state, err)
	}
	if rr := waitAppDoneReceipt(t, seen); rr.version != "1.4.3" {
		t.Fatalf("第二次下载应广播 1.4.3 的 app done 回执，实得 %q", rr.version)
	} else if rr.activeAtBroadcast != "1.4.2" {
		t.Fatalf("active 已有值时广播位不应被后续下载改写，实得 %q", rr.activeAtBroadcast)
	}
}

// waitAppDoneReceipt 等下载 goroutine 广播到 app 组件 done 票据（drain 中间
// 阶段），返回其广播位快照；超时判失败。
func waitAppDoneReceipt(t *testing.T, seen <-chan downloadReceipt) downloadReceipt {
	t.Helper()
	for {
		select {
		case r := <-seen:
			if r.stage == "done" {
				return r
			}
		case <-time.After(5 * time.Second):
			t.Fatal("等待下载 done 事件超时")
			return downloadReceipt{}
		}
	}
}
