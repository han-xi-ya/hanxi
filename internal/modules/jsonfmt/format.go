package jsonfmt

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// 手写递归下降，而不是 encoding/json 二趟。三个理由各自都足以否决"标准库
// Unmarshal 到 any 再 Marshal"这条看起来最省事的路上：
//
//  1. 数字必须走原始字面量。any 路线会把数字变成 float64，19 位雪花 ID 尾数被
//     静默改写（JS 的 JSON.parse 犯的是同一个错，也正是本模块不放前端的原因）；
//  2. map[string]any 会丢键的原始顺序，还会把重复键合并掉——日志排障时字段顺序
//     是信息，而"同一份日志里同名键出现两次"恰恰是根因，不能合并；
//  3. encoding/json 的 Marshal 默认把 & < > 转义成 &…，美化带 URL 的日志
//     反而更难看。这里字符串一律按输入的原样字面量回写，一个字符都不多改。

type valueKind uint8

const (
	kInvalid valueKind = iota
	kObject
	kArray
	kString
	kNumber
	kLiteral // true / false / null
)

func (k valueKind) String() string {
	switch k {
	case kObject:
		return "object"
	case kArray:
		return "array"
	default:
		return "scalar"
	}
}

type member struct {
	keyLit string // 输入里的原始键字面量（含引号），输出时原样回写
	key    string
	value  node
}

type node struct {
	kind    valueKind
	lit     string // 标量的原始字面量；字符串含两侧引号
	members []member
	items   []node
	line    int
	col     int
}

// parseErr 带输入偏移，供上层换算成行列与片段。
type parseErr struct {
	msg string
	off int
}

func (e *parseErr) Error() string { return e.msg }

type parser struct {
	src      []byte
	pos      int
	warnings []string
	warned   map[string]bool
}

// analyze 解析输入并收集警告。root 仅在 perr == nil 时有效。
// 结构统计不在这里做——统一由 walkStats 从节点树上算，一份实现（见 service.go）。
func analyze(src string) (root node, perr *parseErr, warnings []string) {
	p := &parser{src: []byte(src), warned: map[string]bool{}}

	p.skipSpace()
	root, err := p.parseValue(0)
	if err != nil {
		return node{}, err, p.warnings
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return node{}, p.errAt(p.pos, "JSON 之后还有多余内容（可能混进了别的日志行）"), p.warnings
	}
	return root, nil, p.warnings
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) parseValue(depth int) (node, *parseErr) {
	if depth > maxDepth {
		return node{}, p.errAt(p.pos, fmt.Sprintf("嵌套深度超过上限 %d（畸形或超大输入）", maxDepth))
	}
	if p.pos >= len(p.src) {
		return node{}, p.errAt(p.pos, "内容意外结束")
	}
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.parseObject(depth)
	case c == '[':
		return p.parseArray(depth)
	case c == '"':
		lit, err := p.scanString()
		if err != nil {
			return node{}, err
		}
		return node{kind: kString, lit: lit, line: p.lineAt(p.pos), col: p.colAt(p.pos)}, nil
	case c == '-' || (c >= '0' && c <= '9'):
		lit, err := p.scanNumber()
		if err != nil {
			return node{}, err
		}
		return node{kind: kNumber, lit: lit, line: p.lineAt(p.pos), col: p.colAt(p.pos)}, nil
	case c == 't' && p.litAt("true"):
		p.pos += 4
		return node{kind: kLiteral, lit: "true"}, nil
	case c == 'f' && p.litAt("false"):
		p.pos += 5
		return node{kind: kLiteral, lit: "false"}, nil
	case c == 'n' && p.litAt("null"):
		p.pos += 4
		return node{kind: kLiteral, lit: "null"}, nil
	default:
		return node{}, p.errAt(p.pos, fmt.Sprintf("意外的字符 %q（JSON 值不能这样开头）", rune(c)))
	}
}

func (p *parser) litAt(want string) bool {
	return p.pos+len(want) <= len(p.src) && string(p.src[p.pos:p.pos+len(want)]) == want
}

func (p *parser) parseObject(depth int) (node, *parseErr) {
	start := p.pos
	p.pos++ // '{'
	n := node{kind: kObject, line: p.lineAt(start), col: p.colAt(start)}
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return n, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != '"' {
			return node{}, p.errAt(p.pos, "对象的键必须是字符串")
		}
		keyPos := p.pos
		keyLit, err := p.scanString()
		if err != nil {
			return node{}, err
		}
		key := unquoteLit(keyLit)
		if key == "" && len(keyLit) > 0 {
			return node{}, p.errAt(keyPos, fmt.Sprintf("键的写法不合法：%s", keyLit))
		}
		if p.seen(n.members, key) {
			p.warnOnce("dup:"+key, fmt.Sprintf("第 %d 行附近有重复键 %q：已按原样保留（Go map 会静默合并成一条，那会把日志里的真实问题抹平）", p.lineAt(keyPos), key))
		}

		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return node{}, p.errAt(p.pos, "键后缺少 ':'")
		}
		p.pos++

		p.skipSpace()
		val, verr := p.parseValue(depth + 1)
		if verr != nil {
			return node{}, verr
		}
		n.members = append(n.members, member{keyLit: keyLit, key: key, value: val})

		p.skipSpace()
		if p.pos >= len(p.src) {
			return node{}, p.errAt(p.pos, "对象未闭合")
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return n, nil
		default:
			return node{}, p.errAt(p.pos, "对象成员之间需要 ','（或对象应以 '}' 结束）")
		}
	}
}

func (p *parser) parseArray(depth int) (node, *parseErr) {
	start := p.pos
	p.pos++ // '['
	n := node{kind: kArray, line: p.lineAt(start), col: p.colAt(start)}
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return n, nil
	}
	for {
		p.skipSpace()
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return node{}, err
		}
		n.items = append(n.items, val)

		p.skipSpace()
		if p.pos >= len(p.src) {
			return node{}, p.errAt(p.pos, "数组未闭合")
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return n, nil
		default:
			return node{}, p.errAt(p.pos, "数组元素之间需要 ','（或数组应以 ']' 结束）")
		}
	}
}

// scanNumber 按 JSON 文法扫出数字字面量。刻意**不转成任何数值类型**：
// 扫完之后它是原样回写的文本，精度因此与输入逐字节一致。
func (p *parser) scanNumber() (string, *parseErr) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.src) || !isDigit(p.src[p.pos]) {
		return "", p.errAt(start, "数字写法不完整")
	}
	if p.src[p.pos] == '0' {
		p.pos++
	} else {
		for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.src) || !isDigit(p.src[p.pos]) {
			return "", p.errAt(start, "小数点后缺少数字")
		}
		for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.src) || !isDigit(p.src[p.pos]) {
			return "", p.errAt(start, "指数部分缺少数字")
		}
		for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
			p.pos++
		}
	}
	lit := string(p.src[start:p.pos])
	// 紧跟字母（1abc、0x1f）说明这不是 JSON 数字，多半是日志里的裸值。
	if p.pos < len(p.src) && (isLetter(p.src[p.pos]) || p.src[p.pos] == '.') {
		return "", p.errAt(start, fmt.Sprintf("无法解析 %s（JSON 数字不能这样写）", lit+string(p.src[p.pos])))
	}
	return lit, nil
}

// scanString 扫字符串字面量，返回**含引号的原文**。
func (p *parser) scanString() (string, *parseErr) {
	start := p.pos
	p.pos++ // 开引号
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\\':
			if p.pos+1 >= len(p.src) {
				return "", p.errAt(start, "字符串以孤立的反斜杠结尾")
			}
			next := p.src[p.pos+1]
			switch next {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				p.pos += 2
			case 'u':
				if p.pos+6 > len(p.src) {
					return "", p.errAt(start, "\\u 转义不完整")
				}
				for i := p.pos + 2; i < p.pos+6; i++ {
					if !isHex(p.src[i]) {
						return "", p.errAt(start, fmt.Sprintf("\\u 后有非十六进制字符 %q", rune(p.src[i])))
					}
				}
				p.pos += 6
			default:
				return "", p.errAt(p.pos, fmt.Sprintf("非法转义 \\%c", next))
			}
		case c == '"':
			p.pos++
			return string(p.src[start:p.pos]), nil
		case c < 0x20:
			// 日志里极常见：消息里夹了裸换行/制表符。报出来比含糊失败有用得多。
			return "", p.errAt(p.pos, fmt.Sprintf("字符串内含未转义的控制字符 0x%02X（日志消息里的裸换行要写成 \\n）", c))
		default:
			p.pos++
		}
	}
	return "", p.errAt(start, "字符串未闭合")
}

// parseValueAt 从 off 起解析**一个** JSON 值，返回结束偏移（失败时返回已扫到的位置）。
// 抽取模式的地基：analyze 要求"解析完恰好到文本尾"，而日志里 JSON 后面必然还跟着别的。
//
// **收 []byte 而非 string**：抽取会对每个 `{`/`[` 候选调一次本函数，若在函数里
// []byte(src) 就是"每次试解析都重抄整份输入"——实测 20 万个左花括号跑 57 秒、
// 2 万个 0.85 秒（教科书级平方曲线）。字节切片由调用方转一次、全程共享。
func parseValueAt(src []byte, off int) (node, int, *parseErr) {
	p := &parser{src: src, pos: off, warned: map[string]bool{}}
	n, err := p.parseValue(0)
	if err != nil {
		return node{}, p.pos, err
	}
	return n, p.pos, nil
}

// ---------- 渲染 ----------

// render 输出结果。indent<=0 为压缩形。两种模式都只用原始字面量，不改一个字符。
func render(n node, indent int, sortKeys bool) string {
	var b strings.Builder
	if indent <= 0 {
		writeCompact(&b, n, sortKeys)
		return b.String()
	}
	writePretty(&b, n, 0, indent, sortKeys)
	return b.String()
}

func membersToRender(members []member, sortKeys bool) []member {
	if !sortKeys {
		return members
	}
	out := make([]member, len(members))
	copy(out, members)
	sort.SliceStable(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func writeCompact(b *strings.Builder, n node, sortKeys bool) {
	switch n.kind {
	case kObject:
		b.WriteByte('{')
		for i, m := range membersToRender(n.members, sortKeys) {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(m.keyLit)
			b.WriteByte(':')
			writeCompact(b, m.value, sortKeys)
		}
		b.WriteByte('}')
	case kArray:
		b.WriteByte('[')
		for i, it := range n.items {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCompact(b, it, sortKeys)
		}
		b.WriteByte(']')
	default:
		b.WriteString(n.lit)
	}
}

func writePretty(b *strings.Builder, n node, level, indent int, sortKeys bool) {
	pad := strings.Repeat(" ", level*indent)
	inner := strings.Repeat(" ", (level+1)*indent)
	switch n.kind {
	case kObject:
		if len(n.members) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		members := membersToRender(n.members, sortKeys)
		for i, m := range members {
			b.WriteString(inner)
			b.WriteString(m.keyLit)
			b.WriteString(": ")
			writePretty(b, m.value, level+1, indent, sortKeys)
			if i < len(members)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		b.WriteByte('}')
	case kArray:
		if len(n.items) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, it := range n.items {
			b.WriteString(inner)
			writePretty(b, it, level+1, indent, sortKeys)
			if i < len(n.items)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		b.WriteByte(']')
	default:
		b.WriteString(n.lit)
	}
}

// ---------- 位置与文本工具 ----------

func (p *parser) lineAt(off int) int {
	line, _ := lineCol(p.src, off)
	return line
}

func (p *parser) colAt(off int) int {
	_, col := lineCol(p.src, off)
	return col
}

// lineCol 把字节偏移换算成 1 基行列。**包级唯一一份**：解析器内部与失败结果都要给
// 行列，两处各写一遍迟早给出两个互相矛盾的行号。列按 rune 计，中文不会把列数撑歪。
func lineCol(src []byte, off int) (line, col int) {
	if off > len(src) {
		off = len(src)
	}
	if off < 0 {
		off = 0
	}
	line = 1 + strings.Count(string(src[:off]), "\n")
	if nl := strings.LastIndexByte(string(src[:off]), '\n'); nl >= 0 {
		col = utf8.RuneCount(src[nl+1:off]) + 1
	} else {
		col = utf8.RuneCount(src[:off]) + 1
	}
	return line, col
}

func (p *parser) errAt(off int, msg string) *parseErr {
	return &parseErr{msg: msg, off: off}
}

// snippet 取出错位置附近的单行化片段，并用 ^ 指出列。
func snippet(src string, off int) string {
	if off > len(src) {
		off = len(src)
	}
	lo := off - snippetRadius
	if lo < 0 {
		lo = 0
	}
	hi := off + snippetRadius
	if hi > len(src) {
		hi = len(src)
	}
	prefix := ""
	if lo > 0 {
		prefix = "…"
	}
	suffix := ""
	if hi < len(src) {
		suffix = "…"
	}
	seg := strings.ReplaceAll(src[lo:hi], "\n", "⏎")
	seg = strings.ReplaceAll(seg, "\t", "⇥")
	// 标记列按 rune 计，避免中文把箭头指偏。
	mark := utf8.RuneCountInString(strings.ReplaceAll(src[lo:off], "\n", "⏎"))
	indentRunes := utf8.RuneCountInString(prefix)
	return prefix + seg + suffix + "\n" + strings.Repeat(" ", indentRunes+mark) + "^"
}

func (n node) hasMembers() bool { return len(n.members) > 0 || len(n.items) > 0 }

func (p *parser) seen(members []member, key string) bool {
	for _, m := range members {
		if m.key == key {
			return true
		}
	}
	return false
}

func (p *parser) warnOnce(id, msg string) {
	if p.warned[id] {
		return
	}
	p.warned[id] = true
	p.warnings = append(p.warnings, msg)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isHex(c byte) bool { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }

// unquoteLit 把键字面量解成可比较的字符串（只为重复键检测与排序，输出仍用原文）。
func unquoteLit(lit string) string {
	if len(lit) < 2 || lit[0] != '"' {
		return ""
	}
	var b strings.Builder
	for i := 1; i < len(lit)-1; i++ {
		c := lit[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(lit)-1 {
			return ""
		}
		switch lit[i] {
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			if i+4 >= len(lit)-1 {
				return ""
			}
			var v int
			if _, err := fmt.Sscanf(lit[i+1:i+5], "%04x", &v); err != nil {
				return ""
			}
			b.WriteRune(rune(v))
			i += 4
		default:
			return ""
		}
	}
	return b.String()
}
