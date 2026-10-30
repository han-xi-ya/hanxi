package aichat

// 聊天服务状态机的单测：不装 CLI、不起进程也能把"懒起、单飞、复进、回收、
// 拆账"钉死。假会话实现 agentStream，事件面经 s.emitEvent 捕获器断言——
// 输出面不可断言的状态机等于没测（"断言抄实现等于没测"的同族教训）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
)

type fakeStream struct {
	events    chan aiagent.Event
	sent      []string
	mu        sync.Mutex
	closeOnce sync.Once
	closeErr  error
	closed    int
}

func (f *fakeStream) Send(text string) (string, error) {
	f.mu.Lock()
	f.sent = append(f.sent, text)
	f.mu.Unlock()
	return aiagent.NewTurnID(), nil
}
func (f *fakeStream) Events() <-chan aiagent.Event { return f.events }
func (f *fakeStream) Info() aiagent.SessionInfo    { return aiagent.SessionInfo{Model: "fake"} }
func (f *fakeStream) StderrNote() string           { return "stderr note" }
func (f *fakeStream) DroppedFrames() int64         { return 0 }
func (f *fakeStream) Close() error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	f.closeOnce.Do(func() { close(f.events) }) // 幂等：真 Close 可被 teardown 双路触发（显式 + 泵收口）
	return f.closeErr
}

type capture struct {
	mu    sync.Mutex
	names []string
	items []any
}

func (c *capture) fn(name string, payload any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names = append(c.names, name)
	c.items = append(c.items, payload)
}

// waitFor 轮询等待条件成立（事件是异步泵，不许 sleep 猜时长）。
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

func newTestService(t *testing.T) (*Service, *fakeStream, *capture, *[]aiagent.SessionOptions) {
	t.Helper()
	holder := extapi.NewLeaseHolder(ID) // 未注入门 = 纯内存判定（extapi 三档口径），单测直过
	svc, err := NewService(holder, filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.idleReap = 50 * time.Millisecond // 回收窗口缩到可测
	cap := &capture{}
	svc.emitEvent = cap.fn
	var optsMu sync.Mutex
	var dialed []aiagent.SessionOptions
	f := &fakeStream{events: make(chan aiagent.Event, 16)}
	svc.newStream = func(_ context.Context, o aiagent.SessionOptions) (agentStream, error) {
		optsMu.Lock()
		dialed = append(dialed, o)
		optsMu.Unlock()
		return f, nil
	}
	return svc, f, cap, &dialed
}

// TestLazyDialFirstSendUsesSessionID 钉两条实测分界：建会话**不起进程**、
// 首聊用 --session-id（探测 E2 形态），复进才用 --resume（E4 形态）。
func TestLazyDialFirstSendUsesSessionID(t *testing.T) {
	svc, f, _, dialed := newTestService(t)
	row, err := svc.CreateSession("")
	if err != nil {
		t.Fatal(err)
	}
	if len(*dialed) != 0 {
		t.Fatal("CreateSession 不得起进程（按下才外发的同一条纪律）")
	}
	if _, err := svc.SendMessage(row.ID, "你好"); err != nil {
		t.Fatal(err)
	}
	if len(*dialed) != 1 || (*dialed)[0].Resume {
		t.Fatalf("首聊应恰一次 Dial 且 Resume=false（--session-id 实测形态），得 %+v", *dialed)
	}
	if (*dialed)[0].SessionID != row.ID || (*dialed)[0].WorkDir == "" {
		t.Fatalf("会话身份/固定工作目录必须透传: %+v", (*dialed)[0])
	}
	// 首条消息升格标题（本地截取，不外发）
	rows, _ := svc.ListSessions()
	if rows[0].Title != "你好" {
		t.Fatalf("标题应从首条消息本地升格，得 %q", rows[0].Title)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) != 1 || f.sent[0] != "你好" {
		t.Fatalf("文本应原样进 Send: %v", f.sent)
	}
}

func TestSendMessageGuards(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	if _, err := svc.SendMessage("no-such-id", "hi"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("未知会话应拒: %v", err)
	}
	row, _ := svc.CreateSession("")
	if _, err := svc.SendMessage(row.ID, "   "); err == nil {
		t.Fatal("空白消息应拒")
	}
	if _, err := svc.SendMessage(row.ID, string(make([]byte, maxMessageBytes+1))); err == nil {
		t.Fatal("超限消息应拒（聊天窗不是日志投喂口）")
	}
}

// TestSingleFlightPerSession 实测协议里 EventText 不带回合标记，归属靠到达序——
// 于是"同会话一轮在途拒绝第二发"是**协议事实**而非 UI 自觉，必须服务级 CAS 拦。
func TestSingleFlightPerSession(t *testing.T) {
	svc, f, _, _ := newTestService(t)
	row, _ := svc.CreateSession("")
	if _, err := svc.SendMessage(row.ID, "第一发"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage(row.ID, "第二发"); err == nil {
		t.Fatal("在途期间第二发必须当面拒，不许排队猜归属")
	}
	f.events <- aiagent.Event{Kind: aiagent.EventTurnDone, Result: &aiagent.TurnResult{Text: "答", DurationMs: 1234}}
	waitFor(t, func() bool {
		_, err := svc.SendMessage(row.ID, "收口后的第三发")
		if err != nil {
			return false
		}
		svc.teardown(row.ID, "")
		return true
	}, "result 后 busy 应释放，第三发可进")
}

// TestTextEventsAndStatsEmit 事件翻译面：块快照成 timeline、result 成 stats，
// 且**文本不双发**（result.text 与块一致是实测事实，两处都上屏即重复渲染）。
func TestTextEventsAndStatsEmit(t *testing.T) {
	svc, f, cap, _ := newTestService(t)
	row, _ := svc.CreateSession("")
	res, err := svc.SendMessage(row.ID, "问")
	if err != nil {
		t.Fatal(err)
	}
	f.events <- aiagent.Event{Kind: aiagent.EventText, Text: "答出一半"}
	f.events <- aiagent.Event{Kind: aiagent.EventTurnDone, Result: &aiagent.TurnResult{
		TurnUUID: res.TurnID, Text: "答出一半", DurationMs: 2513, TTFTMs: 1667, TotalCostUSD: 0.12763}}
	waitFor(t, func() bool {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		return len(cap.items) >= 3
	}, "timeline+stats+idle 三连事件")

	var timeline TimelineItem
	var stats TurnStats
	nTimeline, nStats := 0, 0
	for _, it := range cap.items {
		switch v := it.(type) {
		case TimelineItem:
			nTimeline++
			timeline = v
		case TurnStats:
			nStats++
			stats = v
		}
	}
	if nTimeline != 1 || nStats != 1 {
		t.Fatalf("双发防线破了: timeline=%d stats=%d", nTimeline, nStats)
	}
	if timeline.Text != "答出一半" || timeline.TurnID != res.TurnID || timeline.Kind != "assistant" {
		t.Fatalf("timeline 事件形状不对: %+v", timeline)
	}
	if stats.ElapsedMs != 2513 || stats.TTFTMs != 1667 || stats.TotalCostUSD != 0.12763 || stats.TurnID != res.TurnID {
		t.Fatalf("result 统计必须原样上交: %+v", stats)
	}
}

// TestDeadTriggersTeardownAndResumeNext 崩溃/暴死 → 拆 runtime（杀进程、放租约）、
// 报 dead 事件带 stderr 尾巴；下一条消息**自动复进且 Resume=true**（E4 实测形态）。
func TestDeadTriggersTeardownAndResumeNext(t *testing.T) {
	svc, f, cap, dialed := newTestService(t)
	row, _ := svc.CreateSession("")
	if _, err := svc.SendMessage(row.ID, "第一聊"); err != nil {
		t.Fatal(err)
	}
	f.events <- aiagent.Event{Kind: aiagent.EventDead, Err: errors.New("exit status 1")}
	waitFor(t, func() bool {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		for i, n := range cap.names {
			if n == "aichat:session-state" {
				if st, ok := cap.items[i].(SessionState); ok && st.State == "dead" {
					return true
				}
			}
		}
		return false
	}, "dead 事件")
	if f.closed != 1 {
		t.Fatalf("dead 收口应 Close 恰好一次，得 %d", f.closed)
	}
	if _, err := svc.SendMessage(row.ID, "复进发"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(*dialed) == 2 }, "第二次 Dial")
	if !(*dialed)[1].Resume {
		t.Fatalf("复进必须 --resume（started 账），得 %+v", (*dialed)[1])
	}
}

// TestIdleReapThenResurrect 回收窗口到点杀进程；账本还在、started 还在，
// 下一条自动复进——"空闲回收"与"会话可复进"两件事的接线验证。
func TestIdleReapThenResurrect(t *testing.T) {
	svc, f, _, dialed := newTestService(t)
	row, _ := svc.CreateSession("")
	if _, err := svc.SendMessage(row.ID, "触发回收计时"); err != nil {
		t.Fatal(err)
	}
	f.events <- aiagent.Event{Kind: aiagent.EventTurnDone, Result: &aiagent.TurnResult{Text: "答"}}
	waitFor(t, func() bool { return f.closed == 1 }, "空闲回收应 Close")
	if rows, _ := svc.ListSessions(); len(rows) != 1 {
		t.Fatal("回收不除账")
	}
	if _, err := svc.SendMessage(row.ID, "回收后再来"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(*dialed) == 2 }, "复活 Dial")
	if !(*dialed)[1].Resume {
		t.Fatalf("复活必须 Resume: %+v", (*dialed)[1])
	}
}

func TestCloseSessionArchivesAndDeleteKeepsTranscript(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	row, _ := svc.CreateSession("工单助手")
	if err := svc.CloseSession(row.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ := svc.ListSessions()
	if len(rows) != 1 || !rows[0].Archived {
		t.Fatalf("Close 只封存不除名: %+v", rows)
	}
	if err := svc.RenameSession(row.ID, "改名解封"); err != nil {
		t.Fatal(err)
	}
	rows, _ = svc.ListSessions()
	if rows[0].Archived {
		t.Fatal("改名=还要用，应顺手解除封存")
	}
	// Delete 不越界删 CLI 的 transcript：在 WorkDir 放一个哨兵文件验证。
	wd := svc.workDir
	sentinel := filepath.Join(wd, "transcript-sentinel.jsonl")
	if err := os.WriteFile(sentinel, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSession(row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("DeleteSession 越界删了 claude 数据根的文件: %v", err)
	}
	if rows, _ := svc.ListSessions(); len(rows) != 0 {
		t.Fatal("Delete 应从账本除名")
	}
	if err := svc.DeleteSession("nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("删不存在应如实报: %v", err)
	}
}

func TestStoreSurvivesRestartAndRefusesCorrupt(t *testing.T) {
	dir := t.TempDir()
	holder := extapi.NewLeaseHolder(ID)
	s1, err := NewService(holder, dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := s1.CreateSession("重启后还要在")
	// 重开一份 service（模拟 app 重启）
	s2, err := NewService(extapi.NewLeaseHolder(ID), dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := s2.ListSessions()
	if len(rows) != 1 || rows[0].ID != row.ID || rows[0].Title != "重启后还要在" {
		t.Fatalf("账本未跨重启存活: %+v", rows)
	}
	// 损坏账本：报错且不清空（自动清空 = 二次伤害）
	p := filepath.Join(dir, "aichat", "aichat-sessions.json")
	if err := os.WriteFile(p, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(extapi.NewLeaseHolder(ID), dir, dir); err == nil {
		t.Fatal("坏账本必须拒")
	}
	orig, _ := os.ReadFile(p)
	if json.Valid(orig) && len(orig) < 5 {
		t.Fatal("坏账本被悄悄改写")
	}
	if string(orig) != "{broken" {
		t.Fatalf("拒收时不得动用户文件，原样保真: %q", orig)
	}
}

func TestAbbrevTitle(t *testing.T) {
	long := fmt.Sprintf("%s尾巴", string([]rune("一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十")))
	got := abbrevTitle(long)
	if len([]rune(got)) > 25 || !strings.HasSuffix(got, "…") {
		t.Fatalf("长标题应截到 24 字加省略号: %q", got)
	}
	if abbrevTitle("短  句") != "短 句" {
		t.Fatal("连续空白应折叠")
	}
}
