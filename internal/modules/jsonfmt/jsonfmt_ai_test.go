package jsonfmt

// 「AI 一键美化」的验收与状态机用例。假 Asker 注入（不装 CLI、不联网也能把
// 全部拒收分支钉死）；拒收样本全部取自真机事故形态（#126 裸词、#133 拒绝语
// 里编示例），不是编出来好看的形状。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
)

type fakeAsker struct {
	mu     sync.Mutex
	text   string
	err    error
	seen   []aiagent.Request
	block  chan struct{} // 非 nil：Ask 挂到 close 才返回（单飞用例）
	calls  int
	instrT string // 记录收到的 instruction，钉"提示词由后端定死"
}

func (f *fakeAsker) Ask(ctx context.Context, req aiagent.Request) (*aiagent.Result, error) {
	f.mu.Lock()
	f.calls++
	f.seen = append(f.seen, req)
	f.instrT = req.Instruction
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return &aiagent.Result{Text: f.text, BytesIn: len(req.Input), BytesOut: len(f.text), ElapsedMs: 4200}, nil
}

func newAITestService(t *testing.T, fa *fakeAsker) *JsonFmtService {
	t.Helper()
	svc := NewService(extapi.NewLeaseHolder("jsonfmt"))
	svc.newAsker = func() (Asker, error) { return fa, nil }
	return svc
}

func TestExtractByAIRerunsLocalPipeline(t *testing.T) {
	fa := &fakeAsker{text: `{"b": 1,"a":[1,2]}`}
	svc := newAITestService(t, fa)
	r, err := svc.ExtractByAI(JsonFmtRequest{Text: "前缀 noise 请参 {\"b\": 1,\"a\":[1,2]}", Indent: 2})
	if err != nil || !r.OK {
		t.Fatalf("应成功: %v %+v", err, r)
	}
	// 排版归本地渲染器：键序保留 + 两空格缩进，AI 原样吐的紧凑度不残留
	if !strings.Contains(r.Output, "\"b\": 1") || !strings.Contains(r.Output, "\n  ") {
		t.Fatalf("未按本地缩进渲染: %q", r.Output)
	}
	if r.Kind != "object" {
		t.Fatalf("单份返回 Kind 应是真实形状: %q", r.Kind)
	}
	w := strings.Join(r.Warnings, "｜")
	if !strings.Contains(w, "外发") || !strings.Contains(w, "耗时") {
		t.Fatalf("外发/返回字节与耗时必须上屏: %v", r.Warnings)
	}
	// 提示词由后端定死：请求里逐字带任务说明，输入原文完整外发
	if fa.instrT != extractInstruction || fa.seen[0].Input == "" {
		t.Fatal("指令或原文透传不对")
	}
}

// TestExtractByAIRejectsRefusalWithFakeExample #133 的事故原文：拒绝语里
// 夹着模型编造的示例对象——扫描式验收会把它当"提取成功 1 段"。
func TestExtractByAIRejectsRefusalWithFakeExample(t *testing.T) {
	fa := &fakeAsker{text: `很抱歉，这段文本里没有 JSON（形如 {"key":"value"} 的那样才算）。`}
	svc := newAITestService(t, fa)
	_, err := svc.ExtractByAI(JsonFmtRequest{Text: "垃圾"})
	if !errors.Is(err, ErrAINotJSON) {
		t.Fatalf("拒绝语必须拒收: %v", err)
	}
	if !strings.Contains(err.Error(), "完整的 JSON") {
		t.Fatalf("拒因要说明模型在解释不是返回: %v", err)
	}
}

func TestExtractByAINoJsonSentinel(t *testing.T) {
	fa := &fakeAsker{text: "NO_JSON"}
	svc := newAITestService(t, fa)
	if _, err := svc.ExtractByAI(JsonFmtRequest{Text: "x"}); !errors.Is(err, ErrAINotJSON) {
		t.Fatalf("NO_JSON 哨兵必须拦下: %v", err)
	}
}

func TestExtractByAIRejectsEmptyContainers(t *testing.T) {
	for _, canned := range []string{"{}", "[]", `[31735]`, `"一句话"`} {
		fa := &fakeAsker{text: canned}
		svc := newAITestService(t, fa)
		if _, err := svc.ExtractByAI(JsonFmtRequest{Text: "x"}); !errors.Is(err, ErrAINotJSON) {
			t.Fatalf("%s 不算成果必须拒: %v", canned, err)
		}
	}
}

func TestExtractByAIAcceptsContractShapes(t *testing.T) {
	cases := []struct {
		name, canned string
		wantKind     string
		wantSegs     int
	}{
		{"两份空行分隔", "{\"a\":1}\n\n{\"b\":2}", "multi", 2},
		{"pretty 自带空行", "{\n  \"x\": [\n    1\n  ]\n}", "object", 1},
		{"干净围栏", "```json\n{\"a\":1}\n```", "object", 1},
		{"JSON 后附解释（点名丢弃）", "{\"a\":1}\n\n以上是从日志里提取的配置。", "object", 1},
	}
	for _, c := range cases {
		fa := &fakeAsker{text: c.canned}
		svc := newAITestService(t, fa)
		r, err := svc.ExtractByAI(JsonFmtRequest{Text: "x", Indent: 2})
		if err != nil || !r.OK {
			t.Fatalf("%s 应成功: %v", c.name, err)
		}
		if r.Kind != c.wantKind {
			t.Fatalf("%s Kind=%q want %q", c.name, r.Kind, c.wantKind)
		}
		if n := strings.Count(r.Output, "\n\n") + 1; n != c.wantSegs && c.wantSegs > 1 {
			t.Fatalf("%s 段数不对: %q", c.name, r.Output)
		}
		if strings.Contains(c.name, "附解释") && !strings.Contains(strings.Join(r.Warnings, "｜"), "丢弃") {
			t.Fatalf("丢弃必须点名: %v", r.Warnings)
		}
	}
}

func TestExtractByAISingleFlight(t *testing.T) {
	fa := &fakeAsker{text: `{"a":1}`, block: make(chan struct{})}
	svc := newAITestService(t, fa)
	first := make(chan error, 1)
	go func() { _, err := svc.ExtractByAI(JsonFmtRequest{Text: "x"}); first <- err }()
	waitForFalse(t, func() bool { return fa.calls == 0 }, "第一发进入")
	if _, err := svc.ExtractByAI(JsonFmtRequest{Text: "y"}); err == nil {
		t.Fatal("在途期间第二发必须当面拒")
	}
	close(fa.block)
	if err := <-first; err != nil {
		t.Fatalf("第一发不该被连坐: %v", err)
	}
	// 闸随收口放开
	if _, err := svc.ExtractByAI(JsonFmtRequest{Text: "z"}); err != nil {
		t.Fatalf("收口后应可再发: %v", err)
	}
}

func TestExtractByAIGuards(t *testing.T) {
	fa := &fakeAsker{text: `{"a":1}`}
	svc := newAITestService(t, fa)
	if _, err := svc.ExtractByAI(JsonFmtRequest{Text: "  "}); err == nil {
		t.Fatal("空输入拒")
	}
	if _, err := svc.ExtractByAI(JsonFmtRequest{Text: strings.Repeat("x", MaxInputBytes+1)}); err == nil {
		t.Fatal("超限在发出前拒")
	}
	if fa.calls != 0 {
		t.Fatalf("守卫路径不得外发: calls=%d", fa.calls)
	}
	// 环境错误原样上抛（未装与"返回不是 JSON"分列——调用方才有可执行下一步）
	svc2 := NewService(extapi.NewLeaseHolder("jsonfmt"))
	svc2.newAsker = func() (Asker, error) { return nil, aiagent.ErrNotInstalled }
	if _, err := svc2.ExtractByAI(JsonFmtRequest{Text: "ok"}); !errors.Is(err, aiagent.ErrNotInstalled) {
		t.Fatalf("未装哨兵必须原样上抛: %v", err)
	}
}

func waitForFalse(t *testing.T, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待失败: %s", why)
}
