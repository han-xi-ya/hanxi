package jsonfmt

import (
	"regexp"
	"strings"
)

// 日志前缀剔除。
//
// 机主的一等诉求：从日志里复制出来的 JSON，每行开头都挂着时间戳/级别/线程名，
// 粘进任何 JSON 工具都是一句"解析失败"。这里把这些前缀按行剥掉。
//
// 但"剥前缀"天生危险——同一形态 `[xxx]` 既可能是 `[worker-1]`，也可能是数组行
// `[true]`。所以本文件有三道硬保护，一条比一条重要：
//  1. **只在行首匹配**已知前缀形态，绝不碰行中/行尾（值里出现 "INFO" 不受影响）；
//  2. 括号型规则**排除** JSON 内容字符（引号、逗号、冒号、花括号），于是 `[1,2]`、
//     `["a"]`、`{"k"` 都不会被当成前缀段；
//  3. **剥空即回退**：某行一旦剥到只剩空白，判定为误伤（`[true]` 这类整行就是数组），
//     该行原样保留。这条比"行首是 { 或 [ 就整行不碰"精确得多——后者会把
//     `[2026-10-26 10:11:12] [INFO] {"a":1}` 这种以方括号时间开头、确实该剥的行一起放过。
//
// 最后一道兜底在 service 层：**清洗后的文本仍解析不过，就退回原文报错**——
// 宁可承认"我剥不动"，也不能把用户的数据洗坏之后交付一个漂亮但错的东西。

// prefixRules 按锚定行首的形态排列，循环套用直到某行不再变化（有次数上限）。
// 括号段的字符类刻意排除 JSON 结构字符——见上面第 2 条保护。
var prefixRules = []*regexp.Regexp{
	regexp.MustCompile(`^\s+`),
	regexp.MustCompile(`^\[[^\[\]{}":,]{1,64}\][\s,|>*-]*`),
	regexp.MustCompile(`^\([^\[\]{}":,]{1,64}\)[\s,|>*-]*`),
	regexp.MustCompile(`^<[^<>\[\]{}":,]{1,64}>[\s,|>*-]*`),
	// ISO8601 / 常见 Go 日志时间，含带括号的形态
	regexp.MustCompile(`^\[?\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:?\d{2})?\]?[\s,|>*-]*`),
	regexp.MustCompile(`^\d{4}/\d{2}/\d{2}[ T]?\d{1,2}:\d{2}:\d{2}[\s,|>*-]*`),
	regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(\.\d{1,9})?[\s,|>*-]*`),
	regexp.MustCompile(`(?i)^(trace|debug|info|information|notice|warn|warning|err|error|fatal|panic|critical|severe)\b[\s,|>*-]*`),
	regexp.MustCompile(`(?i)^(level|lvl|severity)[=:]\s*\S+\s+`),
	regexp.MustCompile(`[\w.\-/]*\.(go|js|ts|tsx|jsx|py|java|kt|c|cc|cpp|h|hpp|rs|rb|php|swift|scala):\d+[\s,|):>-]*`),
	regexp.MustCompile(`^[|>-]+\s*`),
}

const maxPrefixPasses = 8

// cleanLogPrefixes 返回清洗后的文本、被剥掉前缀的行数、以及被丢弃的纯前缀行数。
func cleanLogPrefixes(src string) (cleaned string, stripped int) {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}

		cur, changed := line, false
		for range maxPrefixPasses {
			next, hit := stripOnce(cur)
			if !hit {
				break
			}
			cur = next
			changed = true
			// 剥过头了：整行空掉说明这行本来就只是内容（或全行是前缀噪声）。
			// 全行是前缀的情况交回上层按"噪声行"处理；这里先不制造空行误删。
			if strings.TrimSpace(cur) == "" {
				cur = line
				changed = false
				break
			}
		}
		if changed {
			stripped++
		}
		out = append(out, cur)
	}
	return strings.Join(out, "\n"), stripped
}

// stripOnce 套一轮规则；返回新行与"是否真的剥掉了非空白内容"。
func stripOnce(line string) (string, bool) {
	// 只在行首有实质内容可剥时才进入循环，避免把纯缩进当作剥除。
	for _, re := range prefixRules {
		loc := re.FindStringIndex(line)
		if loc == nil || loc[0] != 0 {
			continue
		}
		if loc[1] == 0 {
			continue
		}
		// 纯空白命中（^\s+ 之类）只挪缩进，不算"剥掉了前缀"——否则每行都会被
		// 记成已处理，dropped 计数直接失真。
		if strings.TrimSpace(line[:loc[1]]) == "" {
			return line[loc[1]:], false
		}
		return line[loc[1]:], true
	}
	return line, false
}

// ---------- 转义还原 ----------

// unwrapStringified 把"被字符串化的 JSON"就地展开：日志里极常见
//
//	{"body":"{\"orderId\":123}"}
//
// 这种形态——外层能解析，但内层是一坨转义文本，看着等于没美化。展开后结构改变，
// 所以必须计数并写进 warnings，而不是偷偷做掉。depth 限 3 层（见 maxUnwrapDepth）。
func unwrapStringified(n node, depth int) (node, int) {
	if depth >= maxUnwrapDepth {
		return n, 0
	}
	switch n.kind {
	case kString:
		inner, ok := parseAsNode(n.lit)
		if !ok {
			return n, 0
		}
		inner2, more := unwrapStringified(inner, depth+1)
		return inner2, 1 + more
	case kObject:
		total := 0
		out := make([]member, len(n.members))
		for i, m := range n.members {
			v, cnt := unwrapStringified(m.value, depth+1)
			// 必须分成两句：写成 `m.value, out[i] = v, m` 时右侧的 m 在赋值前就
			// 被求值成旧副本，out[i] 拿到的还是没有展开的那份——计数报 1、输出不变，
			// 实测才看得出来（诊断跑过一次，别靠眼看这段）。
			m.value = v
			out[i] = m
			total += cnt
		}
		n.members = out
		return n, total
	case kArray:
		total := 0
		out := make([]node, len(n.items))
		for i, it := range n.items {
			v, cnt := unwrapStringified(it, depth+1)
			out[i] = v
			total += cnt
		}
		n.items = out
		return n, total
	default:
		return n, 0
	}
}

// parseAsNode 尝试把一个字符串字面量当成一段嵌套 JSON 解析。
// 刻意要求解出来的文本以 { 或 [ 开头：否则 "123" 这类普通字符串会被"顺手"变成数字，
// 那是改变数据含义，不是美化。
func parseAsNode(lit string) (node, bool) {
	plain := unquoteLit(lit)
	if plain == "" {
		return node{}, false
	}
	trimmed := strings.TrimSpace(plain)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return node{}, false
	}
	root, perr, _ := analyze(trimmed)
	if perr != nil || !root.hasMembers() {
		return node{}, false
	}
	return root, true
}
