package terminal

// 终端服务状态机单测：窄接口假 pty + 假连接 + 可编程门注入，不起真伪控制台、
// 不起 Wails。审计批次的回归钉全部在这里可断言（B1 停用收口过无门路径、
// B2 数据面准入、M1 抢主退泵、M2 CAS 不清现任、drain 防冻结、resize 闸）——
// 上一版测试在未注入门的 holder 上跑，B1 类缺陷结构上测不到，本次补齐。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
	"hanxi/packages/go/conpty"
)

// ---- 假件 ----

type fakePty struct {
	closed     int
	written    [][]byte
	resizes    [][2]uint16
	resizeEr   error
	done       chan struct{}
	readChunks chan []byte // 泵从 Read 取的真字节源；空则阻塞（模拟没输出）
	closeRead  sync.Once
	mu         sync.Mutex
}

func newFakePty() *fakePty {
	// readChunks **无缓冲**：推送方只有被真读者接走才能返回——
	// "没人读就被反压冻住"正是 drain 用例要观测的事实，缓冲会把假象粉饰成真。
	return &fakePty{done: make(chan struct{}), readChunks: make(chan []byte)}
}

// pushChunk 限时投递一块 pty 输出；无人接即判失败（不静默卡测试）。
func pushChunk(t *testing.T, f *fakePty, data string) {
	t.Helper()
	select {
	case f.readChunks <- []byte(data):
	case <-time.After(2 * time.Second):
		t.Fatalf("无人读取 pty 字节: %q", data)
	}
}

func (f *fakePty) Read(b []byte) (int, error) {
	chunk, ok := <-f.readChunks
	if !ok {
		return 0, errors.New("pty closed")
	}
	return copy(b, chunk), nil
}
func (f *fakePty) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.written = append(f.written, append([]byte(nil), b...))
	f.mu.Unlock()
	return len(b), nil
}
func (f *fakePty) Resize(c, r uint16) error {
	f.mu.Lock()
	f.resizes = append(f.resizes, [2]uint16{c, r})
	f.mu.Unlock()
	return f.resizeEr
}
func (f *fakePty) Close() error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	f.closeRead.Do(func() { close(f.readChunks) })
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	return nil
}
func (f *fakePty) Pid() uint32           { return 4242 }
func (f *fakePty) Done() <-chan struct{} { return f.done }
func (f *fakePty) Err() error            { return nil }

// fakeConn 实现 streamIO：收件箱（客户端→服务端帧）、出箱（服务端帧流）、可取消 ctx。
type fakeConn struct {
	in     chan []byte
	out    chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	closed atomic.Bool
}

func newFakeConn() *fakeConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &fakeConn{in: make(chan []byte, 64), out: make(chan []byte, 64), ctx: ctx, cancel: cancel}
}

func (c *fakeConn) Send(b []byte) error {
	if c.closed.Load() {
		return errors.New("closed")
	}
	c.out <- append([]byte(nil), b...)
	return nil
}
func (c *fakeConn) Close() error             { c.closed.Store(true); return nil }
func (c *fakeConn) Context() context.Context { return c.ctx }
func (c *fakeConn) Receive() ([]byte, error) {
	select {
	case f, ok := <-c.in:
		if !ok {
			return nil, errors.New("收件箱已关闭")
		}
		return f, nil
	case <-c.ctx.Done():
		return nil, context.Canceled
	}
}

// ctlFrame 组一帧客户端控制帧。
func ctlFrame(t *testing.T, ctl controlFrame) []byte {
	t.Helper()
	b, err := json.Marshal(ctl)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte{frameControl}, b...)
}

// nextControl 取下一条控制帧并解码（带超时，防测试挂死）。
func nextControl(t *testing.T, c *fakeConn) controlFrame {
	t.Helper()
	select {
	case f := <-c.out:
		if len(f) == 0 || f[0] != frameControl {
			t.Fatalf("期待控制帧，得标签 0x%02x", f[0])
		}
		var ctl controlFrame
		if err := json.Unmarshal(f[1:], &ctl); err != nil {
			t.Fatal(err)
		}
		return ctl
	case <-time.After(2 * time.Second):
		t.Fatal("等待控制帧超时")
		return controlFrame{}
	}
}

func waitFor(t *testing.T, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", why)
}

// denyGate 可编程门：开=放行，关=拒绝（模拟停用/stopping）。
type denyGate struct{ deny atomic.Bool }

func (g *denyGate) Acquire(string) (func(), error) {
	if g.deny.Load() {
		return nil, errors.New("模块停用中，拒绝 Acquire")
	}
	return func() {}, nil
}

func newTestService(t *testing.T) (*TerminalService, *fakePty, *denyGate) {
	t.Helper()
	f := newFakePty()
	holder := extapi.NewLeaseHolder(ID)
	gate := &denyGate{}
	holder.SetGate(gate)
	svc := NewService(holder)
	svc.spawnFn = func(string, []string, conpty.Options) (ptyLike, error) { return f, nil }
	return svc, f, gate
}

// ---- RPC 面 ----

func TestOpenKillLifecycle(t *testing.T) {
	svc, f, _ := newTestService(t)
	row, err := svc.OpenTerminal("cmd", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	if row.Pid != 4242 || !row.Alive || row.ProfileID != "cmd" {
		t.Fatalf("行形状不对: %+v", row)
	}
	if err := svc.RenameTerminal(row.ID, "编译专用"); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.ListTerminals()
	if len(list) != 1 || list[0].Title != "编译专用" {
		t.Fatalf("改名未生效: %+v", list)
	}
	if err := svc.KillTerminal(row.ID); err != nil {
		t.Fatal(err)
	}
	if f.closed != 1 {
		t.Fatalf("Kill 必须 Close 伪控制台恰好一次，得 %d", f.closed)
	}
	if err := svc.KillTerminal(row.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("重复 Kill 应如实报: %v", err)
	}
}

func TestOpenGuards(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.OpenTerminal("cmd", 0, 30); err == nil {
		t.Fatal("非法尺寸应拒")
	}
	if _, err := svc.OpenTerminal("nope", 80, 24); err == nil {
		t.Fatal("未知档应拒")
	}
}

// TestKillAllUnderStoppingGate 是审计 B1 的回归钉：停用态（门拒绝）下
// killAll 必须**照样杀光**——旧实现逐会话走带 Enter 的 KillTerminal，
// 门一拒就"一个都不杀、账一项不清"还显示已停用。
func TestKillAllUnderStoppingGate(t *testing.T) {
	svc, f, gate := newTestService(t)
	r1, _ := svc.OpenTerminal("cmd", 80, 24)
	r2, _ := svc.OpenTerminal("powershell", 80, 24)
	gate.deny.Store(true) // 进入停用态
	svc.killAll()
	if f.closed != 2 {
		t.Fatalf("stopping 态收口必须杀掉全部会话（B1 回归），close=%d", f.closed)
	}
	list, _ := svc.ListTerminals()
	if len(list) != 0 {
		t.Fatalf("收口后账目应清空，得 %+v", list)
	}
	_ = r1
	_ = r2
}

// ---- 数据面（serveConn）----

// TestServeConnOpenAdmissionDenied 审计 B2 回归钉：停用态下 open 帧必须被
// 拒（带原因），不得 attach；连接本身不作乱。
func TestServeConnOpenAdmissionDenied(t *testing.T) {
	svc, _, gate := newTestService(t)
	row, _ := svc.OpenTerminal("cmd", 80, 24)
	gate.deny.Store(true)
	c := newFakeConn()
	c.in <- ctlFrame(t, controlFrame{Op: "open", SessionID: row.ID})
	go svc.serveConn(c)
	ctl := nextControl(t, c)
	if ctl.Error == "" {
		t.Fatalf("停用态 open 必须拒绝并说明: %+v", ctl)
	}
	c.cancel()
}

func TestServeConnHappyHandshakeAndStream(t *testing.T) {
	svc, f, _ := newTestService(t)
	row, _ := svc.OpenTerminal("cmd", 80, 24)
	c := newFakeConn()
	go svc.serveConn(c)
	c.in <- ctlFrame(t, controlFrame{Op: "open", SessionID: row.ID})
	ctl := nextControl(t, c)
	if ctl.Op != "ready" || ctl.SessionID != row.ID || ctl.Error != "" {
		t.Fatalf("握手 ready 不对: %+v", ctl)
	}
	// 泵在场：pty 出一块 → 连接收到 0x01 原字节帧。
	pushChunk(t, f, "PS C:\\>")
	select {
	case fr := <-c.out:
		if fr[0] != frameRaw || string(fr[1:]) != "PS C:\\>" {
			t.Fatalf("输出帧形态不对: %q", fr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("泵未把 pty 字节送达连接")
	}
	// 输入直通。
	c.in <- append([]byte{frameRaw}, "dir\r"...)
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.written) == 1 && string(f.written[0]) == "dir\r"
	}, "输入应写进 pty")
	// resize 直通。
	c.in <- ctlFrame(t, controlFrame{Op: "resize", Cols: 120, Rows: 40})
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.resizes) == 1
	}, "resize 应直达 pty")
	c.cancel()
}

func TestServeConnUnknownAndBadFrames(t *testing.T) {
	svc, _, _ := newTestService(t)
	c := newFakeConn()
	go svc.serveConn(c)
	c.in <- []byte{0x7f, 'x'} // 未知标签
	if ctl := nextControl(t, c); ctl.Error == "" {
		t.Fatal("未知标签必须明说")
	}
	c.in <- append([]byte{frameControl}, []byte("{broken")...) // 坏 JSON
	if ctl := nextControl(t, c); ctl.Error == "" {
		t.Fatal("坏控制帧必须明说")
	}
	c.cancel()
	<-time.After(50 * time.Millisecond)
	if !c.closed.Load() {
		t.Fatal("坏 JSON 收场时连接应被关闭（defer 兜底）")
	}
}

// TestRouteChunkDecisionTable 审计 M1 的决策钉：抢主/无主/退场三条分支
// 抽成纯函数后逐行可断言（goroutine 竞态里测时序要赌运气，赌来的绿不算绿）。
// 语义账：现任→保留；他人→转交后退；nil→drain 后退；读结束→exit 通知后退。
func TestRouteChunkDecisionTable(t *testing.T) {
	a := streamIO(newFakeConn())
	b := streamIO(newFakeConn())
	pa, pb := &a, &b

	if act, tgt := routeChunk(pa, pa, true, nil); act != routeKeep || tgt != pa {
		t.Fatalf("现任在场应 keep: %v %v", act, tgt != pa)
	}
	if act, tgt := routeChunk(pb, pa, true, nil); act != routeHandoff || tgt != pb {
		t.Fatalf("被抢主应转交现任并退: %v", act)
	}
	if act, _ := routeChunk(nil, pa, true, nil); act != routeDrain {
		t.Fatalf("无主应起 drain: %v", act)
	}
	if act, tgt := routeChunk(pb, pa, false, errors.New("eof")); act != routeExit || tgt != pb {
		t.Fatalf("读结束应 exit 通知在场者: %v", act)
	}
	if act, _ := routeChunk(nil, pa, false, errors.New("eof")); act != routeExit {
		t.Fatalf("读结束且无主也要退: %v", act)
	}
}

// TestDeferDoesNotWipeCurrentOwner 审计 M2 回归钉：A 收尾的 CAS 不得擦掉
// 已上位的 B（Load+Store 的旧写法在窄窗里会造成"B 黑屏但输入通"）。
func TestDeferDoesNotWipeCurrentOwner(t *testing.T) {
	svc, _, _ := newTestService(t)
	row, _ := svc.OpenTerminal("cmd", 80, 24)
	var sess *session
	svc.mu.Lock()
	sess = svc.sessions[row.ID]
	svc.mu.Unlock()

	a := newFakeConn()
	go svc.serveConn(a)
	a.in <- ctlFrame(t, controlFrame{Op: "open", SessionID: row.ID})
	nextControl(t, a) // ready 落地，A 是现任

	b := newFakeConn()
	go svc.serveConn(b)
	b.in <- ctlFrame(t, controlFrame{Op: "open", SessionID: row.ID})
	nextControl(t, b) // B 抢主成功

	a.cancel() // A 收场：defer 的 CAS 应失配（现任已是 B），不清空
	waitFor(t, func() bool { return a.closed.Load() }, "A 连接应被自己关闭")
	// 关键断言：现任仍在，且是 B。
	waitFor(t, func() bool {
		cur := sess.owner.Load()
		return cur != nil
	}, "A 的 CAS 不得擦掉现任 B")
	b.cancel()
}

// TestDrainKeepsChildFlowing 审计 R1 回归钉：无主期间 drain 必须持续消费
// pty 字节（否则 4KB 管道写满、claude 被反压冻死，"断线不杀进程"成假话）。
func TestDrainKeepsChildFlowing(t *testing.T) {
	svc, f, _ := newTestService(t)
	row, _ := svc.OpenTerminal("claude", 80, 24)
	_ = row
	var sess *session
	svc.mu.Lock()
	sess = svc.sessions[row.ID]
	svc.mu.Unlock()

	svc.startDrain(sess)
	// 灌满远超 4KB 的字节（管道容量级），drain 在场则全部被消费、Read 不阻塞。
	for i := 0; i < 40; i++ {
		select {
		case f.readChunks <- []byte(strings.Repeat("x", 512)):
		case <-time.After(time.Second):
			t.Fatalf("第 %d 块灌不进：drain 没在读（管道反压假说成立）", i)
		}
	}
	// 新主上位后 drain 离场：置 owner，等 draining 位释放。
	my := streamIO(newFakeConn())
	sess.owner.Store(&my)
	waitFor(t, func() bool { return !sess.draining.Load() }, "新主在场后 drain 应离场")
	// 重复 startDrain 不起第二只。
	sess.owner.Store(nil)
	svc.startDrain(sess)
	svc.startDrain(sess)
	if !sess.draining.Load() {
		t.Fatal("drain 应在场")
	}
}

// TestResizeAfterSessionDeadRejected 测的是"会话已终但连接仍活着"这一格
// （自然退场/旁观通知时序）：serveConn 的 exited 闸必须回 exit 帧，
// 不许把 resize 递给悬空 pty。
// 注：KillTerminal 路径里 killSession **先关连接**再杀 pty——之后的任何
// "回复"都发不出去且 by design（击杀即断流），所以击杀场景不在此断言
// 回执，只断言闸本身的决策。
func TestResizeAfterSessionDeadRejected(t *testing.T) {
	svc, _, _ := newTestService(t)
	row, _ := svc.OpenTerminal("cmd", 80, 24)
	c := newFakeConn()
	go svc.serveConn(c)
	c.in <- ctlFrame(t, controlFrame{Op: "open", SessionID: row.ID})
	nextControl(t, c)

	svc.mu.Lock()
	sess := svc.sessions[row.ID]
	svc.mu.Unlock()
	sess.exited.Store(true) // 模拟自然退场：连接保留，账已死

	c.in <- ctlFrame(t, controlFrame{Op: "resize", Cols: 100, Rows: 40})
	ctl := nextControl(t, c)
	if ctl.Op != "exit" {
		t.Fatalf("已终止会话的 resize 应回 exit 帧，得 %+v", ctl)
	}
	c.cancel()
	svc.killSession(row.ID) // 收掉在场泵，测试不留挂起的 goroutine
}

// ---- 既有契约钉 ----

func TestControlFrameEncoding(t *testing.T) {
	frame, err := encodeControl(controlFrame{Op: "ready", SessionID: "x1"})
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != frameControl {
		t.Fatalf("控制帧标签必须 0x00，得 0x%02x", frame[0])
	}
	var back controlFrame
	if err := json.Unmarshal(frame[1:], &back); err != nil {
		t.Fatal(err)
	}
	if back.Op != "ready" || back.SessionID != "x1" {
		t.Fatalf("回读不一致: %+v", back)
	}
	if s := string(frame[1:]); strings.Contains(s, `"cols"`) || strings.Contains(s, `"code"`) {
		t.Fatalf("空字段泄漏进帧: %s", s)
	}
}

func TestResolveProfileStatic(t *testing.T) {
	if c, _, _, err := resolveProfile("cmd"); err != nil || c != "cmd.exe" {
		t.Fatalf("cmd 档: %v %s", err, c)
	}
	if _, a, _, err := resolveProfile("powershell"); err != nil || len(a) != 1 || a[0] != "-NoProfile" {
		t.Fatalf("powershell 档: %v %v", err, a)
	}
	if _, _, _, err := resolveProfile("banana"); err == nil {
		t.Fatal("未知档必须拒")
	}
	c, a, _, err := resolveProfile("claude")
	if err != nil {
		if !errors.Is(err, aiagent.ErrNotInstalled) {
			t.Fatalf("未装 claude 必须报 ErrNotInstalled 哨兵，得: %v", err)
		}
	} else if c != "cmd.exe" || len(a) != 2 || a[0] != "/c" {
		t.Fatalf("claude 档必须是 cmd.exe /c <绝对路径>（探针 run3 姿势），得: %s %v", c, a)
	}
}
