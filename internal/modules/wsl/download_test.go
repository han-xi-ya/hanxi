package wsl

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadMsiSingleFlightGateNotStuck 钉死"单飞闸拒绝不锁死"回归：
// 旧实现 dlBusy/dlOp 两道闸分置两段且 goroutine 收尾先清 dlBusy 后清 dlOp，
// 撞上窗口的请求过了 dlBusy 门却被 registerDownload 拒掉——早退路径不还原
// dlBusy、不 cancel 自建 opCtx，此后本会话每一次 DownloadMsi 都被
// "已有下载正在进行"拒绝，下载功能永久锁死。
// 现单飞收口 registerDownload 一处：断言三个外部事实——
//  1. 在飞期间的并发请求被拒且**没有**发起第二路下载（驱动调用计数为 1）；
//  2. 在飞一路收口后闸自动放通，后续请求可再受理（旧 bug 下此处永远拒绝）；
//  3. 被拒请求不产生 done 事件、不登记落盘路径（闸门放通只看在飞一路收口）。
func TestDownloadMsiSingleFlightGateNotStuck(t *testing.T) {
	svc, _, _ := newTestService()

	var calls atomic.Int32
	dlStarted := make(chan struct{})
	release := make(chan struct{})
	svc.downloadTo = func(_ context.Context, tag, name, dir, rawURL string) (string, error) {
		if calls.Add(1) == 1 {
			close(dlStarted)
		}
		<-release // 模拟数十 MB 级慢下载在飞
		return filepath.Join(dir, name), nil
	}
	events := make(chan DownloadProgress, 8)
	svc.emit = func(_ string, payload any) {
		if p, ok := payload.(DownloadProgress); ok {
			events <- p
		}
	}

	const tag, msiName = "v2.9.10", "wsl.2.9.10.0.x64.msi"
	if out, err := svc.DownloadMsi(tag, msiName); err != nil || !out.Success {
		t.Fatalf("首路下载应受理: %+v %v", out, err)
	}
	select {
	case <-dlStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("下载驱动未在 5s 内启动")
	}

	// 在飞期间的并发请求：拒绝，且绝不得触达下载驱动
	for i := 0; i < 3; i++ {
		if _, err := svc.DownloadMsi(tag, msiName); err == nil {
			t.Fatal("在飞期间的并发下载请求必须被单飞闸拒绝")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("被拒请求触发了额外下载（驱动调用 %d 次）", got)
	}

	// 放落在飞一路：驱动收口 → 事件 done → dlOp 销账
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	var doneSeen *DownloadProgress
	for doneSeen == nil {
		select {
		case p := <-events:
			if p.Stage == "done" {
				p := p
				doneSeen = &p
			}
		case <-time.After(100 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("首路下载 10s 内未收口出 done 事件")
			}
		}
	}
	if doneSeen.Path == "" || !strings.HasSuffix(doneSeen.Path, msiName) {
		t.Fatalf("done 事件应携带落盘路径，实得 %q", doneSeen.Path)
	}

	// 旧 bug 的锁死点在此显形：闸收口干净后，再来一轮必须能受理
	svc.downloadTo = func(_ context.Context, tag, name, dir, rawURL string) (string, error) {
		calls.Add(1)
		return filepath.Join(dir, name+"-r2"), nil
	}
	if out, err := svc.DownloadMsi(tag, msiName); err != nil || !out.Success {
		t.Fatalf("前一轮收口后下载功能应恢复可用（旧实现此处永久锁死）: %+v %v", out, err)
	}
	// 等第二轮收口出 done 事件（不留后台在飞协程），再断言驱动实际二次发起
	waitDeadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case p := <-events:
			if p.Stage == "done" {
				if got := calls.Load(); got != 2 {
					t.Fatalf("第二轮下载应实际发起（驱动调用计数 = %d）", got)
				}
				return
			}
		case <-time.After(100 * time.Millisecond):
			if time.Now().After(waitDeadline) {
				t.Fatal("第二轮下载 10s 内未收口")
			}
		}
	}
}
