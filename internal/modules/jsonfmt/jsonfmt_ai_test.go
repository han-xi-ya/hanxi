package jsonfmt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
)

// AI 提取路径的用例。全部用注入的假 Asker：不能让这些分支依赖"真装了 CLI +
// 真有登录态 + 真联网"才能跑（CI 必飘，本地也会莫名烧 token）。

type fakeAsker struct {
	calls int
	got   aiagent.Request
	out   *aiagent.Result
	err   error
	// onAsk 把收到的 ctx 交回给用例看：租约是不是可取消的，只有在这里才验得了
	onAsk func(ctx context.Context)
}

func (f *fakeAsker) Ask(ctx context.Context, r aiagent.Request) (*aiagent.Result, error) {
	f.calls++
	f.got = r
	if f.onAsk != nil {
		f.onAsk(ctx)
	}
	// 按真 Ask 的口径回填 BytesIn：它记的是**送出去多少**（len(req.Input)），不是返回文本
	// 长度。此前 aiOut() 把 BytesIn 设成返回文本，于是"外发 N 字节"这条断言核的是假数据
	// ——等于只检查了 Sprintf 的格式串有没有写错，用户真正关心的那个数字没人管。
	if f.out != nil {
		f.out.BytesIn = len(r.Input)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func svcWithAI(a Asker) *JsonFmtService {
	return &JsonFmtService{
		holder:   extapi.NewLeaseHolder(ID),
		newAsker: func() (Asker, error) { return a, nil },
	}
}

func aiOut(text string) *aiagent.Result {
	return &aiagent.Result{Text: text, BytesIn: len(text), BytesOut: len(text), ElapsedMs: 1200}
}

// TestExtractByAIRerunsLocalPipeline AI 的返回必须再过一遍本地解析与格式化：
// 模型只负责"从脏文本里认出结构"，合法与否由解析器裁决。
func TestExtractByAIRerunsLocalPipeline(t *testing.T) {
	dirty := "Oct 28 10:56:34 host proc[1]: SomeNoise\nrandom text\n  big_id: 9007199254740993 ???"
	fa := &fakeAsker{out: aiOut(`{"big_id": 9007199254740993, "ok": true}`)}

	r, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: dirty, Indent: 2})
	if err != nil {
		t.Fatalf("应成功: %v", err)
	}
	if !r.OK || !strings.Contains(r.Output, "9007199254740993") {
		t.Fatalf("输出不对: %+v", r)
	}
	// 19 位 ID 经过本地解析器仍逐字保留（AI 抄错的话这里就露出来）
	if strings.Contains(r.Output, "9.007") || strings.Contains(r.Output, "9007199254740992") {
		t.Errorf("数字被改写: %s", r.Output)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "由本机 AI 提取") {
		t.Errorf("必须声明这次走了 AI 并给出外发量: %+v", r.Warnings)
	}
	// 断言**具体数字**等于真送出去的字节数，而不是只看见"外发"两个字：
	// 用户要靠这个数知道自己交出去了多大一份内容，数字错了比没有更坏。
	want := fmt.Sprintf("外发 %d 字节", len(dirty))
	if !strings.Contains(strings.Join(r.Warnings, "|"), want) {
		t.Errorf("警告里应有 %q（实际外发量），实得 %+v", want, r.Warnings)
	}
}

// TestExtractByAIRejectsNonJSON AI 答非所问时必须拒绝渲染，而不是"看起来成功了"。
//
// 每条都写死期望（"成功或失败都算过"的断言等于没断言——上一版对围栏那条就是双向放行，
// 结果好坏都绿）。围栏内干净的一份 JSON 该收，已在 AcceptsContractShapes 里单列。
func TestExtractByAIRejectsNonJSON(t *testing.T) {
	cases := []string{
		"NO_JSON",
		"抱歉，我在文本里没有看到 JSON。",
		"```json\n{\"a\":1}\n``` 以上是提取结果", // 围栏干净但尾巴上挂了句解释，且没空行分块
		"我找到了两个对象：第一个是 a，第二个是 b。",
	}
	for _, in := range cases {
		fa := &fakeAsker{out: aiOut(in)}
		_, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x", Indent: 2})
		if err == nil {
			t.Errorf("%q 不该被当成成功结果", in)
			continue
		}
		if !errors.Is(err, ErrAINotJSON) {
			t.Errorf("应归为 ErrAINotJSON，实得 %v", err)
		}
	}
}

// TestExtractByAISendsRawTextAndBackendOwnedInstruction 两件事一起锁：
// 原文整段交给模型（不预先截断），任务说明由后端常量决定（前端传不进提示）。
func TestExtractByAISendsRawTextAndBackendOwnedInstruction(t *testing.T) {
	dirty := strings.Repeat("noise ", 200) + `{"a":1}`
	fa := &fakeAsker{out: aiOut(`{"a":1}`)}
	if _, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: dirty, Indent: 0}); err != nil {
		t.Fatal(err)
	}
	if fa.got.Input != dirty {
		t.Error("原文要完整交给模型，不得先行截断")
	}
	for _, want := range []string{"只输出 JSON", "数字逐字符照抄", "不要解释"} {
		if !strings.Contains(fa.got.Instruction, want) {
			t.Errorf("任务说明应含 %q（这些约束是后端契约，不该由前端传）", want)
		}
	}
}

// TestExtractByAISurfacesEnvError 环境问题（没装 CLI、没登录、超时）要原样上抛：
// 它们与"AI 返回的不是 JSON"修法完全不同，混成一个错误就没有下一步。
func TestExtractByAISurfacesEnvError(t *testing.T) {
	want := aiagent.ErrNotInstalled
	fa := &fakeAsker{err: want}
	_, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x"})
	if !errors.Is(err, want) {
		t.Fatalf("环境错误应原样暴露，实得 %v", err)
	}
	if errors.Is(err, ErrAINotJSON) {
		t.Error("别把环境问题归类成「AI 没返回 JSON」")
	}
}

func TestExtractByAIGuards(t *testing.T) {
	// 空内容不该惊动模型（白花一次 token）
	fa := &fakeAsker{out: aiOut(`{}`)}
	if _, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "   "}); err == nil || fa.calls != 0 {
		t.Errorf("空内容应直接拒绝且不调用 AI: err=%v calls=%d", err, fa.calls)
	}
	// 未注入 gate 时放行（无头/单测模式下这个 RPC 必须可用）
	if _, err := svcWithAI(&fakeAsker{out: aiOut(`{"a":1}`)}).ExtractByAI(JsonFmtRequest{Text: "x"}); err != nil {
		t.Errorf("未注入门时应放行: %v", err)
	}
}

// TestExtractByAICapsOversizedSend 超限文本必须**在发出去之前**拦住。
//
// 少了这道闸，本地注定拒收的体积会先原样交给模型、再在校验时失败：白外发一次
// 用户内容，还拿到一个必然的错误结果——外发与本地两条线的尺寸口径必须一致。
// TestExtractByAIRejectsFakeSuccesses 三个"看着像成功"的形态必须被拒。
//
// 它们都是审计推演给出、我逐条实测确认过的洞：走散文扫描时，`[]`/`{}` 是合法容器、
// 拒绝语里模型**自己编造的示例对象**也能解析成功，于是拒绝被包装成"提取成功 1 段"，
// 还顶着"已经过本地 JSON 校验"的警告——那句话当时是假话。
func TestExtractByAIRejectsFakeSuccesses(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"空数组不是成果", "[]"},
		{"空对象不是成果", "{}"},
		{"拒绝语夹编造示例", `很抱歉，这段文本里没有 JSON（形如 {"key":"value"} 的那样才算）。`},
		{"单标量数组不是成果", "[0]"},
		{"整句解释", "这是一份配置数据，包含商户号与状态字段。"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fa := &fakeAsker{out: aiOut(c.in)}
			r, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x", Indent: 2})
			if err == nil {
				t.Fatalf("%s 不该被当成成功，实得 OK=%v Kind=%q Output=%q warnings=%v",
					c.name, r.OK, r.Kind, r.Output, r.Warnings)
			}
			if !errors.Is(err, ErrAINotJSON) {
				t.Errorf("应归 ErrAINotJSON，实得 %v", err)
			}
		})
	}
}

// TestExtractByAIAcceptsContractShapes 契约内的形态要收下，别把守卫修成"拒一切"。
func TestExtractByAIAcceptsContractShapes(t *testing.T) {
	t.Run("空行分隔的两份对象", func(t *testing.T) {
		fa := &fakeAsker{out: aiOut("{\"a\":1}\n\n{\"b\":2}")}
		r, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x", Indent: 0})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Segments) != 2 || r.Kind != "multi" {
			t.Fatalf("两份都该收：段数=%d Kind=%q", len(r.Segments), r.Kind)
		}
		// AI 来源不报行号：那是模型输出的行数，与用户原文无关
		if r.Segments[0].Line != 0 || r.Segments[1].Line != 0 {
			t.Errorf("段行号必须留空，实得 %d / %d", r.Segments[0].Line, r.Segments[1].Line)
		}
	})
	t.Run("格式化对象里自带空行", func(t *testing.T) {
		// 中间有空行的 pretty JSON：必须走"整份先判"那条路，不能被切成两块判成垃圾
		fa := &fakeAsker{out: aiOut("{\n  \"a\": 1,\n\n  \"b\": 2\n}")}
		r, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x", Indent: 2})
		if err != nil {
			t.Fatalf("一份完整对象不该被切块切死: %v", err)
		}
		if r.Kind != "object" || !strings.Contains(r.Output, `"b": 2`) {
			t.Errorf("Kind 应收成 object（不是 multi），实得 %q / %q", r.Kind, r.Output)
		}
	})
	t.Run("围栏包住的单个对象", func(t *testing.T) {
		fa := &fakeAsker{out: aiOut("```json\n{\"a\":1,\"b\":[1,2]}\n```")}
		r, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x", Indent: 0})
		if err != nil {
			t.Fatalf("围栏剥掉后整块合法就该收: %v", err)
		}
		if r.Kind != "object" {
			t.Errorf("Kind=%q", r.Kind)
		}
	})
	t.Run("JSON 后附解释要说明丢了什么", func(t *testing.T) {
		fa := &fakeAsker{out: aiOut("{\"a\":1,\"b\":[1,2]}\n\n以上是提取结果，如需继续请说明。")}
		r, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x", Indent: 0})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(r.Warnings, "|")
		if !strings.Contains(joined, "已丢弃") || !strings.Contains(joined, "以上是提取结果") {
			t.Errorf("被丢弃的非 JSON 文字必须点名（不然用户不知道模型还说了话）：%v", r.Warnings)
		}
	})
}

// blockingAsker 可控地"卡在途"，用来测并发闸（不靠 sleep 猜时序）。
type blockingAsker struct {
	entered chan struct{}
	release chan struct{}
	out     *aiagent.Result
	calls   int
	// enteredOnce 不是洁癖：第三发（验"闸放开"那发）复用同一个 asker，
	// 裸 close 会二次关闭直接 panic——先跑的一版就是这么炸的。
	enteredOnce sync.Once
}

func (b *blockingAsker) Ask(ctx context.Context, _ aiagent.Request) (*aiagent.Result, error) {
	b.calls++
	b.enteredOnce.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return b.out, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestExtractByAISingleFlight 服务级单飞：前端的禁用挡不住"切走再切回"。
// 第二发要当场拒，并且不打扰正在跑的那一发，收口后闸要放开。
func TestExtractByAISingleFlight(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	fa := &blockingAsker{entered: entered, release: release, out: aiOut(`{"a":1,"b":[1,2]}`)}
	svc := svcWithAI(fa)

	first := make(chan error, 1)
	go func() { _, err := svc.ExtractByAI(JsonFmtRequest{Text: "x"}); first <- err }()
	<-entered // 确认真在途，而不是靠 sleep 猜

	if _, err := svc.ExtractByAI(JsonFmtRequest{Text: "y"}); err == nil {
		t.Error("第二发应被拒（并发起第二个 CLI 就是各烧几十秒、各外发一份内容）")
	}
	if fa.calls != 1 {
		t.Errorf("被拒的那发不该惊动模型，实调 %d 次", fa.calls)
	}
	close(release)
	if err := <-first; err != nil {
		t.Errorf("第一发不该受影响: %v", err)
	}
	if _, err := svc.ExtractByAI(JsonFmtRequest{Text: "z"}); err != nil {
		t.Errorf("在途结束后应可再发，否则一次调用就把模块锁死: %v", err)
	}
}

// TestExtractByAIUsesCancellableLease 传给 Ask 的 ctx 必须是**可取消**的那一份：
// 关窗/停用时它随背景租约被 cancel，一路传到 exec.CommandContext 收掉子进程。
// 原先传 context.Background()（本仓此前正是），表现就是"父进程被强杀、CLI 成孤儿"。
func TestExtractByAIUsesCancellableLease(t *testing.T) {
	// 观测点必须在**调用那一刻**：留到返回后再读 ctx，defer bg.Release() 早已把它取消，
	// 读到的"已取消"证明的是收口，不是在途可用性（我先就是这么写红的，第一次跑差点被
	// 当成代码缺陷——红了先分清是实现的错还是断言姿势的错）。
	var cancellable, aliveAtCall bool
	fa := &fakeAsker{out: aiOut(`{"a":1,"b":[1,2]}`), onAsk: func(ctx context.Context) {
		cancellable = ctx.Done() != nil // context.Background() 的 Done() 恒为 nil
		aliveAtCall = ctx.Err() == nil
	}}
	if _, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if !cancellable {
		t.Error("Ask 拿到的 ctx 不可取消——关窗时没人能收掉这条几十秒的调用")
	}
	if !aliveAtCall {
		t.Error("在途那一刻 ctx 不该已结束")
	}
}

// TestLiveExtractByAI 真机端到端：把本地四道**确实读不动**的形态交给本机 CLI。
//
// 默认 skip，与 aiagent 的真机夹具同一口径（`HANXI_AI_LIVE=1` 才跑）：真实模型的回答
// 不进 CI（必飘，也白烧 token），但机主一条命令就能验"这条路真能走通"。
// 前半段不是走过场——先证明本地管线在这条输入上真的失败，否则"AI 成功了"说明不了
// 任何事（可能本地早就搞定了）。值全是同形态假数据，真实商户号不进仓库。
func TestLiveExtractByAI(t *testing.T) {
	if os.Getenv("HANXI_AI_LIVE") == "" {
		t.Skip("未设 HANXI_AI_LIVE=1：跳过真机 AI 提取")
	}
	// 内容**本来就是 JSON 对象**，但字符串值里夹着未转义的引号：本地解析在那一列就断，
	// 抽取找不到闭合、补闭合又因"断在字符串内部"一律拒补（那是凭空编数据）——
	// 这正是留给模型的那一档：它照原文把内层引号转义掉就行，不新增也不改写任何值。
	// （刻意不用 Go 的 map 打印当样本：那形态本来不是 JSON，提示词说的是"提取 JSON 值"，
	// 模型照约定回 NO_JSON 是对的，拿它当"AI 失败"是测试设计不公平——真机第一轮就是这么打的。）
	in := `Oct 28 11:00:00 host svc[1]: 请求参数 = {
Oct 28 11:00:00 host svc[1]:   "acc_merchant_no": "2088000000000000",
Oct 28 11:00:00 host svc[1]:   "big_id": 9007199254740993,
Oct 28 11:00:00 host svc[1]:   "remark": "渠道回了一句 "开票通道忙" 就退了",
Oct 28 11:00:00 host svc[1]:   "invoice_status": "INVOICE_FAILED"
Oct 28 11:00:00 host svc[1]: }`
	if r := beautify(JsonFmtRequest{Text: in, Indent: 2, ExtractAll: true, Unescape: true}); r.OK {
		t.Skipf("本地已能处理这条样本，测不到 AI 路径（请换更脏的形态）：%s", r.Output)
	}

	svc := NewService(extapi.NewLeaseHolder(ID))
	r, err := svc.ExtractByAI(JsonFmtRequest{Text: in, Indent: 2})
	if err != nil {
		if errors.Is(err, aiagent.ErrNotInstalled) {
			t.Skip("本机没有 Claude Code 命令行：真机夹具跳过")
		}
		t.Fatalf("真机 AI 提取失败: %v", err)
	}
	if !r.OK || r.Kind != "object" {
		t.Fatalf("应得到一个对象，实得 Kind=%q OK=%v err=%q", r.Kind, r.OK, r.Error)
	}
	// 19 位 ID 逐字符保真是这条链路的真正考点（模型爱把它写成科学计数法或改掉末位）
	if !strings.Contains(r.Output, "9007199254740993") {
		t.Errorf("大整数被改写：%s", r.Output)
	}
	if !strings.Contains(r.Output, "INVOICE_FAILED") {
		t.Errorf("返回的不是样本里那份对象（模型可能答非所问）：%s", r.Output)
	}
	if strings.Contains(r.Output, "9.007") || strings.Contains(r.Output, "9007199254740992") {
		t.Errorf("数字被模型改写（本地校验也没拦住）：%s", r.Output)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "外发") {
		t.Errorf("外发量必须可见：%v", r.Warnings)
	}
	t.Logf("真机提取成功：%d 键，警告：%v", r.Stats.Keys, r.Warnings)
}

func TestExtractByAICapsOversizedSend(t *testing.T) {
	fa := &fakeAsker{out: aiOut(`{"a":1}`)}
	big := strings.Repeat("x", MaxInputBytes+1)
	_, err := svcWithAI(fa).ExtractByAI(JsonFmtRequest{Text: big})
	if err == nil {
		t.Fatal("超限应拒绝")
	}
	if fa.calls != 0 {
		t.Errorf("超限不得外发：模型被调了 %d 次", fa.calls)
	}
	if !strings.Contains(err.Error(), "超过单次上限") {
		t.Errorf("要给出可执行的下一步，实得 %v", err)
	}
}
