package jsonfmt

import (
	"fmt"
	"strings"
)

// 模型返回的**严格验收**。这一段刻意不复用 beautify 的 ExtractAll 扫描，理由是实测出来的：
//
//	"很抱歉，这段文本里没有 JSON（形如 {"key":"value"} 的那样才算）。"
//
// 这句拒绝里含着一个**模型自己编造**的示例对象。走散文扫描，那个例子会被当候选解析成功，
// 于是拒绝语变成"提取成功 1 段"并且顶着"已经过本地 JSON 校验"的警告上屏——本地校验只证
// 语法，它证明不了"这段内容来自用户的原文"。同理 `[]`、`{}` 整份解析成功、Kind 是容器，
// 扫描路径的 hasContainer 类判据全都放行，端给用户的是一个空成功。
//
// 所以这里按**提示词的契约**验：模型被要求"只输出 JSON 本身，多个对象之间用空行分隔"，
// 那么每一份返回就必须自己是一块完整 JSON。逐块严格 parse，不扫描、不修复、不猜。
// 判"值不值得当结果"仍然复用本地那份 `worthExtracting`（空容器与单标量数组一律不算），
// 不另造第二个裁判——两处口径一漂移，就会出现"同一形状在本地算垃圾、在 AI 路算成果"。

// fenceBacktick 用数值常量而不是字面量反引号串：本仓多层转义（编辑器→脚本→文件）
// 已多次吃掉反斜杠/反引号（#127），这里干脆不给它可吃的东西。
const fenceMark = "```"

// isFenceLine 只认"整行就是围栏"：反引号之后允许一个语言标记（json / JSON / json5），
// 别的都不算。这条收紧是有实测依据的——第一版只看"是否以 ``` 开头"，于是
//
//	``` 以上是提取结果
//
// 被当成闭合围栏**静默丢掉**，模型那句解释就此从界面上消失，用户以为返回的只有 JSON。
// 带内容的行必须留下参与判定（它会让整块解析失败，于是如实拒收）。
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

// stripCodeFences 去掉首尾的 ``` / ```json 围栏行。
// 模型加围栏是高概率行为（尽管提示词禁止），剥掉是**尊重契约**而不是放宽契约：
// 围栏之内仍然必须整块是合法 JSON，不做任何"从中间找出 JSON"的宽容。
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

// validateAIJSON 把模型的返回收成"可交付的段"。失败时返回拒因（不为空），
// 调用方据此报错——绝不返回一份"看着像结果"的空壳。
func validateAIJSON(text string) (segs []extracted, note string, reject string) {
	s := strings.TrimSpace(stripCodeFences(text))
	if s == "" {
		return nil, "", "模型没有返回任何内容"
	}

	// ① 整份就是一份 JSON：这条路最可信，先走。也是唯一能容纳"格式化后自带空行"的路——
	//    所以它必须排在切块之前，否则一个漂亮的对象会因为中间有个空行被切成两块垃圾。
	if n, perr, _ := analyze(s); perr == nil {
		if !worthExtracting(n) {
			return nil, "", fmt.Sprintf("返回的是 %s，不是可用的 JSON 容器（空对象/空数组/裸标量都不算成果）",
				n.kind.String())
		}
		return []extracted{{n: n}}, "", ""
	}

	// ② 按契约的多份返回逐块判：块与块之间是空行，每块自身必须完整可解析。
	var ok []extracted
	var bad []string
	for _, blk := range strings.Split(s, "\n\n") {
		blk = strings.TrimSpace(stripCodeFences(blk))
		if blk == "" {
			continue
		}
		if n, perr, _ := analyze(blk); perr == nil && worthExtracting(n) {
			ok = append(ok, extracted{n: n})
			continue
		}
		bad = append(bad, abbrev(blk, 48))
	}
	if len(ok) == 0 {
		// 一帧都不合格：模型在解释而不是在返回。这里不退扫描——扫描就是刚才那个洞
		return nil, "", "没有一块是完整的 JSON（模型多半在解释，而不是在返回）：" + strings.Join(bad, " / ")
	}
	if len(bad) > 0 {
		note = fmt.Sprintf("模型另附了 %d 段非 JSON 文字，已丢弃不采纳：%s", len(bad), strings.Join(bad, " / "))
	}
	return ok, note, ""
}
