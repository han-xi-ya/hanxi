package jsonfmt

import (
	"fmt"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

// jsonfmt 的行为验收。期望值一律**手算/外部可核对**，不从实现反推——
// 抄实现的断言只能证明"代码等于它自己"，证明不了"数字没被改、行号是对的"。

func beaut(t *testing.T, text string, indent int, sort, unescape bool) *JsonFmtResult {
	t.Helper()
	return beautify(JsonFmtRequest{Text: text, Indent: indent, SortKeys: sort, Unescape: unescape})
}

// ---------- 保真：这是本模块存在的理由 ----------

// TestBigIntegersArePreserved 19 位雪花 ID 尾数必须逐字不动。
// 外部事实：该值超过 IEEE-754 double 的精确整数域（2^53≈9.0e15），任何
// "解析成 float64 再序列化"的实现都会改掉末位——JS 的 JSON.parse 与 Go 的
// map[string]any 路线都是。这里断言的是数学事实，不是实现巧合。
func TestBigIntegersArePreserved(t *testing.T) {
	cases := []string{
		`{"id":9007199254740993}`,                                        // 2^53+1：double 必塌成 ...92
		`{"id":18446744073709551615}`,                                    // uint64 满值
		`{"n":0.100000000000000005551115123125782702118158348728481425}`, // 超长小数
		`[3e-7,1E+30,-0,0.5e10]`,
	}
	for _, in := range cases {
		r := beaut(t, in, 2, false, false)
		if !r.OK {
			t.Fatalf("%s 应解析成功: %+v", in, r)
		}
		for _, lit := range bigLits(in) {
			if !strings.Contains(r.Output, lit) {
				t.Errorf("输入 %s 的数字字面量 %s 被改写，输出：%s", in, lit, r.Output)
			}
		}
	}
}

// bigLits 从样例里抠出数字字面量（测试侧独立实现，不复用解析器）。
func bigLits(s string) []string {
	var out []string
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '{' || r == '}' || r == '[' || r == ']' || r == ',' || r == ':' || r == '"'
	}) {
		tk := strings.TrimSpace(strings.ReplaceAll(tok, "id:", ""))
		tk = strings.TrimSpace(strings.ReplaceAll(tk, "n:", ""))
		if tk != "" && (tk[0] >= '0' && tk[0] <= '9' || tk[0] == '-' || tk[0] == '3' || tk[0] == '1') {
			out = append(out, tk)
		}
	}
	return out
}

// TestKeyOrderPreservedByDefault 默认保留原顺序（外部事实：Go 的 map 序列化必然
// 按字典序排，走 map 路线根本做不到这条）；开关打开后才重排。
func TestKeyOrderPreservedByDefault(t *testing.T) {
	in := `{"zeta":1,"alpha":2,"Mid":3}`

	asIs := beaut(t, in, 0, false, false)
	if iZ, iA := strings.Index(asIs.Output, `"zeta"`), strings.Index(asIs.Output, `"alpha"`); iZ > iA {
		t.Errorf("默认应保留写入顺序，实得 %s", asIs.Output)
	}
	if asIs.Output != in {
		t.Errorf("压缩形不应改动内容：got %s want %s", asIs.Output, in)
	}

	sorted := beaut(t, in, 0, true, false)
	if want := `{"Mid":3,"alpha":2,"zeta":1}`; sorted.Output != want {
		t.Errorf("排序输出 = %s, want %s", sorted.Output, want)
	}
}

// TestDuplicateKeysKeptAndWarned 重复键必须留痕。外部事实：RFC 8259 允许重复键，
// 而 Go 的 map 路线会把前一条静默覆盖掉——那恰好抹掉"同一份日志里同键写了两遍"这个
// 真实 bug 形态。
func TestDuplicateKeysKeptAndWarned(t *testing.T) {
	in := `{"a":1,"b":2,"a":3}`
	r := beaut(t, in, 0, false, false)
	if !r.OK {
		t.Fatalf("重复键不该报错: %+v", r)
	}
	if r.Output != in {
		t.Errorf("重复键必须原样保留，实得 %s", r.Output)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "重复键") {
		t.Errorf("应给出重复键警告，实得 %+v", r.Warnings)
	}
}

// TestHTMLEscapingNotIntroduced 输出不该冒出 &…——带 URL 的日志被美化后
// 反而更难读，是"美化"的反面。
func TestHTMLEscapingNotIntroduced(t *testing.T) {
	in := `{"u":"https://x.test/a?b=1&c=2","s":"<tag>","p":"中文 ✅"}`
	r := beaut(t, in, 2, false, false)
	if !r.OK {
		t.Fatalf("解析应成功: %+v", r)
	}
	// 断言的是**转义形式**不该出现，而不是原字符不该出现——输入本来就带着
	// & < >，把它们原样搬过去才是正确行为（曾把这条断言写成禁原字符，红得毫无意义）。
	for _, bad := range []string{"\\u0026", "\\u003c", "\\u003e"} {
		if strings.Contains(r.Output, bad) {
			t.Errorf("输出引入了 HTML 转义 %s：%s", bad, r.Output)
		}
	}
	// 反面同样要锁：原字符必须**原样**在场，不然"没引入转义"可能是因为字符被丢了。
	for _, keep := range []string{"&", "<", ">"} {
		if !strings.Contains(r.Output, keep) {
			t.Errorf("原字符 %s 应原样保留，实得 %s", keep, r.Output)
		}
	}
	if !strings.Contains(r.Output, "中文") {
		t.Errorf("非 ASCII 不应被转义掉：%s", r.Output)
	}
}

// ---------- 日志前缀剔除 ----------

func TestStripsCommonLogPrefixes(t *testing.T) {
	// 每行都带前缀的三段：真实日志的常见形态（时间戳、级别、线程段、logger 行号）。
	cases := []struct{ name, in string }{
		{
			name: "zap/JSON 日志行风格",
			in:   "2026-10-26T10:11:12.345+08:00\tINFO\tworker/run.go:88\t{\"a\":1,\"b\":[1,2]}",
		},
		{
			name: "方括号级别 + 线程",
			in:   "[2026-10-26 10:11:12] [INFO ] [worker-1] {\"a\":1}",
		},
		{
			name: "竖线分隔 + 短日期",
			in:   "10:11:12.345 | WARN | {\"a\":1}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := beaut(t, c.in, 0, false, false)
			if !r.OK {
				t.Fatalf("应剥前缀后解析成功: %+v | warnings=%v", r, r.Warnings)
			}
			if r.DroppedPrefixLines != 1 {
				t.Errorf("应记录剔除 1 行前缀，实得 %d", r.DroppedPrefixLines)
			}
			if r.Output != `{"a":1}` && r.Output != `{"a":1,"b":[1,2]}` {
				t.Errorf("输出只该含 JSON 本体，实得 %s", r.Output)
			}
			if !strings.Contains(strings.Join(r.Warnings, "|"), "日志前缀") {
				t.Error("剔除动作必须如实说明")
			}
		})
	}
}

func TestStripsPrefixOnEveryLineOfPrettyJSON(t *testing.T) {
	// 日志器把 pretty JSON 逐行输出时，每行都有前缀；中间行（"k": v、}）也必须能处理。
	in := strings.Join([]string{
		`2026-10-26 10:11:12 INFO  {`,
		`2026-10-26 10:11:12 INFO    "id": 9007199254740993,`,
		`2026-10-26 10:11:12 INFO    "list": [`,
		`2026-10-26 10:11:12 INFO      "x"`,
		`2026-10-26 10:11:12 INFO    ]`,
		`2026-10-26 10:11:12 INFO  }`,
	}, "\n")

	r := beaut(t, in, 2, false, false)
	if !r.OK {
		t.Fatalf("六行全带前缀也应能洗出来: %+v | %v", r, r.Warnings)
	}
	if r.DroppedPrefixLines != 6 {
		t.Errorf("应记 6 行被剥，实得 %d", r.DroppedPrefixLines)
	}
	if !strings.Contains(r.Output, "9007199254740993") {
		t.Errorf("大整数跨行也必须保真：%s", r.Output)
	}
}

// TestNeverManglesJSONLookingLines 是三道误剥保护的反面用例：这些行的形态与
// 日志前缀撞车，一旦当前缀剥掉就是**毁数据**，比解析失败严重得多。
func TestNeverManglesJSONLookingLines(t *testing.T) {
	cases := []struct{ in, want string }{
		{`[true]`, `[true]`},                     // 剥括号段规则会盯上它
		{`["a","b"]`, `["a","b"]`},               // 含引号，不该被当线程段
		{`[1,2]`, `[1,2]`},                       // 含逗号
		{`{"info":"keep"}`, `{"info":"keep"}`},   // 值里出现级别词
		{`{"t":"10:11:12"}`, `{"t":"10:11:12"}`}, // 值里出现时间形态
	}
	for _, c := range cases {
		r := beaut(t, c.in, 0, false, false)
		if !r.OK {
			t.Errorf("%s 应原样可解析: %+v", c.in, r)
			continue
		}
		if r.Output != c.want {
			t.Errorf("%s 被改写了：got %s want %s（前缀剔除不得碰 JSON 本体）", c.in, r.Output, c.want)
		}
		if r.DroppedPrefixLines != 0 {
			t.Errorf("%s 不该记为剔过前缀，实得 %d", c.in, r.DroppedPrefixLines)
		}
	}
}

func TestFallsBackWhenCleaningDoesNotHelp(t *testing.T) {
	// 剥完仍解析不了：要报"尝试过剥前缀"，并给出位置，不假装成功。
	in := `2026-10-26 10:11:12 INFO {"a": }`
	r := beaut(t, in, 2, false, false)
	if r.OK {
		t.Fatalf("畸形 JSON 不该判成功: %+v", r)
	}
	if r.Line == 0 || r.Col == 0 {
		t.Errorf("应给出行列，实得 %d:%d", r.Line, r.Col)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "仍无法解析") {
		t.Errorf("应交代清洗也未成功，实得 %+v", r.Warnings)
	}
}

// ---------- 转义还原 ----------

func TestUnwrapsStringifiedJSON(t *testing.T) {
	in := `{"code":0,"body":"{\"orderId\":9007199254740993,\"ok\":true}"}`
	r := beaut(t, in, 0, false, true)
	if !r.OK {
		t.Fatalf("应成功: %+v | %v", r, r.Warnings)
	}
	if r.Unwrapped != 1 {
		t.Errorf("应记展开 1 处，实得 %d", r.Unwrapped)
	}
	if !strings.Contains(r.Output, `"orderId":9007199254740993`) {
		t.Errorf("内层应被展开成真实节点：%s", r.Output)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "输出结构与输入不同") {
		t.Error("展开改变结构必须声明")
	}
	// 统计要跟着最终形状走，不能报展开前的数
	if r.Stats.Keys < 4 {
		t.Errorf("展开后键数应包含内层字段，实得 %+v", r.Stats)
	}
}

// TestDoesNotUnwrapNonJSONStrings 只展开 {/[ 开头的内容。"123"、"true" 被解成
// 数字/布尔是**改变数据含义**，不是美化。
func TestDoesNotUnwrapNonJSONStrings(t *testing.T) {
	for _, in := range []string{`{"a":"123"}`, `{"a":"true"}`, `{"a":"[not json]"}`, `{"a":"{unclosed"}`} {
		r := beaut(t, in, 0, false, true)
		if !r.OK {
			t.Fatalf("%s 应成功: %+v", in, r)
		}
		if r.Output != in {
			t.Errorf("%s 不该被改写，实得 %s", in, r.Output)
		}
		if r.Unwrapped != 0 {
			t.Errorf("%s 不该计为展开，实得 %d", in, r.Unwrapped)
		}
	}
}

func TestUnwrapIsOffByDefaultAndBounded(t *testing.T) {
	// 关掉开关就不该有任何展开迹象
	nested := `{"a":"{\"b\":\"{\\\"c\\\":1}\"}"}`
	if r := beaut(t, nested, 0, false, false); r.Unwrapped != 0 || !strings.Contains(r.Output, `\"c\\\":1`) {
		t.Errorf("未开转义还原却动了数据: %+v", r)
	}
	// 递归有上限（maxUnwrapDepth），畸形自嵌套不能无限剥
	r := beaut(t, nested, 0, false, true)
	if !r.OK || r.Unwrapped > maxUnwrapDepth {
		t.Errorf("展开层数应受限，实得 %+v", r)
	}
}

// ---------- 失败定位（行列均为 1 基，标点和多字节字符都占一格，逐个手算）----------

func TestFailurePositions(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantLine int
		wantCol  int
		wantFrag string
	}{
		{"第 3 行缺值", "{\"a\":1,\n\"b\":2,\n\"c\":}", 3, 5, "}"}, // 第 3 行是 "c":} ，} 是第 5 列
		{"未闭合字符串", "{\"m\":\"abc", 1, 6, "\""},                 // 报字符串起点：{ " m " : " → 第 6 列
		{"裸换行在字符串内", "{\"m\":\"a\nb\"}", 1, 8, ""},             // { " m " : " a LF → LF 是第 8 列
		{"尾随多余内容", "{\"a\":1} oops", 1, 9, "o"},
		{"中文前列数按 rune", "{\"汉字\":1, \"b\":}", 1, 14, "}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := beaut(t, c.in, 2, false, false)
			if r.OK {
				t.Fatalf("应失败: %+v", r)
			}
			if r.Line != c.wantLine || r.Col != c.wantCol {
				t.Errorf("位置 = %d:%d, want %d:%d（err=%s）", r.Line, r.Col, c.wantLine, c.wantCol, r.Error)
			}
			if r.Error == "" {
				t.Error("必须给人话原因")
			}
		})
	}
}

// ---------- 门与边界 ----------

func TestGuards(t *testing.T) {
	if r := beaut(t, "   ", 2, false, false); r.OK || !strings.Contains(r.Error, "没有内容") {
		t.Errorf("空输入应给可读提示: %+v", r)
	}
	if r := beaut(t, "[]", 2, false, false); !r.OK || r.Output != "[]" || r.Kind != "array" {
		t.Errorf("空数组应成功: %+v", r)
	}
	if r := beaut(t, "{}", 2, false, false); !r.OK || r.Kind != "object" || r.Stats.Nodes != 1 {
		t.Errorf("空对象统计应为 1 节点: %+v", r)
	}
	// 超限：拒绝而非截断（截断后的错误位置会把人带偏）
	big := `{"a":"` + strings.Repeat("x", MaxInputBytes) + `"}`
	r := beaut(t, big, 2, false, false)
	if r.OK || !strings.Contains(r.Error, "上限") {
		t.Errorf("超限应明确拒绝: %+v", r)
	}
	// 缩进钳制：200 不该撑出巨型输出
	if out := beaut(t, `{"a":1}`, 200, false, false).Output; strings.Contains(out, strings.Repeat(" ", 9)) {
		t.Errorf("缩进未被钳制：%q", out)
	}
}

func TestNormalizeIndent(t *testing.T) {
	cases := map[int]int{0: 0, -3: 0, 1: 1, 2: 2, 4: 4, 8: 8, 9: 8, 200: 8}
	for in, want := range cases {
		if got := normalizeIndent(in); got != want {
			t.Errorf("normalizeIndent(%d) = %d, want %d", in, got, want)
		}
	}
}

// ---------- 多段抽取（终端里一段日志夹好几段 JSON） ----------

// TestExtractMultipleSegments 一次粘进终端输出，里面有 3 段 JSON 与噪声文字：
// 必须全抽出来、按出现顺序编号、各自带**在原文里**的行号。
// 这是这条需求的全部理由——否则用户得手工注释掉别的段才能美化一段。
func TestExtractMultipleSegments(t *testing.T) {
	in := strings.Join([]string{
		`2026-10-26 10:11:12 INFO  starting worker`,
		`request={"id":9007199254740993,"path":"/a"}`,
		`some noise between`,
		`response body: [{"code":1},{"code":2}]`,
		`panic: runtime error`,
		`{"nested":{"deep":[{"x":1}]}}`,
	}, "\n")

	r := beautify(JsonFmtRequest{Text: in, Indent: 0, Unescape: false, ExtractAll: true})
	if !r.OK {
		t.Fatalf("应抽出多段: %+v | %v", r, r.Warnings)
	}
	// 3 段：第 4 行的 [{"code":1},{"code":2}] 整体是一个数组值，不该被拆成两段
	if len(r.Segments) != 3 {
		t.Fatalf("应抽到 3 段，实得 %d：%+v", len(r.Segments), r.Segments)
	}
	if r.Kind != "multi" {
		t.Errorf("多段时顶层 Kind 应是 multi 标记，实得 %q", r.Kind)
	}
	// 行号必须指向原文里那段 JSON 所在的行，用户才能回对日志
	wantLines := []int{2, 4, 6}
	for i, want := range wantLines {
		if r.Segments[i].Line != want {
			t.Errorf("第 %d 段行号 = %d, want %d", i+1, r.Segments[i].Line, want)
		}
	}
	if r.Segments[0].Index != 1 || r.Segments[2].Index != 3 {
		t.Errorf("段序号应从 1 起，实得 %+v", r.Segments)
	}
	// 大整数在多段路径上同样不能毁（两条路径两套代码，最容易一头保真一头丢）
	if !strings.Contains(r.Segments[0].Output, "9007199254740993") {
		t.Errorf("第 1 段大整数被改写：%s", r.Segments[0].Output)
	}
	// 顶层 Output 是拼接且不带装饰文字：复制出去还得是能用文本
	if strings.Contains(r.Output, "段") || strings.Contains(r.Output, "——") {
		t.Errorf("顶层 Output 混入了展示用文字：%s", r.Output)
	}
	if !strings.Contains(r.Output, `{"code":1}`) || !strings.Contains(r.Output, `{"nested"`) {
		t.Errorf("顶层 Output 应含各段内容：%s", r.Output)
	}
	// 聚合数必须等于各段之和，否则汇总行与明细互斥
	var sumKeys, sumNodes int
	for _, s := range r.Segments {
		sumKeys += s.Stats.Keys
		sumNodes += s.Stats.Nodes
	}
	if r.Stats.Keys != sumKeys || r.Stats.Nodes != sumNodes {
		t.Errorf("顶层统计与分段明细不一致：顶层 %+v vs 合计 keys=%d nodes=%d", r.Stats, sumKeys, sumNodes)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "抽取") {
		t.Error("抽取动作必须声明")
	}
}

// TestExtractDoesNotSplitNestedJSON 外层能整体解析时，不许把它的子对象再抽成独立段
// ——那是把一份数据切成四份，比不美化更糟。
func TestExtractDoesNotSplitNestedJSON(t *testing.T) {
	// 两侧都带噪声：剥前缀道救不了（尾部还有杂词），只能靠抽取——这才是本用例要测的形态
	r := beautify(JsonFmtRequest{Text: `noise here {"a":{"b":{"c":1}}} tail noise`, Indent: 2, ExtractAll: true})
	if !r.OK {
		t.Fatalf("应成功: %+v | %v", r, r.Warnings)
	}
	if len(r.Segments) != 1 {
		t.Fatalf("嵌套结构应整体算一段，实得 %d：%+v", len(r.Segments), r.Segments)
	}
	if r.Segments[0].Kind != "object" || r.Segments[0].Stats.MaxDepth != 4 {
		t.Errorf("形状/深度不对: %+v", r.Segments[0].Stats)
	}
}

// TestExtractIsOptIn 关掉开关必须回到"报一个错"的老行为：抽取是最后一道兜底，
// 不是默认改写用户输入的许可。
func TestExtractOffKeepsFailurePath(t *testing.T) {
	in := `noise {"a":1} more noise`
	off := beautify(JsonFmtRequest{Text: in, Indent: 2, ExtractAll: false})
	if off.OK {
		t.Fatalf("关闭抽取时不该成功: %+v", off)
	}
	on := beautify(JsonFmtRequest{Text: in, Indent: 2, ExtractAll: true})
	if !on.OK || len(on.Segments) != 1 {
		t.Fatalf("开启抽取应抽出 1 段: %+v | %v", on, on.Warnings)
	}
}

// TestExtractPrefersWholeDocument 前两道能成就不走抽取：整段解析的结果是"确定的一个值"，
// 比扫描器猜出 N 段更可信。这里输入同时满足两种解读。
func TestExtractPrefersWholeDocument(t *testing.T) {
	r := beautify(JsonFmtRequest{Text: `{"a":1,"b":{"c":2}}`, Indent: 2, ExtractAll: true})
	if !r.OK || len(r.Segments) != 0 {
		t.Fatalf("整段可解析时不该进抽取分支: %+v", r)
	}
	if r.Kind != "object" {
		t.Errorf("Kind = %q", r.Kind)
	}
}

// TestServiceBeautifyPassesGate 锁 JsonFmtService 层的门接线本身：门未注入时 LeaseHolder
// 必须放行（否则无头/单测模式下这个 RPC 永远不可用）。门矩阵那边只按字面函数名
// 匹配 Enter()（#109 那类坑），这里覆盖的是运行时行为而不是静态形状。
func TestServiceBeautifyPassesGate(t *testing.T) {
	s := NewService(extapi.NewLeaseHolder(ID))
	r, err := s.Beautify(JsonFmtRequest{Text: `{}`})
	if err != nil {
		t.Fatalf("未注入门时应放行: %v", err)
	}
	if !r.OK || r.Kind != "object" {
		t.Errorf("应成功: %+v", r)
	}
}

// TestScanAttemptCapIsReported 锁抽取的闸门**会被告知**。
//
// 刻意不断言耗时：墙钟断言在并发门禁下必假红（见 TROUBLESHOOTING #124/#122 同族教训），
// 而这道闸的意义本来就是"最坏情况有上界"，所以断言上界与诚实声明，不掐表。
func TestScanAttemptCapIsReported(t *testing.T) {
	r := beautify(JsonFmtRequest{
		Text: strings.Repeat("{", maxScanAttempts+5000), Indent: 2, ExtractAll: true,
	})
	if r.OK {
		t.Fatalf("纯左括号不该判成功: %+v", r)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "尝试数已达上限") {
		t.Errorf("撞闸必须说明已停止扫描，否则用户会去查自己 JSON 的语法：%+v", r.Warnings)
	}
}

// ---------- 截断救回与失败分类 ----------

// TestAutoRepairNeverTouchesValidInput 是这条新语义的正面锁：能原样解析的输入，
// 一个字节都不许被改写——"自动修复"的前提必须是"原文本来就读不了"。
func TestAutoRepairNeverTouchesValidInput(t *testing.T) {
	for _, in := range []string{
		`{"a":1,"b":[1,2,{"c":3}]}`,
		`{"s":"quote \" here, and colon: ok"}`,
		`[true,false,null,-1.5e3]`,
	} {
		r := beautify(JsonFmtRequest{Text: in, Indent: 0})
		if !r.OK {
			t.Fatalf("合法输入不该失败: %+v", r)
		}
		if r.Output != in {
			t.Errorf("合法输入被改写了: got %s want %s", r.Output, in)
		}
		if len(r.Repairs) != 0 || r.Repaired != 0 {
			t.Errorf("不该报任何修复: %+v", r.Repairs)
		}
	}
}

// TestAutoRepairsLooseSyntax 宽松写法不必再要求用户手工改：逐类计数摆出来。
func TestAutoRepairsLooseSyntax(t *testing.T) {
	r := beautify(JsonFmtRequest{Text: "{'a': 1, b: 2,}", Indent: 0})
	if !r.OK {
		t.Fatalf("宽松写法应被自动修好: %+v | %v", r, r.Warnings)
	}
	if r.Output != `{"a":1,"b":2}` {
		t.Errorf("修复结果不对: %s", r.Output)
	}
	got := map[string]int{}
	for _, st := range r.Repairs {
		got[st.Kind] = st.Count
	}
	if got["单引号改双引号"] != 1 || got["无引号的键补上引号"] != 1 || got["删掉多余的尾逗号"] != 1 {
		t.Errorf("修复分类计数不对: %+v", r.Repairs)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "已自动修复") {
		t.Errorf("必须声明做过修复: %+v", r.Warnings)
	}
}

// TestAutoRepairRescuesTruncated 截断输入的救回（无需任何勾选），且必须标修复态。
func TestAutoRepairRescuesTruncated(t *testing.T) {
	r := beautify(JsonFmtRequest{Text: `{"a":{"b":[1,2,3`, Indent: 2})
	if !r.OK {
		t.Fatalf("应自动补尾救回: %+v | hint=%q", r, r.Hint)
	}
	if r.Repaired != 3 { // 未闭合三层：外层 {、a 的值 {、里面的 [
		t.Errorf("应补 3 个闭合符，实得 %d", r.Repaired)
	}
	if !strings.Contains(r.Output, `"b"`) || !strings.Contains(r.Output, "3") {
		t.Errorf("内容不该丢：%s", r.Output)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "修复态") {
		t.Errorf("必须声明修复态: %+v", r.Warnings)
	}
}

// TestCompleteTailRefusesToInventData 三道闸逐条验：不该补的绝不补。
func TestCompleteTailRefusesToInventData(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"完整输入无需补", `{"a":1}`, false},
		{"断在字符串里（补引号=编数据）", `{"a":"bc`, false},
		{"括号顺序本来就错（不猜意图）", `{]}`, false},
		{"尾部缺一层", `{"a":[1,2`, true},
		{"头部问题不由尾部补", `],"x":1}`, false},
	}
	for _, c := range cases {
		_, _, ok := completeTail([]byte(c.in))
		if ok != c.ok {
			t.Errorf("%s: completeTail(%q).ok = %v, want %v", c.name, c.in, ok, c.ok)
		}
	}
	// 补齐数要等于未闭合层数
	_, added, ok := completeTail([]byte(`{"a":{"b":{"c":[1`))
	if !ok || added != 4 {
		t.Errorf("应补 4 个闭合符，实得 added=%d ok=%v", added, ok)
	}
}

// TestFailureHintsPointRight 三种病因对应三种不同动作，文案不能指反方向。
// 旧行为把"开头没复制全"报成"JSON 之后还有多余内容"——用户会去改结尾，永远改不对。
func TestFailureHintsPointRight(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantFrag string
		wantNone bool
	}{
		{"尾部截断且救不了", `{"a":"bc`, "尾部被截断", false}, // 断在字符串里：补引号等于编数据，只能报错并指方向
		{"开头没复制全", `],"x":1}`, "开头没复制全", false},
		// 前导文字（`请求参数 = `）：解析器在第 1 个字符就撞墙，光报"意外的字符"会让人
		// 以为是自己 JSON 写错。这里连行列都写死——"第 8 列"是按 rune 数出来的真实位置
		// （请求参数=7 个 rune + 1），数字错了就是位置算错，不是文案差异。
		{"开头有前导文字", `请求参数 = {"a": 1}`, "第 1 行第 8 列", false},
		{"纯语法错不乱猜", `{"a":tru}`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := beautify(JsonFmtRequest{Text: c.in, Indent: 2})
			if c.wantNone {
				if r.Hint != "" {
					t.Errorf("无把握时不该给提示，实得 %q", r.Hint)
				}
				return
			}
			if !strings.Contains(r.Hint, c.wantFrag) {
				t.Errorf("提示应含 %q，实得 %q", c.wantFrag, r.Hint)
			}
		})
	}
}

// TestRelaxLooseForms 逐种宽松写法验到"能解析且值不变"。
// 期望值全部手算，不从实现反推——这条表里任何一行翻车，都等于把用户的数据改了。
func TestRelaxLooseForms(t *testing.T) {
	cases := []struct {
		name, in, want string
		wantKind       string // 出现的修复类别之一
		repairless     bool   // true 表示不该发生任何修复
	}{
		{name: "单引号包双引号内容", in: `{'a': 'say "hi"'}`, wantKind: repSingleQuote},
		{name: "转义的单引号还原", in: `{'msg': 'it\'s ok'}`, wantKind: repSingleQuote},
		{name: "Python 字面量", in: `{'a': True, 'b': None, 'c': False}`, wantKind: repPyLiteral},
		{name: "尾逗号", in: `[1, 2, 3,]`, wantKind: repTrailingComma},
		{name: "块注释", in: `{"a": 1 /* x */, "b": 2}`, wantKind: repComment},
		{name: "行注释", in: "{\"a\": 1 // tail\n}", wantKind: repComment},
		{name: "等号分隔", in: `{a=1, b=2}`, wantKind: repEqSeparator},
		{name: "NaN 转 null 要留痕", in: `{"a": NaN}`, wantKind: repNaNToNull},
		{name: "已经是严格 JSON", in: `{"a":1,"b":[1,2]}`, repairless: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := beautify(JsonFmtRequest{Text: c.in, Indent: 0})
			if !r.OK {
				t.Fatalf("应修好并解析: %+v | hint=%q warn=%v", r, r.Hint, r.Warnings)
			}
			if c.repairless {
				if len(r.Repairs) != 0 {
					t.Errorf("严格 JSON 不该被修: %+v", r.Repairs)
				}
				return
			}
			var kinds []string
			for _, st := range r.Repairs {
				kinds = append(kinds, st.Kind)
			}
			if !strings.Contains(strings.Join(kinds, "|"), c.wantKind) {
				t.Errorf("应记类别 %q，实得 %+v（输出 %s）", c.wantKind, r.Repairs, r.Output)
			}
			if c.want != "" && r.Output != c.want {
				t.Errorf("输出 = %s, want %s", r.Output, c.want)
			}
		})
	}
}

// TestRelaxKeepsNumbersAndStringsUntouched 修复只许动语法外壳：
// 数字变字符串、字符串内容被改，都属于"编数据"，当场算失败。
func TestRelaxKeepsNumbersAndStringsUntouched(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{a: 1, b: -2.5, c: 1e3}`, `{"a":1,"b":-2.5,"c":1e3}`},
		{`{'n': 9007199254740993}`, `{"n":9007199254740993}`},
		{`{a: 'x y', b: "1\"2"}`, `{"a":"x y","b":"1\"2"}`},
	}
	for _, c := range cases {
		r := beautify(JsonFmtRequest{Text: c.in, Indent: 0})
		if !r.OK {
			t.Fatalf("%s 应成功: %+v", c.in, r)
		}
		if r.Output != c.want {
			t.Errorf("relax(%s) = %s, want %s", c.in, r.Output, c.want)
		}
	}
}

// ---------- 统计与查找 ----------

func statOf(rs []KeyStat, path string) (KeyStat, bool) {
	for _, s := range rs {
		if s.Path == path {
			return s, true
		}
	}
	return KeyStat{}, false
}

// TestKeyStatsCountsPaths 路径表是"一批记录"最有用的一页。
// 数组下标折叠成 []：期望的是一条 `items[].id count=2`，不是两条带索引的重复行。
func TestKeyStatsCountsPaths(t *testing.T) {
	in := `{"data":{"items":[{"id":1,"name":"a"},{"id":2,"name":null}]}}`
	r := beautify(JsonFmtRequest{Text: in, Indent: 2})
	if !r.OK {
		t.Fatalf("应成功: %+v", r)
	}
	cases := []struct {
		path  string
		count int
		typ   string // 该路径下应出现的类型
	}{
		{"data", 1, "object"},
		{"data.items", 1, "array"},
		{"data.items[]", 2, "object"},
		{"data.items[].id", 2, "number"},
		{"data.items[].name", 2, "string"},
		{"data.items[].name", 2, "null"}, // 同名键两种类型都要在，不能合并成一个
	}
	for _, c := range cases {
		st, ok := statOf(r.KeyStats, c.path)
		if !ok {
			t.Errorf("缺路径 %q，实得 %+v", c.path, r.KeyStats)
			continue
		}
		if st.Count != c.count {
			t.Errorf("%s count = %d, want %d", c.path, st.Count, c.count)
		}
		found := false
		for _, tc := range st.Types {
			if tc.Type == c.typ {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 应含类型 %q，实得 %+v", c.path, c.typ, st.Types)
		}
	}
	// 排序：最常出现的键在前，用户不必自己数
	if len(r.KeyStats) > 1 && r.KeyStats[0].Count < r.KeyStats[len(r.KeyStats)-1].Count {
		t.Errorf("应按次数降序，实得 %+v", r.KeyStats)
	}
}

// TestQueryMatches 查找命中要给出可照着改的路径与值摘要。
func TestQueryMatches(t *testing.T) {
	in := `{"user":{"traceId":"abc-123"},"items":[{"traceId":"zzz"},{"note":"contains ABC here"}]}`
	r := beautify(JsonFmtRequest{Text: in, Indent: 2, Query: "traceid"})
	if !r.OK {
		t.Fatalf("应成功: %+v", r)
	}
	// 路径命中 2 条（user.traceId、items[].traceId）
	if len(r.Matches) != 2 {
		t.Fatalf("应命中 2 条路径，实得 %d：%+v", len(r.Matches), r.Matches)
	}
	if r.Matches[0].Path != "user.traceId" || r.Matches[0].Value != "abc-123" {
		t.Errorf("命中内容不对: %+v", r.Matches[0])
	}

	// 值命中（大小写不敏感）与空查询不做匹配
	rv := beautify(JsonFmtRequest{Text: in, Indent: 2, Query: "contains"})
	if len(rv.Matches) != 1 || rv.Matches[0].Path != "items[].note" {
		t.Errorf("值命中不对: %+v", rv.Matches)
	}
	empty := beautify(JsonFmtRequest{Text: in, Indent: 2})
	if len(empty.Matches) != 0 || len(empty.KeyStats) == 0 {
		t.Errorf("不查时不该给命中但仍要给统计: matches=%+v stats=%d", empty.Matches, len(empty.KeyStats))
	}
}

// TestQueryMatchCap 命中上限必须"告知被截断"，而不是静默只给前 500 条——
// 用户以为那就是全部，就会开始怀疑数据。
func TestQueryMatchCap(t *testing.T) {
	var b strings.Builder
	b.WriteByte('{')
	for i := range maxMatches + 120 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"k%d": "hit me"`, i)
	}
	b.WriteByte('}')
	r := beautify(JsonFmtRequest{Text: b.String(), Indent: 0, Query: "hit me"})
	if !r.OK {
		t.Fatalf("应成功: %+v", r)
	}
	if len(r.Matches) != maxMatches || !r.MatchTruncated {
		t.Errorf("应给满 %d 条并声明截断，实得 %d truncated=%v", maxMatches, len(r.Matches), r.MatchTruncated)
	}
}

// TestStatsAcrossExtractedSegments 多段抽取时统计合并、命中带段号与行号：
// 光说"这个键出现 4 次"不够，得能回到是第几段的第几行。
func TestStatsAcrossExtractedSegments(t *testing.T) {
	in := "noise {\"traceId\":\"a-x\"}\nmore noise {\"traceId\":\"b-y\"}"
	r := beautify(JsonFmtRequest{Text: in, Indent: 0, ExtractAll: true, Query: "traceid"})
	if !r.OK || len(r.Segments) != 2 {
		t.Fatalf("应抽出 2 段: %+v | %v", r, r.Warnings)
	}
	st, ok := statOf(r.KeyStats, "traceId")
	if !ok || st.Count != 2 {
		t.Fatalf("合并统计应有 traceId count=2，实得 %+v", r.KeyStats)
	}
	if len(r.Matches) != 2 {
		t.Fatalf("应命中 2 条，实得 %+v", r.Matches)
	}
	if r.Matches[0].Segment != 1 || r.Matches[1].Segment != 2 {
		t.Errorf("命中应带段号: %+v", r.Matches)
	}
	if r.Matches[0].Line != 1 || r.Matches[1].Line != 2 {
		t.Errorf("命中应带原文行号: %+v", r.Matches)
	}
}

// TestStatsLineCountsAreRealLines 行数统计数的是真的换行符。
//
// 期望值取自渲染形态本身而不是取自实现：2 空格缩进下单键对象就是 3 行
// （`{`、一行键、`}`），跟单键对象 JSON.stringify(x, null, 2) 的行数一致。
// 本轮这里恒报 1 行——那处把换行写成了"反斜杠加 n"两个字符的字面量，数不到东西
// 又不报错，是典型的静默撒谎，所以数字要写死在测试里钉住。
func TestStatsLineCountsAreRealLines(t *testing.T) {
	in := "noise {\"traceId\":\"a-x\"}\nmore noise {\"traceId\":\"b-y\"}"
	r := beautify(JsonFmtRequest{Text: in, Indent: 2, ExtractAll: true})
	if !r.OK || len(r.Segments) != 2 {
		t.Fatalf("应抽出 2 段: %+v | %v", r, r.Warnings)
	}
	for _, sg := range r.Segments {
		if sg.Stats.LinesOut != 3 {
			t.Errorf("单键对象缩进 2 应是 3 行，实得 %d：\n%s", sg.Stats.LinesOut, sg.Output)
		}
	}
	if r.Stats.LinesOut != 6 {
		t.Errorf("两段的行数应聚合成 6，实得 %d", r.Stats.LinesOut)
	}

	// 整段解析路径（非抽取）也要同一口径
	s := beautify(JsonFmtRequest{Text: `{"a":1,"b":2}`, Indent: 2})
	if !s.OK || s.Stats.LinesOut != 4 {
		t.Errorf("两键对象缩进 2 应是 4 行，实得 %+v", s.Stats)
	}
	// 压缩输出只有一行
	m := beautify(JsonFmtRequest{Text: `{"a":1,"b":2}`, Indent: 0})
	if !m.OK || m.Stats.LinesOut != 1 {
		t.Errorf("压缩输出应为 1 行，实得 %+v", m.Stats)
	}
}

// ---------- 结构化替换 ----------

func repReq(in string, path, val, key string) JsonFmtRequest {
	return JsonFmtRequest{Text: in, Indent: 0, Replace: &Replace{Path: path, NewValue: val, NewKey: key}}
}

// TestReplaceValueAtPath 折叠路径一次命中数组里所有同名键：这是日志场景最常做的
// （"把这一批记录的 status 都改成 ok"）。
func TestReplaceValueAtPath(t *testing.T) {
	in := `{"a":{"items":[{"id":1,"ok":false},{"id":2,"ok":false}]}}`
	r := beautify(repReq(in, "a.items[].ok", "true", ""))
	if !r.OK {
		t.Fatalf("应成功: %+v", r)
	}
	if r.Replaced != 2 {
		t.Errorf("应改 2 处，实得 %d（输出 %s）", r.Replaced, r.Output)
	}
	if strings.Count(r.Output, `"ok":true`) != 2 || strings.Contains(r.Output, "false") {
		t.Errorf("替换没落到输出：%s", r.Output)
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "已按路径") {
		t.Errorf("必须声明替换了几处: %+v", r.Warnings)
	}
}

// TestReplaceKeepsValueTypes 新值的类型由字面量决定：带引号是字符串、不带是数字。
// 猜成字符串等于悄悄改类型，所以非法字面量一律拒绝。
func TestReplaceKeepsValueTypes(t *testing.T) {
	base := `{"n":1,"s":"x"}`
	num := beautify(repReq(base, "n", `"now-a-string"`, ""))
	if !num.OK || !strings.Contains(num.Output, `"n":"now-a-string"`) {
		t.Errorf("数字改字符串应生效: %+v | %s", num, num.Output)
	}
	str := beautify(repReq(base, "s", `42`, ""))
	if !str.OK || !strings.Contains(str.Output, `"s":42`) {
		t.Errorf("字符串改数字应生效: %+v | %s", str, str.Output)
	}
	bad := beautify(repReq(base, "s", `abc`, "")) // 裸词不是合法 JSON
	if bad.OK {
		t.Errorf("非法新值必须拒绝，实得输出 %s", bad.Output)
	}
	if !strings.Contains(bad.Error, "合法 JSON") {
		t.Errorf("要说明为什么拒绝: %q", bad.Error)
	}
}

// TestReplaceRenameKey 改名后仍是合法 JSON，且键的字面量形态被一起换掉。
func TestReplaceRenameKey(t *testing.T) {
	r := beautify(repReq(`{"a":1,"b":2}`, "a", "", "alpha"))
	if !r.OK || r.Output != `{"alpha":1,"b":2}` {
		t.Fatalf("改名结果不对: %+v | %s", r, r.Output)
	}
	if r.Replaced != 1 {
		t.Errorf("Replaced = %d", r.Replaced)
	}
}

// TestReplaceRequiresExplicitRequest 替换不自动：不带 Replace 的请求 Replaced 恒 0，
// 且输出与基线逐字相同。这条防的是"某天后端把替换塞进默认路径"。
func TestReplaceRequiresExplicitRequest(t *testing.T) {
	in := `{"a":1}`
	base := beautify(JsonFmtRequest{Text: in, Indent: 0})
	noRep := beautify(JsonFmtRequest{Text: in, Indent: 0, Query: "a"})
	if base.Output != noRep.Output || noRep.Replaced != 0 {
		t.Errorf("无替换请求却动了数据: %s vs %s", base.Output, noRep.Output)
	}
	// 未命中：必须一字不改 + 如实说没命中（而不是给个"成功"的沉默）
	miss := beautify(repReq(in, "zzz.deep", `1`, ""))
	if !miss.OK || miss.Replaced != 0 || miss.Output != base.Output {
		t.Errorf("未命中不该改任何内容: %+v | %s", miss, miss.Output)
	}
	if !strings.Contains(strings.Join(miss.Warnings, "|"), "没命中") {
		t.Errorf("未命中必须声明: %+v", miss.Warnings)
	}
}

// TestReplaceAcrossSegments 多段抽取时同样按路径替换，并汇总计数。
func TestReplaceAcrossSegments(t *testing.T) {
	in := "log {\"traceId\":\"a\"} tail {\"traceId\":\"b\"}"
	r := beautify(JsonFmtRequest{Text: in, Indent: 0, ExtractAll: true,
		Replace: &Replace{Path: "traceId", NewValue: `"cleared"`}})
	if !r.OK || len(r.Segments) != 2 {
		t.Fatalf("应抽出 2 段: %+v", r)
	}
	if r.Replaced != 2 {
		t.Errorf("两段各命中一处，Replaced = %d", r.Replaced)
	}
	if strings.Count(r.Output, `"cleared"`) != 2 {
		t.Errorf("输出未都替换: %s", r.Output)
	}
}

// TestReplaceBadRequests 三类非法请求都要挡住（它们是"没法执行"，不是"没命中"）。
func TestReplaceBadRequests(t *testing.T) {
	cases := []struct{ name, path, val, key, want string }{
		{"没给路径", "", `1`, "", "请填写要替换的路径"},
		{"既没新值也没新键", "a", "", "", "至少一个"},
		{"键名带空格", "a", "", "new key", "换个写法"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := beautify(repReq(`{"a":1}`, c.path, c.val, c.key))
			if r.OK || !strings.Contains(r.Error, c.want) {
				t.Errorf("应拒绝并含 %q，实得 ok=%v err=%q", c.want, r.OK, r.Error)
			}
		})
	}
}
