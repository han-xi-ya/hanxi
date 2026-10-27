package jsonfmt

import (
	"fmt"
	"strings"

	"hanxi/internal/extapi"
)

// JsonFmtService 暴露给前端的 JSON 美化服务。
//
// 无状态、纯计算：一次调用一次返回，不发事件、不留任务。全部逻辑在 beautify 这个
// 纯函数里，JsonFmtService 只负责接调用门——这样清洗/解析/渲染的行为不必启动 Wails 就能逐条断言。
type JsonFmtService struct {
	holder *extapi.LeaseHolder
}

// NewService 创建服务实例；holder 由模块构造期建好并经 SetGate 接上调用门。
func NewService(holder *extapi.LeaseHolder) *JsonFmtService {
	return &JsonFmtService{holder: holder}
}

// Beautify 清洗并格式化一段 JSON。
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
		if res := segmentsResult(segs, req, bytesIn, stripped, warnings, attemptsCapHit); res != nil {
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
			Error:              perr.msg,
			Hint:               classifyFailure(perr.msg, perr.off, len(src), src),
			Line:               line,
			Col:                col,
			Snippet:            snippet(src, perr.off),
			DroppedPrefixLines: stripped,
			// 撞闸必须开口说：只报"键应为字符串"而不提"我在第 2 万个候选就停了"，
			// 用户会以为是自己 JSON 写错，实际是文本大到没继续扫——两种修法完全不同。
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
			LinesOut: strings.Count(out, "\n") + 1,
		},
		Warnings: warnings,
	}
}

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
func segmentsResult(segs []extracted, req JsonFmtRequest, bytesIn, stripped int, warnings []string, attemptsHit bool) *JsonFmtResult {
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
		out = append(out, Segment{
			Index: i + 1, Line: sg.line, Kind: n.kind.String(), Output: rendered, Unwrapped: u,
			Stats: JsonFmtStats{
				Keys: keys, Nodes: nodes, MaxDepth: maxDepth,
				BytesOut: len(rendered), LinesOut: strings.Count(rendered, "\\n") + 1,
			},
		})
	}

	if len(out) == 0 {
		return nil // 一段都没抽到：交回上层按"解析失败"报，不端出一个空成功
	}
	warn := append(warnings, fmt.Sprintf("从文本中抽取并格式化 %d 段 JSON（各行号见分段明细）", len(out)))
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
