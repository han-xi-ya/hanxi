package jsonfmt

import "strings"

// 抽取模式：从任意文本里把**每一段**完整 JSON 找出来分别美化。
//
// 为什么不是"把日志前缀正则写得更全"：前缀形态是无穷的（zap 的 tab 分隔、log4j 的
// `[线程] 级别`、Go panic 的缩进栈、shell 里 `key={...}`、curl -v 的 `* Connected to`），
// 穷举必输。而"从一堆文本里认出合法 JSON 值"有确定判据——扫到 `{`/`[` 就试着解析一个
// 值，成了收下并从它结束处继续，败了前进一格。
//
// 代价是复杂度最坏 O(n²)：每个候选都可能向前扫很远才失败。所以带总工作量预算
// （scanWorkBudget）：超了就把手上已抽到的交出去并如实 warning，而不是让一次粘贴
// 变成不可中断的 CPU 绞杀。

// extracted 一段抽取结果：节点树 + 它在文本里的起始行。
type extracted struct {
	n    node
	line int
}

// extractSegments 按出现顺序扫描全部 JSON 值。返回段列表与消耗的工作量（尝试次数）。
//
// 预算单位是**候选数**而不是"扫过的字节距离"：失败往往在候选后一两字节就发生（`{“{`
// 只扫 2 字节），按字节算预算形同虚设，而每次尝试本身的常数开销才是真正会被放大的东西。
func extractSegments(src []byte) (segs []extracted, attempts int) {
	for i := 0; i < len(src) && len(segs) < maxSegments; {
		if src[i] != '{' && src[i] != '[' {
			i++
			continue
		}
		if attempts++; attempts > maxScanAttempts {
			return segs, attempts
		}
		n, end, err := parseValueAt(src, i)
		if err != nil {
			// 不跳过整段：失败的那个 `{` 里面可能真没有 JSON，但下一个 `{` 有。
			// 预算只在"扫得很远 yet 失败"时才起作用，正常文本碰不到。
			i++
			continue
		}
		if !worthExtracting(n) {
			// 假阳性防护：`oilStationV2.linux[31735]:` 里的 `[31735]`、数组下标 `[0]`
			// 都是**合法 JSON 数组**。照单全收的话，用户会看到"成功抽出 N 段"而内容
			// 全是日志里的计数碎片——那比老实报错更坏。
			i++
			continue
		}
		line, _ := lineCol(src, i)
		segs = append(segs, extracted{n: n, line: line})
		i = end
	}
	return segs, attempts
}

// worthExtracting 决定一个候选值算不算"值得交给用户的 JSON 段"。
//
// 取舍写明白：空容器与"单个标量的数组"（`[31735]`、`[0]`）不算——它们在日志里
// 几乎都是进程 pid、下标、计数,照收就会把一堆碎片当成果端出去；而真实业务日志里
// 的数组通常是对象列表（`[{...}]`）或多元素（`[1,2]`），这两类都放行。
// 代价是"只含一个数字的数组"这种真 JSON 会被漏抽——宁可漏,不可假。
func worthExtracting(n node) bool {
	switch n.kind {
	case kObject:
		return len(n.members) > 0
	case kArray:
		if len(n.items) == 0 {
			return false
		}
		if len(n.items) == 1 && isScalarNode(n.items[0]) {
			return false
		}
		return true
	default:
		return false
	}
}

func isScalarNode(n node) bool { return n.kind != kObject && n.kind != kArray }

// aggregateStats 把多段的统计并成一份顶层数字（顶层 Stats 与 Segments 必须同源，
// 不能各算各的——否则汇总行与分段明细会对不上，那是最容易骗人的两处数字）。
func aggregateStats(segs []Segment, bytesIn int) JsonFmtStats {
	var acc JsonFmtStats
	for _, s := range segs {
		acc.Keys += s.Stats.Keys
		acc.Nodes += s.Stats.Nodes
		acc.BytesOut += s.Stats.BytesOut
		acc.LinesOut += s.Stats.LinesOut
		if s.Stats.MaxDepth > acc.MaxDepth {
			acc.MaxDepth = s.Stats.MaxDepth
		}
	}
	acc.BytesIn = bytesIn
	return acc
}

// joinOutputs 拼接各段输出。刻意不加任何装饰文字：复制出去的东西必须仍是能用的文本
// （用户在编辑器里粘的是 JSON，不是"—— 第 2 段 ——"）。
func joinOutputs(segs []Segment) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, s.Output)
	}
	return strings.Join(parts, "\n\n")
}
