// download_cancel_test.go 锁 downloadTo 的取消语义（ctx 贯穿改造的回归门）。
//
// 断言对象全部是**外部可观察事实**，不读实现形状：
//   - 服务端观察到的 TCP 新建连接数（证明"取消后一个源都没去敲"）；
//   - 墙上耗时（证明"退避间隙真能被取消打断"——旧形态裸 time.Sleep 必然睡满 1+2=3s）；
//   - 落盘文件是否被创建/写入（证明"取消不会留下半截包"）。
//
// 夹具一律用 127.0.0.1 本地端口，离线可跑、无需真机网络。
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

// deadPort 占一个端口再立刻释放，返回其地址——此后对它拨号必得"连接被拒"（毫秒级失败，
// 不经服务端处理，比 httptest 靶更适合做"快速失败"夹具）。
func deadPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占端口失败: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// countingListener 只计数不服务的靶：记录被 accept 的连接数，用于证明"有没有真去连"。
type countingListener struct {
	ln      net.Listener
	accepts chan struct{}
}

func newCountingListener(t *testing.T) *countingListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	cl := &countingListener{ln: ln, accepts: make(chan struct{}, 64)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			select {
			case cl.accepts <- struct{}{}:
			default:
			}
			_ = c.Close() // 立刻断开，让客户端拿到失败而不是挂住
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return cl
}

func (cl *countingListener) url() string { return "http://" + cl.ln.Addr().String() + "/pkg.exe" }

// countAccepted 在给定窗口内数连接（窗口到点即返回当前计数，不阻塞用例）。
func (cl *countingListener) countAccepted(within time.Duration) int {
	deadline := time.After(within)
	n := 0
	for {
		select {
		case <-cl.accepts:
			n++
		case <-deadline:
			return n
		}
	}
}

// TestDownloadToCanceledContextTriesNoSource 已取消的 context 下，downloadTo 必须
// 一个候选源都不去连（旧形态没有这个保证：它连 ctx 都没有，4 个镜像会挨个敲一遍）。
func TestDownloadToCanceledContextTriesNoSource(t *testing.T) {
	srv := newCountingListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进函数前就取消

	dest := filepath.Join(t.TempDir(), "pkg.exe")
	start := time.Now()
	err := downloadTo(ctx, &http.Client{Timeout: 5 * time.Second}, []string{srv.url(), srv.url()}, dest, nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消必须原样上抛 context.Canceled，实际 err=%v", err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("已取消却仍耗了 %v（取消应在任何拨号前即刻返回）", elapsed)
	}
	if got := srv.countAccepted(200 * time.Millisecond); got != 0 {
		t.Fatalf("取消后服务端观察到 %d 条新建连接，应为 0（一个源都不该去敲）", got)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatalf("取消路径却创建了目标文件 %s（不该留下半截包）", dest)
	}
}

// TestDownloadToBackoffInterruptibleByCancel 核心回归门：所有候选源都快速失败时，
// 第二轮重试之前要睡 1s、第三轮之前要睡 2s——旧形态是裸 time.Sleep，取消只能干等；
// 改造后在退避间隙取消必须即刻返回。判据用墙上耗时，取 1s 为界：
// 旧形态下限是 3s（1+2 两段退避），新形态实测在百毫秒级，1s 既能拉开差距又不怕调度噪声。
func TestDownloadToBackoffInterruptibleByCancel(t *testing.T) {
	dead := deadPort(t)
	urls := []string{"http://" + dead + "/a.exe", "http://" + dead + "/b.exe"}

	ctx, cancel := context.WithCancel(context.Background())
	// 第一轮 4 个源毫秒级失败后进入 1s 退避；在退避中途取消。
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
		t.Fatalf("退避间隙取消后仍耗时 %v（旧形态裸 time.Sleep 必然 ≥3s：这条正是它的回归门）", elapsed)
	}
}

// TestDownloadToWithoutCancelStillExhaustsRetries 反向护栏：ctx 正常时行为不变——
// 仍走满 2 轮重试并如实报"所有下载源与重试均失败"，证明上面的取消分支没把正常路径改松。
// 本用例会真睡满退避（1+2=3s），故 -short 下跳过。
func TestDownloadToWithoutCancelStillExhaustsRetries(t *testing.T) {
	if testing.Short() {
		t.Skip("需真睡满 1+2s 退避，-short 跳过")
	}
	dead := deadPort(t)
	urls := []string{"http://" + dead + "/a.exe"}

	dest := filepath.Join(t.TempDir(), "pkg.exe")
	err := downloadTo(context.Background(), &http.Client{Timeout: 5 * time.Second}, urls, dest, nil)
	if err == nil {
		t.Fatal("全部源不可达却返回 nil（重试耗尽必须如实报错）")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("未取消的路径却冒出 context.Canceled: %v", err)
	}
	if got := err.Error(); got == "" {
		t.Fatal("错误文案为空")
	}
}

// TestManagerDownloadContextRejectsCanceledUpfront Manager 层入口同样要先验 ctx：
// 已取消时不得进入任何远程解析/建临时文件的路径（外部事实=目标目录里什么都没多出来）。
func TestManagerDownloadContextRejectsCanceledUpfront(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.DownloadContext(ctx, "v9.9.9", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("入口未先验取消，实际 err=%v", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("读目录失败: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("已取消的调用却在数据目录留下 %d 项: %v", len(entries), entries)
	}
}
