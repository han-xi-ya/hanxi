package aiagent

// 常驻会话的单测**不需要真装 CLI**：帧解析、序列化、回合配对、暴死语义全部
// 走 io.Pipe 构造——沿用 agent.go「Runner 可注入」的同一条理由（CI 无 CLI、
// 无网络、无登录态）。协议帧的形状不抄实现、抄外部事实：样本取自 2026-10-30
// 真机探测原始日志（C:/tmp/aichat-probe/logs/frames-session-A.log 的截样），
// 字段名与嵌套以 CLI 实际吐的为准。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// newPipeSession 造一个不经进程的真实 Session 骨架：stdin 端可回读、
// stdout 端可喂帧、waitCh 可控退出码。
func newPipeSession() (*Session, *io.PipeWriter, chan []byte, chan error) {
	stdinR, stdinW := io.Pipe()
	outR, outW := io.Pipe()
	readCh := make(chan []byte, 4096)
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := stdinR.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				readCh <- cp
			}
			if err != nil {
				close(readCh)
				return
			}
		}
	}()
	s := &Session{
		w:      stdinW,
		events: make(chan Event, eventsQueue),
		stderr: &boundedBuffer{limit: stderrCapBytes},
	}
	seed := &SessionInfo{SessionID: "seed-id"}
	s.info.Store(seed)
	waitCh := make(chan error, 1)
	go s.readLoop(outR, waitCh)
	return s, outW, readCh, waitCh
}

// TestStreamSendSerializesWholeFrame 钉"手拼禁令"的可验证形态：
// 文本里塞换行、引号、乃至一整帧仿冒 JSON，写出的仍必须是**恰好一行合法 JSON**，
// 且原样解回来是原文。这条用例的假想敌就是实测的 38ms 暴死路径。
func TestStreamSendSerializesWholeFrame(t *testing.T) {
	s, outW, readCh, _ := newPipeSession()
	defer outW.Close()
	nasty := "第一行\n第二行带\"引号\"和仿冒帧 {\"type\":\"banana\"} 结束"
	uuid, err := s.Send(nasty)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if uuid == "" {
		t.Fatal("Send 必须返回回合 uuid（result.user_message_uuid 的对账键）")
	}
	// 攒到换行出现（一次写出可能裂成多个 Read 块——对账按内容不按块数）
	var acc []byte
	for chunk := range readCh {
		acc = append(acc, chunk...)
		if len(acc) > 0 && acc[len(acc)-1] == '\n' {
			break
		}
	}
	line := strings.TrimSuffix(string(acc), "\n")
	if strings.Contains(line[:len(line)-1], "\n") {
		t.Fatalf("写出必须是单行，实际含裸换行: %q", line)
	}
	var back userFrame
	if err := json.Unmarshal([]byte(line), &back); err != nil {
		t.Fatalf("写出的帧不是合法 JSON: %v", err)
	}
	if back.Type != "user" || back.Message.Role != "user" || back.UUID != uuid {
		t.Fatalf("帧头字段不对: %+v", back)
	}
	if got := back.Message.Content[0].Text; got != nasty {
		t.Fatalf("文本被转义改写: got %q want %q", got, nasty)
	}
}

func TestStreamSendGuards(t *testing.T) {
	s, outW, _, _ := newPipeSession()
	defer outW.Close()
	if _, err := s.Send("   "); !errors.Is(err, ErrNoInput) {
		t.Fatalf("空输入应拒: %v", err)
	}
	s.dead.Store(true)
	if _, err := s.Send("x"); !errors.Is(err, ErrSessionDead) {
		t.Fatalf("死会话应拒: %v", err)
	}
}

// 真机帧样本（截样自 frames-session-A.log，字段形状原样保留）。
const (
	sampleRunning   = `{"type":"system","subtype":"session_state_changed","state":"running","session_id":"e088d57a-1111-4222-8333-444455556666"}`
	sampleInit      = `{"type":"system","subtype":"init","session_id":"e088d57a-1111-4222-8333-444455556666","model":"qwen3.8-flash[1m]","claude_code_version":"2.1.283","permissionMode":"default","tools":["Task","AskUserQuestion"]}`
	sampleThink     = `{"type":"system","subtype":"thinking_tokens","estimated_tokens":14,"session_id":"e088d57a-1111-4222-8333-444455556666"}`
	sampleAsstTxt   = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"甲"}]},"session_id":"e088d57a-1111-4222-8333-444455556666"}`
	sampleAsstThink = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"用户要单字"}]},"session_id":"e088d57a-1111-4222-8333-444455556666"}`
	sampleResult    = `{"subtype":"success","is_error":false,"result":"甲","session_id":"e088d57a-1111-4222-8333-444455556666","user_message_uuid":"9c4a1b2e-aaaa-4bbb-8ccc-ddddeeeeffff","duration_ms":2513,"ttft_ms":2471,"total_cost_usd":0.12763,"usage":{"input_tokens":6,"output_tokens":44},"type":"result","num_turns":1}`
	sampleComplete  = `{"type":"system","subtype":"command_lifecycle","state":"completed"}`
	sampleIdle      = `{"type":"system","subtype":"session_state_changed","state":"idle","session_id":"e088d57a-1111-4222-8333-444455556666"}`
)

// sampleHook 只能做 var：Repeat 不是常量表达式。它代表实测 17KB+ 的
// SessionStart 插件喷帧——存在的意义就是证明 4MB 行缓冲这条路走得通。
var sampleHook = `{"type":"system","subtype":"hook_response","output":"# Vercel Plugin Session Context ...` +
	strings.Repeat("x", 900) + `","exit_code":0,"outcome":"success"}`

// TestStreamReadLoopEvents 一回合的完整帧序（实测 E2 原样）：
// running→hook→init→thinking噪音→assistant(思考块+文本块)→result→completed→idle。
// 断言的是**外部事实**：只有 text 块成 EventText、result 是唯一终态、
// 尾帧不产生事件、hook 巨帧不炸缓冲、init 被镜像进 Info。
func TestStreamReadLoopEvents(t *testing.T) {
	s, outW, _, waitCh := newPipeSession()
	lines := []string{sampleRunning, sampleHook, sampleInit, sampleThink, sampleThink,
		sampleAsstThink, sampleAsstTxt, sampleResult, sampleComplete, sampleIdle}
	for _, l := range lines {
		fmt.Fprintln(outW, l)
	}
	waitCh <- nil
	outW.Close()

	var texts []string
	var done *Event
	deadline := time.After(2 * time.Second)
	for ev := range s.Events() {
		switch ev.Kind {
		case EventText:
			texts = append(texts, ev.Text)
		case EventTurnDone:
			cp := ev
			done = &cp
		case EventDead:
			if ev.Err != nil {
				t.Fatalf("体面退出应带 nil 死因，得到: %v", ev.Err)
			}
		}
		_ = deadline // 通道由 readLoop 关闭，正常不会挂
	}
	if len(texts) != 1 || texts[0] != "甲" {
		t.Fatalf("EventText 应恰为 1 条文本块（thinking 块不算），得到 %q", texts)
	}
	if done == nil {
		t.Fatal("result 帧必须产出 EventTurnDone")
	}
	r := done.Result
	if r.TurnUUID != "9c4a1b2e-aaaa-4bbb-8ccc-ddddeeeeffff" || r.Text != "甲" ||
		r.DurationMs != 2513 || r.TTFTMs != 2471 || r.InputTokens != 6 || r.OutputTokens != 44 ||
		r.TotalCostUSD != 0.12763 || r.IsError {
		t.Fatalf("result 统计字段对不上原始帧: %+v", r)
	}
	if s.DroppedFrames() != 0 {
		t.Fatalf("已识别类型的帧不应计为 dropped，得到 %d", s.DroppedFrames())
	}
	if info := s.Info(); info.Model != "qwen3.8-flash[1m]" || info.Version != "2.1.283" {
		t.Fatalf("init 帧应被镜像进 Info: %+v", info)
	}
}

// TestStreamErrorResult 实测 result 有 is_error 形态（subtype 也非 success）：
// 必须原样上交 TurnResult，由消费方决定怎么显示——内核不把它伪装成成功。
func TestStreamErrorResult(t *testing.T) {
	s, outW, _, waitCh := newPipeSession()
	fmt.Fprintln(outW, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"","session_id":"s","duration_ms":9}`)
	waitCh <- errors.New("exit status 1")
	outW.Close()
	var done *TurnResult
	for ev := range s.Events() {
		if ev.Kind == EventTurnDone {
			done = ev.Result
		}
	}
	if done == nil || !done.IsError || done.Subtype != "error_during_execution" {
		t.Fatalf("错误 result 应原样上交: %+v", done)
	}
}

func TestStreamUnknownLinesCounted(t *testing.T) {
	s, outW, _, waitCh := newPipeSession()
	fmt.Fprintln(outW, `{"type":"banana"}`)
	fmt.Fprintln(outW, `not json at all`)
	waitCh <- nil
	outW.Close()
	for range s.Events() {
	}
	if s.DroppedFrames() != 2 {
		t.Fatalf("未知帧应计数不静默，得到 %d", s.DroppedFrames())
	}
}

func TestStreamDeadCarriesExitError(t *testing.T) {
	s, outW, _, waitCh := newPipeSession()
	boom := errors.New("exit status 1")
	waitCh <- boom
	outW.Close()
	var deadErr error
	for ev := range s.Events() {
		if ev.Kind == EventDead {
			deadErr = ev.Err
		}
	}
	if !errors.Is(deadErr, boom) {
		t.Fatalf("进程退出码必须经 EventDead 如实转交，得到 %v", deadErr)
	}
}

func TestStreamArgsShape(t *testing.T) {
	got := strings.Join(streamArgs(SessionOptions{SessionID: "sid", WorkDir: "wd"}), " ")
	for _, want := range []string{"--input-format stream-json", "--output-format stream-json",
		"--verbose", "--permission-mode default", "--disallowedTools Bash Edit Write MultiEdit NotebookEdit WebFetch",
		"--session-id sid"} {
		if !strings.Contains(got, want) {
			t.Fatalf("argv 缺实测组合 %q，完整: %s", want, got)
		}
	}
	resumed := strings.Join(streamArgs(SessionOptions{SessionID: "sid", WorkDir: "wd", Resume: true}), " ")
	if !strings.Contains(resumed, "--resume sid") || strings.Contains(resumed, "--session-id") {
		t.Fatalf("续聊必须走 --resume 不带 --session-id: %s", resumed)
	}
}

func TestDialValidatesBeforeSpawn(t *testing.T) {
	// 参数错误必须在 LookPath/spawn **之前**拦下：这些用例不依赖机器装没装 CLI。
	if _, err := Dial(t.Context(), SessionOptions{WorkDir: "wd"}); err == nil ||
		!strings.Contains(err.Error(), "SessionID") {
		t.Fatalf("空 SessionID 应被源头拒绝: %v", err)
	}
	if _, err := Dial(t.Context(), SessionOptions{SessionID: NewSessionID()}); err == nil ||
		!strings.Contains(err.Error(), "WorkDir") {
		// 实测坑 5：会话档案按 cwd 派生目录归档，WorkDir 不固定 = resume 随缘。
		t.Fatalf("空 WorkDir 应被源头拒绝: %v", err)
	}
}

func TestNewSessionIDShape(t *testing.T) {
	id := NewSessionID()
	// 8-4-4-4-12、version 位=4、variant 位=10xx（实测 --session-id 吃 uuid4）
	if len(id) != 36 || id[8] != '-' || id[14] != '4' || !strings.Contains("89ab", string(id[19])) {
		t.Fatalf("uuid4 形状不对: %q", id)
	}
	if NewSessionID() == id {
		t.Fatal("两次生成不得相同")
	}
}

// TestLiveStreamSession 真机夹具（默认 skip，机主一条命令可跑）：
// 常驻进程**同进程连发两轮**是探测 E3 的结论，这里把它钉成回归——
// 若某天 CLI 升级把多轮常驻打回"一条消息一进程"，本用例先红。
func TestLiveStreamSession(t *testing.T) {
	if os.Getenv("HANXI_AI_LIVE") == "" {
		t.Skip("未设 HANXI_AI_LIVE=1：跳过真机常驻会话")
	}
	dir := os.Getenv("HANXI_AI_LIVE_DIR")
	if dir == "" {
		t.Skip("真机常驻会话需设 HANXI_AI_LIVE_DIR（固定工作目录，resume 依赖它）")
	}
	sid := NewSessionID()
	s, err := Dial(t.Context(), SessionOptions{SessionID: sid, WorkDir: dir})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer s.Close()

	ask := func(text string) *TurnResult {
		t.Helper()
		uuid, err := s.Send(text)
		if err != nil {
			t.Fatalf("Send(%q): %v", text, err)
		}
		to := time.NewTimer(2 * time.Minute) // 常驻实测单轮 2.5~8s，给足模型长答余量
		defer to.Stop()
		for {
			select {
			case ev, ok := <-s.Events():
				if !ok {
					t.Fatalf("会话提前死亡，死因见 StderrNote：%s", s.StderrNote())
				}
				if ev.Kind == EventTurnDone {
					if ev.Result.TurnUUID != uuid {
						t.Fatalf("回合对账键不符: got %q want %q", ev.Result.TurnUUID, uuid)
					}
					return ev.Result
				}
			case <-to.C:
				t.Fatal("真机回合超时（2 分钟）")
			}
		}
	}

	first := ask("只回一个字：甲")
	if !strings.Contains(first.Text, "甲") {
		t.Fatalf("第一轮返回不含约定字: %q", first.Text)
	}
	second := ask("我上一条让你回什么字？只答那个字。")
	if !strings.Contains(second.Text, "甲") {
		t.Fatalf("同进程第二轮不记得第一轮（多轮常驻回归被破）: %q", second.Text)
	}
	if s.Info().SessionID != sid {
		t.Fatalf("init 回报的 session_id(%s) 应与我方起名(%s)一致", s.Info().SessionID, sid)
	}
	t.Logf("真机常驻两轮通过：%dms / %dms cost=%.4f", first.DurationMs, second.DurationMs, second.TotalCostUSD)
}
