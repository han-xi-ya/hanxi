package jsonfmt

// AI 一键美化的任务契约与**严格验收**——纪律从 ExtractByAI 一代原样继承
// （#126/#133 的学费不能白交），产物面适配 83dfb6b 简化后的地基：
// 不再回 Segment/Stats 明细，多段就渲染后以空行拼接，界面吃现成的 Output。
//
// 为什么验收不复用"扫描"：实测过——模型的拒绝语里会自带一个它编造的示例
// 对象（"这段文本里没有 JSON（形如 {"key":"value"} 的那样才算）"），走散文
// 扫描会把那个例子当提取成功端上桌。所以只按**提示词的契约**逐块验收：
// 整份先判，判不过才按空行切块，每块自身必须是完整 JSON——不扫描、不修复、不猜。
// "值不值得当结果"（空容器/裸标量不算成果）仍是这文件里唯一的一份判据，
// 不与任何第二裁判商量。

import (
	"errors"
	"fmt"
	"strings"
)

// extractInstruction 写死在后端：让"只提取、不改数值、不解释"成为接口契约的
// 一部分，而不是某个 UI 文案的副产品（前端能传提示，就等于谁都能改它）。
const extractInstruction = `任务：从下面 ---INPUT-BEGIN--- 与 ---INPUT-END--- 之间的文本里，提取其中所有 JSON 值（对象或数组），按出现顺序输出。

硬性要求：
1. 只输出 JSON 本身，多个对象之间用空行分隔；不要解释、不要代码块围栏、不要序号或标题。
2. 数字逐字符照抄原文，禁止改写精度或改用科学计数法（19 位以上的 ID、雪花号不得四舍五入）。
3. 字符串原样照抄，含中文、转义符与内部标点。
4. 文本里若没有完整 JSON，只输出一行 NO_JSON。`

// ErrAINotJSON 单列哨兵："AI 没返回 JSON"与"AI 调用失败"是两类问题，
// 前者重试可能就成，后者要去看环境（未装 CLI/未登录/超时）。
var ErrAINotJSON = errors.New("AI 返回的内容不是合法 JSON")

// fenceMark 用数值常量而不是字面量反引号串：本仓多层转义（编辑器→脚本→文件）
// 已多次吃掉反斜杠/反引号（#127），这里干脆不给它可吃的东西。
const fenceMark = "```"

// isFenceLine 只认"整行就是围栏"：反引号之后允许一个语言标记（json / JSON / json5），
// 别的都不算。收紧的依据在案：第一版只看"是否以 ``` 开头"，于是"``` 以上是提取
// 结果"被当成闭合围栏**静默丢掉**——模型那句解释就此从界面消失，用户以为返回的
// 只有 JSON。带内容的行必须留下参与判定（它会让整块解析失败，于是如实拒收）。
func isFenceLine(l string) bool {
	t := strings.TrimSpace(l)
	if !strings.HasPrefix(t, fenceMark) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(t, fenceMark))
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// stripCodeFences 去掉首尾的 ``` / ```json 围栏行。模型加围栏是高概率行为
// （尽管提示词禁止），剥掉是**尊重契约**而不是放宽契约：围栏之内仍须整块合法。
func stripCodeFences(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && isFenceLine(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && isFenceLine(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// abbrev 取一行内做诊断展示（错误文本会原样上屏，不能把整段拒绝语糊回界面）。
func abbrev(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// worthExtracting 一个解析成功的值"值不值得当成果"：空容器与"单个标量的数组"
// （`[31735]`、`[0]`——日志碎片也是合法 JSON，#130）一律不算。宁可漏，不可假。
func worthExtracting(n node) bool {
	switch n.kind {
	case kObject:
		return len(n.members) > 0
	case kArray:
		if len(n.items) == 0 {
			return false
		}
		if len(n.items) == 1 && n.items[0].kind != kObject && n.items[0].kind != kArray {
			return false
		}
		return true
	default:
		return false
	}
}

// validateAIJSON 把模型返回收成"可交付的节点们"；失败时给拒因（不为空），
// 调用方据此报错——绝不返回一份"看着像结果"的空壳。note 是"丢弃了什么"的
// 点名说明（模型另附的非 JSON 文字被丢弃不静默）。
func validateAIJSON(text string) (nodes []node, note string, reject string) {
	s := strings.TrimSpace(stripCodeFences(text))
	if s == "" {
		return nil, "", "模型没有返回任何内容"
	}
	// ① 整份就是一块 JSON 先走——这也是唯一能容纳"格式化后自带空行"的路。
	if n, perr, _ := analyze(s); perr == nil {
		if !worthExtracting(n) {
			return nil, "", fmt.Sprintf("返回的是 %s，不是可用的 JSON 容器（空对象/空数组/裸标量都不算成果）", n.kind.String())
		}
		return []node{n}, "", ""
	}
	// ② 按契约的多份返回逐块严判。
	var ok []node
	var bad []string
	for _, blk := range strings.Split(s, "\n\n") {
		blk = strings.TrimSpace(stripCodeFences(blk))
		if blk == "" {
			continue
		}
		if n, perr, _ := analyze(blk); perr == nil && worthExtracting(n) {
			ok = append(ok, n)
			continue
		}
		bad = append(bad, abbrev(blk, 48))
	}
	if len(ok) == 0 {
		return nil, "", "没有一块是完整的 JSON（模型多半在解释，而不是在返回）：" + strings.Join(bad, " / ")
	}
	if len(bad) > 0 {
		note = fmt.Sprintf("模型另附了 %d 段非 JSON 文字，已丢弃不采纳：%s", len(bad), strings.Join(bad, " / "))
	}
	return ok, note, ""
}
