package jsonfmt

import "strings"

// 宽松 JSON → 严格 JSON。
//
// 为什么需要它：日志里打出来的"JSON"常常根本不是 JSON——Python 打印用单引号、
// Go 的 map 打印键不带引号、人手写的对象带注释和尾逗号、JS 的 undefined 混进来。
// 这些对严格解析器一律"语法错误"，而用户的意图毫无歧义。
//
// 三条纪律：
//  1. **只在原样解析与剥前缀都失败之后才跑**——本来就能解析的输入永远不会被改写；
//  2. 每改一处都按类计数并随结果交出（Repairs），UI 上写明"已自动修复 N 处"。
//     悄悄把 `{a:1}` 变成 `{"a":1}` 而不告诉用户，正是他以后不信这工具的理由；
//  3. 只改**语法外壳**，不假装能保住一切：NaN/Infinity 无法进 JSON，转成 null 并单独
//     计一类，因为"这个数字丢了"必须可见。
//
// 本文件刻意不使用任何字符字面量写反斜杠/引号/换行——这些转义在
// "编辑器→脚本→文件"多层传递里反复被吃掉（本仓已踩四次，症状都是语法错）。
// 一律用 byte 常量与十进制值表达。

const (
	btBackslash = byte(92)
	btQuote     = byte(34)
	btSQuote    = byte(39)
	btBraceO    = byte('{')
	btBraceC    = byte('}')
	btBracketO  = byte('[')
	btBracketC  = byte(']')
	btColon     = byte(':')
	btComma     = byte(',')
	btSlash     = byte('/')
	btStar      = byte('*')
	btEqual     = byte('=')
)

// 修复类别名，同时是给人看的标签（顺序见 repairs 汇总处）。
const (
	repSingleQuote   = "单引号改双引号"
	repBareKey       = "无引号的键补上引号"
	repTrailingComma = "删掉多余的尾逗号"
	repComment       = "去掉注释"
	repPyLiteral     = "True/False/None/undefined 改为 JSON 字面量"
	repNaNToNull     = "NaN/Infinity 改为 null（原值无法进 JSON）"
	repBareWordValue = "裸词按字符串处理"
	repEqSeparator   = "键值分隔符 = 改为 :"
)

// relax 把宽松写法改写成严格 JSON。fixed 与 src 相同则 changed=false（无事可做）。
// 遇到不敢猜的形态（引号/注释未闭合）直接 changed=false，交回上层按解析失败处理。
func relax(src string) (fixed string, repairs []RepairStat, changed bool) {
	s := []byte(src)
	var b strings.Builder
	b.Grow(len(s) + 32)
	counts := map[string]int{}
	var stack []byte

	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == btSlash && i+1 < len(s) && s[i+1] == btSlash: // 行注释
			j := strings.IndexByte(src[i:], 10)
			if j < 0 {
				j = len(s) - i
			}
			counts[repComment]++
			i += j
			continue

		case c == btSlash && i+1 < len(s) && s[i+1] == btStar: // 块注释
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return "", nil, false
			}
			counts[repComment]++
			i += 2 + end + 2
			continue

		case c == btSQuote: // 单引号字符串 → 双引号
			body, next, ok := scanQuoted(s, i, btSQuote)
			if !ok {
				return "", nil, false
			}
			writeJSONString(&b, body)
			counts[repSingleQuote]++
			i = next
			continue

		case c == btQuote: // 双引号字符串：原样搬，一个字节都不动
			body, next, ok := scanQuoted(s, i, btQuote)
			if !ok {
				return "", nil, false
			}
			b.WriteByte(btQuote)
			b.Write(body)
			b.WriteByte(btQuote)
			i = next
			continue

		case c == btEqual && inObject(stack): // Python/Go 风格 a=1
			b.WriteByte(btColon)
			counts[repEqSeparator]++
			i++
			continue

		case c == btComma: // 尾逗号：后面紧跟 } 或 ] 就丢掉
			j := i + 1
			for j < len(s) && isSpaceByte(s[j]) {
				j++
			}
			if j < len(s) && (s[j] == btBraceC || s[j] == btBracketC) {
				counts[repTrailingComma]++
				i++
				continue
			}
			b.WriteByte(c)
			i++
			continue

		case c == btBraceO || c == btBracketO:
			stack = append(stack, c)
			b.WriteByte(c)
			i++
			continue

		case c == btBraceC || c == btBracketC:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			b.WriteByte(c)
			i++
			continue

		case c == btColon:
			b.WriteByte(c)
			i++
			continue

		case isWordChar(c): // 裸词：可能是键，也可能是值
			j := i
			for j < len(s) && isWordChar(s[j]) {
				j++
			}
			word := string(s[i:j])
			k := j
			for k < len(s) && isSpaceByte(s[k]) {
				k++
			}
			isKey := k < len(s) && (s[k] == btColon || (s[k] == btEqual && inObject(stack)))
			if isKey {
				b.WriteByte(btQuote)
				b.WriteString(word)
				b.WriteByte(btQuote)
				counts[repBareKey]++
			} else if isNumericWord(word) {
				// 数字必须原样搬走。早期实现把值位上的 1 也当"裸词加引号"，
				// 于是 {b: 2} 修成了 {"b":"2"}——**数字变字符串是改数据含义**，
				// 与本文件"只改语法外壳"的纪律直接冲突，测试当场抓到。
				b.WriteString(word)
			} else if lit, kind, ok := literalOf(word); ok {
				b.WriteString(lit)
				counts[kind]++
			} else {
				b.WriteByte(btQuote)
				b.WriteString(word)
				b.WriteByte(btQuote)
				counts[repBareWordValue]++
			}
			i = j
			continue

		default:
			b.WriteByte(c)
			i++
			continue
		}
	}

	out := b.String()
	if out == src {
		return out, nil, false
	}
	return out, collectRepairs(counts), true
}

// literalOf 认那些"写法不对但意思明确"的字面量。认不出的一律交回上层当字符串。
func literalOf(word string) (jsonLit, repKind string, ok bool) {
	switch word {
	case "True":
		return "true", repPyLiteral, true
	case "False":
		return "false", repPyLiteral, true
	case "None":
		return "null", repPyLiteral, true
	case "undefined":
		return "null", repPyLiteral, true
	case "NaN", "nan", "Infinity", "-Infinity", "inf", "-inf":
		return "null", repNaNToNull, true
	default:
		return "", "", false
	}
}

// collectRepairs 按固定顺序输出：map 遍历无序会让 UI 文案每次跳动，
// 也让"同一个输入两次结果不同"这种鬼事看起来像 bug。
func collectRepairs(counts map[string]int) []RepairStat {
	order := []string{
		repSingleQuote, repBareKey, repTrailingComma, repComment,
		repPyLiteral, repNaNToNull, repBareWordValue, repEqSeparator,
	}
	out := make([]RepairStat, 0, len(order))
	for _, kind := range order {
		if n := counts[kind]; n > 0 {
			out = append(out, RepairStat{Kind: kind, Count: n})
		}
	}
	return out
}

// scanQuoted 从 s[start]（引号）扫到配对的同种引号，返回不含首尾引号的内容。
// 反斜杠后一字节无条件跳过，所以 `\n`、`\"` 这类序列保持原样。
func scanQuoted(s []byte, start int, quote byte) (body []byte, next int, ok bool) {
	i := start + 1
	var out []byte
	for i < len(s) {
		c := s[i]
		if c == btBackslash {
			if i+1 >= len(s) {
				return nil, 0, false
			}
			out = append(out, c, s[i+1])
			i += 2
			continue
		}
		if c == quote {
			return out, i + 1, true
		}
		out = append(out, c)
		i++
	}
	return nil, 0, false
}

// writeJSONString 把单引号内的原文写成合法 JSON 字符串字面量：
// 双引号补转义、`\'` 反成裸单引号（JSON 里它不需要转义）、反斜杠本身补齐转义，
// 其余转义序列原样保留。
func writeJSONString(b *strings.Builder, body []byte) {
	b.WriteByte(btQuote)
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != btBackslash {
			if c == btQuote {
				b.WriteByte(btBackslash)
			}
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(body) { // 结尾孤立反斜杠：补成合法的双反斜杠
			b.WriteByte(btBackslash)
			b.WriteByte(btBackslash)
			continue
		}
		nx := body[i+1]
		switch nx {
		case btSQuote: // \' 在 JSON 里是非法转义，还原成裸单引号
			b.WriteByte(btSQuote)
		case btQuote, btBackslash, btSlash, 'b', 'f', 'n', 'r', 't', 'u':
			b.WriteByte(btBackslash)
			b.WriteByte(nx)
		default: // 未知转义：把反斜杠本身转义掉，保住后面的字符不丢
			b.WriteByte(btBackslash)
			b.WriteByte(btBackslash)
			b.WriteByte(nx)
		}
		i++
	}
	b.WriteByte(btQuote)
}

func inObject(stack []byte) bool {
	return len(stack) > 0 && stack[len(stack)-1] == btBraceO
}

func isSpaceByte(c byte) bool {
	return c == 32 || c == 9 || c == 10 || c == 13
}

// isWordChar：标识符与点/负号外的数值字符。点单独放过会打断 1.5 这类值，
// 所以点也算词字符，由 literalOf 与"加引号"分支共同兜住。
func isWordChar(c byte) bool {
	return c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// isNumericWord 判断一个裸词是否本来就是 JSON 数字形状。
// 只认"以数字或点开头 + 全部字符属于 [0-9.eE]"；`e5`、`1x` 这类不当数字，
// 让它们走"裸词加引号"或交由严格解析器报错，而不是在这里猜。
func isNumericWord(w string) bool {
	if w == "" {
		return false
	}
	if !(w[0] == '.' || (w[0] >= '0' && w[0] <= '9')) {
		return false
	}
	hasDigit := false
	for i := 0; i < len(w); i++ {
		c := w[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '.' || c == 'e' || c == 'E':
		default:
			return false
		}
	}
	return hasDigit
}
