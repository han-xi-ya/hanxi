package jsonfmt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
)

// Asker 是"问一次外部模型"的能力接口。刻意留成接口而不是直接调 aiagent：
// 否则这些分支（AI 没装、超时、返回的不是 JSON）就只能靠真装 CLI + 真联网才能测。
type Asker interface {
	Ask(ctx context.Context, req aiagent.Request) (*aiagent.Result, error)
}

// JsonFmtService 暴露给前端的 JSON 美化服务。
//
// 本地路径是无状态纯计算：一次调用一次返回，不发事件、不留任务，全部逻辑在 beautify
// 这个纯函数里，JsonFmtService 只负责接调用门——这样清洗/解析/渲染的行为不必启动 Wails
// 就能逐条断言。**唯一的例外是 AI 那条**：它一次要占几十秒、还拉起一个子进程，
// 于是这里必须有并发状态（见 aiRunning），"无状态"这句话不能再原样说下去了。
type JsonFmtService struct {
	holder *extapi.LeaseHolder
	// newAsker 懒构造：没装 Claude Code 的机器上本地四道照常可用，只有按下 AI 按钮
	// 时才去发现 CLI 并如实报错——可选依赖缺失不该让整个模块构造失败。
	newAsker func() (Asker, error)
	// aiRunning 是**服务级**单飞闸。前端那个 aiBusy 挡不住：切走再切回就是一个新组件
	// 实例、aiBusy 归零，来回两三下即并发起 2~3 个 claude，每个各带几十秒预算、各外发
	// 一份内容，关窗时全成孤儿。并发上限这种事不能指望界面自觉。
	aiRunning atomic.Bool
}

// extractInstruction 是给模型的任务说明，写死在后端：让"只提取、不改数值、不解释"
// 成为接口契约的一部分，而不是某个 UI 文案的副产品（前端能传提示，就等于谁都能改它）。
const extractInstruction = `任务：从下面 ---INPUT-BEGIN--- 与 ---INPUT-END--- 之间的文本里，提取其中所有 JSON 值（对象或数组），按出现顺序输出。

硬性要求：
1. 只输出 JSON 本身，多个对象之间用空行分隔；不要解释、不要代码块围栏、不要序号或标题。
2. 数字逐字符照抄原文，禁止改写精度或改用科学计数法（19 位以上的 ID、雪花号不得四舍五入）。
3. 字符串原样照抄，含中文、转义符与内部标点。
4. 文本里若没有完整 JSON，只输出一行 NO_JSON。`

// ErrAINotJSON 单列哨兵："AI 没返回 JSON"与"AI 调用失败"是两类问题，
// 前者可能重试就好，后者要去看环境（未登录、未安装）。
var ErrAINotJSON = errors.New("AI 返回的内容不是合法 JSON")

// NewService 创建服务实例；holder 由模块构造期建好并经 SetGate 接上调用门。
func NewService(holder *extapi.LeaseHolder) *JsonFmtService {
	return &JsonFmtService{holder: holder, newAsker: func() (Asker, error) { return aiagent.New() }}
}

// Beautify 清洗并格式化一段 JSON。
//
// Wave 3 口径：门拒绝时如实上抛，禁止回空结果冒充"解析失败"（那会让用户以为是自己的
// JSON 有问题）。
func (s *JsonFmtService) Beautify(req JsonFmtRequest) (*JsonFmtResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return beautify(req), nil
}

// ExtractByAI 把本地四道都救不了的脏文本交给模型提取，再走一遍**本地**校验与格式化。
//
// 顺序不能反：AI 只负责"从任意文本里认出结构"，输出是否合法、数值有没有被改写，
// 一律由本地解析器裁决。让模型的返回直接上屏，等于把 hallucination 当成用户数据。
func (s *JsonFmtService) ExtractByAI(req JsonFmtRequest) (*JsonFmtResult, error) {
	// 门分两步，是有意的：
	//   ① `Enter()` 只做**准入判定**并立刻归还。它不能抱着不放——一次 AI 往返实测 66 秒，
	//      而退出 drain 预算是 60 秒（registry.go:25），前台租约压上去，关窗就先白等
	//      一分钟再强制收口，Go 父进程死了、杀掉子进程那一步永远没执行，claude 成孤儿
	//      继续烧网络与 token。
	//   ② 长任务挂**背景租约**：它按设计就是"可取消的在做事"，停用/退出会先 cancel
	//      再等 Release（gate.go 的 BackgroundLease），于是 ctx 一路传到 exec.CommandContext，
	//      子进程真被收，而不是被 drain 超时抛弃。
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	release()

	bg, bgErr := s.holder.EnterBackground(context.Background())
	if bgErr != nil {
		return nil, bgErr
	}
	defer bg.Release()

	// 单飞：宁可明说"已经有一发在跑"，也不要并发起第二个 CLI 各自烧几十秒。
	if !s.aiRunning.CompareAndSwap(false, true) {
		return nil, errors.New("已有一次 AI 提取在进行中（一次往返要几十秒），请等它结束；" +
			"中途改本地内容不受影响，本地美化不排队")
	}
	defer s.aiRunning.Store(false)

	if strings.TrimSpace(req.Text) == "" {
		return nil, errors.New("没有要交给 AI 的内容")
	}
	// 外发也吃本地同一道尺寸闸：本地拒收 2 MiB 的理由（读不动、也不该由工具硬扛）
	// 对外发同样成立。少了这一条，"超限"的文本会先原样送出机器、再在本地校验时失败——
	// 白外发一次，还把用户内容交给了一个本来就处理不了它的链路。
	if len(req.Text) > MaxInputBytes {
		return nil, fmt.Errorf("待交给 AI 的内容 %d 字节，超过单次上限 %d 字节；先截出你要处理的那一段",
			len(req.Text), MaxInputBytes)
	}
	agent, err := s.newAsker()
	if err != nil {
		return nil, err
	}
	// 用背景租约的 ctx，不是 context.Background()：这一路 ctx 直通 exec.CommandContext，
	// 关窗/停用能真的把 CLI 收掉；传 Background 就等于挂一条没有任何人能取消的几十秒调用。
	aiRes, err := agent.Ask(bg.Context(), aiagent.Request{
		Instruction: extractInstruction,
		Input:       req.Text,
	})
	if err != nil {
		return nil, err
	}

	// 模型按约定回报"没找到 JSON"时是一句人话哨兵，必须在进管线前就拦下：
	// 否则它会被宽松修复当成裸词 → 改写成字符串 "NO_JSON" → 变成"提取成功"，
	// 而用户拿到的是一句拒绝的话被伪装成了数据（单测实测抓到）。
	if strings.EqualFold(strings.TrimSpace(aiRes.Text), "NO_JSON") {
		return nil, fmt.Errorf("%w：模型回报这段文本里没有完整 JSON", ErrAINotJSON)
	}
	// 严格验收（见 ai.go 头注释）：不扫描、不修复、不猜——按提示词的契约逐块判。
	segs, note, reject := validateAIJSON(aiRes.Text)
	if reject != "" {
		// 明说而不是降级渲染：给用户一个"看着像 JSON 其实被 AI 改坏/编造"的结果，
		// 比报一句失败恶劣得多。
		return nil, fmt.Errorf("%w：%s", ErrAINotJSON, reject)
	}
	// 段渲染与统计仍走本地那一份算法（不另写），Unescape/SortKeys/Query 照常生效；
	// fromAI=true 让段头不许报"原第 N 行"——那是模型输出的行号，不是用户原文的行号。
	out := segmentsResult(segs, req, len(req.Text), 0,
		[]string{fmt.Sprintf("由本机 AI 提取：外发 %d 字节、模型返回 %d 字节、耗时 %dms",
			aiRes.BytesIn, aiRes.BytesOut, aiRes.ElapsedMs)}, false, true)
	if out == nil {
		return nil, fmt.Errorf("%w：验收后没有可交付的段（不该发生，说明校验与渲染口径不一致）", ErrAINotJSON)
	}
	if note != "" {
		out.Warnings = append(out.Warnings, note)
	}
	// 一份返回时也把 Kind 收成真实形状：顶层报 "multi" 会让用户以为原文里有多段
	if len(out.Segments) == 1 {
		out.Kind = out.Segments[0].Kind
	}
	return out, nil
}

// beautify 是完整管线：尺寸门 → 原样试解析 → 剔前缀重试 → 可选转义还原 → 渲染 → 统计。
//
// **先原样试一次**再考虑清洗，顺序是有意的：绝大多数粘贴本来就是合法 JSON，让它走最短
// 路径，同时保证清洗逻辑没机会去"改善"一份原本没问题的输入。
func beautify(req JsonFmtRequest) *JsonFmtResult {
	src := req.Text
	bytesIn := len(src)

	switch {
	case strings.TrimSpace(src) == "":
		return &JsonFmtResult{Error: "没有内容：贴一段 JSON 进来即可，粘贴后会自动格式化"}
	case bytesIn > MaxInputBytes:
		// 超限如实拒绝，绝不静默截断：截断后的 JSON 必然解析失败，报出来的位置还会
		// 指向"意外结束"，把用户引向完全错误的方向。
		return &JsonFmtResult{
			Error: fmt.Sprintf("输入 %d 字节，超过单次上限 %d 字节；这么大的 JSON 建议在编辑器里打开，或先截出你要看的那一段",
				bytesIn, MaxInputBytes),
			Stats: JsonFmtStats{BytesIn: bytesIn},
		}
	}

	root, perr, warnings := analyze(src)
	stripped, repaired := 0, 0
	var repairs []RepairStat
	if perr != nil {
		cleaned, n := cleanLogPrefixes(src)
		stripped = n
		if n > 0 {
			if root2, err2, w2 := analyze(cleaned); err2 == nil {
				src, root, perr = cleaned, root2, nil
				warnings = append(w2, fmt.Sprintf("已从 %d 行中剔掉日志前缀（时间戳/级别/线程段）后才解析成功", n))
			} else {
				// 两道都失败：报**清洗后文本**上的位置（那才是实际尝试解析的内容），
				// 并交代剥过前缀——行列可能与用户眼前数到的行号不一致，不说明就会
				// 让人怀疑工具读错了行。
				// src 一并换成 cleaned：偏移是 cleaned 上的偏移，行列若还拿原文换算，
				// 报出来的行号会指到日志前缀上去——用户按位置去找，永远找不到那个字符。
				src, perr = cleaned, err2
				warnings = append(w2, fmt.Sprintf(
					"已尝试剔除 %d 行日志前缀仍无法解析；下述位置基于清洗后的文本", n))
			}
		}
	}
	// 第三道：整段与剥前缀都失败，才进"扫描抽取所有 JSON 段"。放在最后是有意的——
	// 前两道成功时结果是**确定的一个值**，比"扫描器猜出几段"更可信，不能反过来。
	attemptsCapHit := false
	if perr != nil && req.ExtractAll {
		segs, attempts := extractSegments([]byte(src))
		attemptsCapHit = attempts >= maxScanAttempts
		if res := segmentsResult(segs, req, bytesIn, stripped, warnings, attemptsCapHit, false); res != nil {
			return res
		}
	}
	// 第四道：自动修复（宽松语法 + 缺失的闭合符）。排在前三道之后是硬要求——
	// 能原样解析的输入绝不会被改写；只有本来就读不了的文本才允许动它。
	if perr != nil {
		if fixed, stats, tail, ok := repairLoose(src); ok {
			if n2, e2, w2 := analyze(fixed); e2 == nil {
				src, root, perr, repairs, repaired = fixed, n2, nil, stats, tail
				warnings = append(w2, repairNotice(stats, tail))
			}
		}
	}
	if perr != nil {
		line, col := lineCol([]byte(src), perr.off)
		return &JsonFmtResult{
			Error:   perr.msg,
			Hint:    classifyFailure(perr.msg, perr.off, len(src), src),
			Line:    line,
			Col:     col,
			Snippet: snippet(src, perr.off),
			// 撞闸必须开口说：只报"键应为字符串"而不提"我在第 2 万个候选就停了"，
			// 用户会以为是自己 JSON 写错，实际是文本大到没继续扫——两种修法完全不同。
			DroppedPrefixLines: stripped,
			Warnings: func() []string {
				if attemptsCapHit {
					return append(warnings, fmt.Sprintf("候选尝试数已达上限 %d，已停止继续扫描（未必是输入本身的问题）", maxScanAttempts))
				}
				return warnings
			}(),
			Stats: JsonFmtStats{BytesIn: bytesIn},
		}
	}

	unwrapped := 0
	if req.Unescape {
		var n int
		root, n = unwrapStringified(root, 0)
		unwrapped = n
		if n > 0 {
			// 展开**改变结构**（字符串变成对象/数组），必须说出来：用户拿输出跟原日志
			// 逐字比对时，得知道这不是同一形状的东西。
			warnings = append(warnings, fmt.Sprintf(
				"展开 %d 处被字符串化的 JSON（输出结构与输入不同：原字段是转义文本，现已展开为真实节点）", n))
		}
	}

	// 替换（显式请求才做）：在渲染与统计之前应用于树，保证统计与查找反映最终输出，
	// 而不是"替换前的形状"。
	replaced := 0
	if req.Replace != nil {
		if cerr := checkReplace(*req.Replace); cerr != nil {
			return &JsonFmtResult{Error: cerr.Error(), Stats: JsonFmtStats{BytesIn: bytesIn}}
		}
		var cnt int
		root, cnt = applyReplace(root, *req.Replace)
		replaced = cnt
		warnings = append(warnings, replaceNotice(*req.Replace, cnt))
	}

	indent := normalizeIndent(req.Indent)
	out := render(root, indent, req.SortKeys)
	keys, nodes, maxDepth := walkStats(root)
	keyStats, matches, trunc := analyzeTree(root, "", 0, 0, req.Query)

	return &JsonFmtResult{
		OK:                 true,
		Output:             out,
		Kind:               root.kind.String(),
		Repaired:           repaired,
		Repairs:            repairs,
		Replaced:           replaced,
		DroppedPrefixLines: stripped,
		Unwrapped:          unwrapped,
		KeyStats:           keyStats,
		Matches:            matches,
		MatchTruncated:     trunc,
		Stats: JsonFmtStats{
			Keys:     keys,
			Nodes:    nodes,
			MaxDepth: maxDepth,
			BytesIn:  bytesIn,
			BytesOut: len(out),
			LinesOut: countLines(out),
		},
		Warnings: warnings,
	}
}

// countLines 数渲染文本的行数。
//
// 只接字符串、只有一个实现，就是为了杜绝"数行"这件事出现第二种写法：本轮的段行数一直
// 报 1，正是因为另一处把换行符写成了反斜杠加 n 两个字符的字面量（多层转义削掉一次），
// 于是 Count 恒为 0。改在这里统一算，红一次就是全线红，不会再静默撒谎。
func countLines(s string) int {
	return strings.Count(s, nl) + 1
}

// nl 是换行符本身。用字节拼接造出来，源码里不出现反斜杠字面量，
// 也就不会被任何一次粘贴/脚本改写吃掉一层转义而悄悄变成别的字符串。
var nl = string([]byte{10})

// normalizeIndent：0=压缩；其余钳进 1~8——把 200 当缩进宽度会撑出无意义的巨型输出。
func normalizeIndent(indent int) int {
	if indent <= 0 {
		return 0
	}
	return min(indent, 8)
}

// walkStats 从节点树上算结构概览。
//
// 刻意走树而不是复用解析途中的累加值：转义还原会改变树的形状，统计必须跟**最终输出**
// 的形状一致，否则前端显示的节点数是"展开之前"的，等于用数字撒谎。
func walkStats(n node) (keys, nodes, maxDepth int) {
	var walk func(node, int)
	walk = func(v node, depth int) {
		nodes++
		maxDepth = max(maxDepth, depth)
		for _, m := range v.members {
			keys++
			walk(m.value, depth+1)
		}
		for _, it := range v.items {
			walk(it, depth+1)
		}
	}
	walk(n, 1)
	return keys, nodes, maxDepth
}

// segmentsResult 把抽取到的多段渲染成结果。
//
// 顶层 Output 是各段拼接且**不加装饰文字**：复制出去要还是能直接用的 JSON，
// 不该夹着"—— 第 2 段 ——"这种给人看的东西。展示交给 Segments（带各自行号）。
//
// fromAI 只改两件事，且都是"不许撒谎"那一类：段头不再报"原第 N 行"（那是模型输出的
// 行号，与用户眼前那份原文无关），以及总结警告换成 AI 口径（不写"从文本中抽取"）。
// 渲染、统计、查找、替换全部复用同一条算法——为一条来源不同就重写一遍，迟早两份漂移。
func segmentsResult(segs []extracted, req JsonFmtRequest, bytesIn, stripped int, warnings []string, attemptsHit, fromAI bool) *JsonFmtResult {
	indent := normalizeIndent(req.Indent)
	out := make([]Segment, 0, len(segs))
	unwrappedTotal := 0
	acc := newStatsAcc()
	var matches []Match
	truncated := false
	segReplaced := 0

	for i, sg := range segs {
		n, u := sg.n, 0
		if req.Unescape {
			n, u = unwrapStringified(n, 0)
			unwrappedTotal += u
		}
		if req.Replace != nil {
			var cnt int
			n, cnt = applyReplace(n, *req.Replace)
			segReplaced += cnt
		}
		rendered := render(n, indent, req.SortKeys)
		keys, nodes, maxDepth := walkStats(n)
		segMatches, segTrunc := analyzeTreeInto(acc, n, "", i+1, sg.line, req.Query, matches, truncated)
		matches, truncated = segMatches, segTrunc
		// AI 来源的段不报行号：sg.line 是模型那份文本里的行，跟用户的原文没有关系，
		// 挂上"原第 N 行"就是让用户照着去数一根本不存在的对应。
		segLine := sg.line
		if fromAI {
			segLine = 0
		}
		out = append(out, Segment{
			Index: i + 1, Line: segLine, Kind: n.kind.String(), Output: rendered, Unwrapped: u,
			Stats: JsonFmtStats{
				Keys: keys, Nodes: nodes, MaxDepth: maxDepth,
				BytesOut: len(rendered), LinesOut: countLines(rendered),
			},
		})
	}

	if len(out) == 0 {
		return nil // 一段都没抽到：交回上层按"解析失败"报，不端出一个空成功
	}
	warn := warnings
	if fromAI {
		warn = append(warn, fmt.Sprintf("模型返回 %d 段，已逐块经本地解析器校验（不采信任何非 JSON 文字）", len(out)))
	} else {
		warn = append(warn, fmt.Sprintf("从文本中抽取并格式化 %d 段 JSON（各行号见分段明细）", len(out)))
	}
	if attemptsHit {
		warn = append(warn, fmt.Sprintf("候选尝试数已达上限 %d，后面可能还有未抽取的段", maxScanAttempts))
	}
	if len(out) == maxSegments {
		warn = append(warn, fmt.Sprintf("段数已达上限 %d，其余未处理", maxSegments))
	}

	return &JsonFmtResult{
		OK: len(out) > 0, Output: joinOutputs(out), Segments: out, Kind: "multi",
		Replaced: segReplaced,
		KeyStats: buildStats(acc), Matches: matches, MatchTruncated: truncated,
		DroppedPrefixLines: stripped, Unwrapped: unwrappedTotal,
		Stats: aggregateStats(out, bytesIn), Warnings: warn,
	}
}
