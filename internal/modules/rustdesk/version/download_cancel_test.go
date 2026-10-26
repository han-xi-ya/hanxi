// download_cancel_test.go 锁 rustdesk 下载链的取消语义（ctx 贯穿改造的回归门）。
// 与 douzy 同谱；断言对象是外部可观察事实：服务端新建连接数、墙上耗时、目录是否被留脏。
// 旧形态（裸 time.Sleep 退避、无 ctx）在本门下必然红——已在 douzy 侧做过变异检验。
package version

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// deadPortForCancel 占位再释放，返回其地址：之后对它拨号必得"连接被拒"（毫秒级失败）。
func deadPortForCancel(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占端口失败: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// cancelCountingListener 只计数不服务的靶：记录 accept 次数，用于证明"有没有真去连"。
type cancelCountingListener struct {
	ln      net.Listener
	accepts chan struct{}
}

func newCancelCountingListener(t *testing.T) *cancelCountingListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	c := &cancelCountingListener{ln: ln, accepts: make(chan struct{}, 64)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			select {
			case c.accepts <- struct{}{}:
			default:
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return c
}

func (c *cancelCountingListener) url() string { return "http://" + c.ln.Addr().String() + "/pkg.exe" }

func (c *cancelCountingListener) countAccepted(within time.Duration) int {
	deadline := time.After(within)
	n := 0
	for {
		select {
		case <-c.accepts:
			n++
		case <-deadline:
			return n
		}
	}
}

// TestDownloadToCanceledContextTriesNoSource 已取消则一个候选源都不去敲，且不留半截文件。
func TestDownloadToCanceledContextTriesNoSource(t *testing.T) {
	srv := newCancelCountingListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := filepath.Join(t.TempDir(), "pkg.exe")
	start := time.Now()
	err := downloadTo(ctx, &http.Client{Timeout: 5 * time.Second}, []string{srv.url(), srv.url()}, dest, nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消必须原样上抛 context.Canceled，实际 err=%v", err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("已取消却仍耗了 %v", elapsed)
	}
	if got := srv.countAccepted(200 * time.Millisecond); got != 0 {
		t.Fatalf("取消后服务端观察到 %d 条新建连接，应为 0", got)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatalf("取消路径却创建了目标文件（不该留下半截包）")
	}
}

// TestDownloadToBackoffInterruptibleByCancel 退避间隙取消即刻返回；旧形态裸 time.Sleep
// 至少要睡满 1s 才继续（两段退避合计 3s），故 1s 为界既拉开差距又不怕调度噪声。
func TestDownloadToBackoffInterruptibleByCancel(t *testing.T) {
	dead := deadPortForCancel(t)
	urls := []string{"http://" + dead + "/a.exe", "http://" + dead + "/b.exe"}

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(80*time.Millisecond, cancel)
	t.Cleanup(func() { timer.Stop() })

	dest := filepath.Join(t.TempDir(), "pkg.exe")
	start := time.Now()
	err := downloadTo(ctx, &http.Client{Timeout: 5 * time.Second}, urls, dest, nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("退避中被取消应返回 context.Canceled，实际 err=%v", err)
	}
	if elapsed >= 1*time.Second {
		t.Fatalf("退避间隙取消后仍耗时 %v（旧形态必然 ≥1s，这条是它的回归门）", elapsed)
	}
}

// TestDownloadInstallerContextRejectsCanceledUpfront MSI 入口同样先验 ctx：
// 已取消时不得进入任何远程解析/建目录路径（外部事实=缓存目录零新增项）。
func TestDownloadInstallerContextRejectsCanceledUpfront(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := m.DownloadInstallerContext(ctx, "1.2.3-20240101", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("入口未先验取消，实际 err=%v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("已取消的调用却在数据目录留下 %d 项", len(entries))
	}
}
