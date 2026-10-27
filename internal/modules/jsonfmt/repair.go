package jsonfmt

import (
	"fmt"
	"strings"
	"unicode"
)

// jsonBackslash 用数值常量而不是字面量写法：本文件里出现裸反斜杠字面量时，
// 它在"编辑器 → 脚本 → 文件"多层转义里反复被吃掉（实测踩过三次，症状都是
// rune literal not terminated）。语义等价，且少一类工具链陷阱。
const jsonBackslash = byte(92)

// completeTail 给"尾部被切掉"的输入补上缺失的闭合符。
//
// 这是全模块**唯一一处会改动数据形状**的操作，所以三道闸都不松：
//   - 只在确实存在未闭合的层时才补；已闭合或属头部问题一律 ok=false，不制造假修复；
//   - 断开点落在字符串字面量内部时**拒绝补**：补引号等于凭空造出一个字段值，
//     那已经不是"补齐结构"而是"编数据"；
//   - 括号顺序不匹配（如 `{]`）直接放弃，不猜用户想写什么。
//
// 调用方还须验证"补完真能解析"才采用；解析不过就维持原错误，不硬凑。
func completeTail(src []byte) (fixed []byte, added int, ok bool) {
	var stack []byte
	inStr, esc := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == jsonBackslash:
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			stack = append(stack, c)
		case '}', ']':
			want := byte('{')
			if c == ']' {
				want = '['
			}
			if len(stack) == 0 || stack[len(stack)-1] != want {
				return nil, 0, false
			}
			stack = stack[:len(stack)-1]
		}
	}
	// 栈空：要么本来就完整（无需修），要么问题在头部——两种都不该由这里补。
	// inStr：断在字符串里，补闭合符救不了，硬补只会产出与原文含义不同的值。
	if len(stack) == 0 || inStr {
		return nil, 0, false
	}
	out := append([]byte(nil), src...)
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			out = append(out, '}')
		} else {
			out = append(out, ']')
		}
		added++
	}
	return out, added, true
}

// classifyFailure 把"解析失败"分成三种**用户动作完全不同**的形态，只依据确定性事实：
// 未闭合类错误 + 出错位置贴近文本末尾 ⇒ 尾部像被截断；首个实字符是 ] } , : ⇒ 头部没复制全。
//
// 判不出来就返回空串。宁可少说一句，也不要给一条方向相反的结论——旧文案曾把
// "开头没复制全"报成"JSON 之后还有多余内容"，正好说反，用户会去改结尾而问题在开头。
func classifyFailure(msg string, off, srcLen int, src string) string {
	end := len(strings.TrimRightFunc(src, unicode.IsSpace))
	nearEnd := off >= end-8
	unclosed := strings.Contains(msg, "未闭合") || strings.Contains(msg, "意外结束") ||
		strings.Contains(msg, "缺少数字") || strings.Contains(msg, "对象成员之间") ||
		strings.Contains(msg, "数组元素之间")
	if nearEnd && unclosed {
		return "看起来是尾部被截断（从日志窗口选区复制时最常见，往往少了末尾的 } 或 ]）。" +
			"可勾「补全尾部」按未闭合层数补齐再试——那是修复态输出，与原文不一致。"
	}
	head := strings.TrimLeftFunc(src, unicode.IsSpace)
	if head != "" && strings.IndexByte("]},:", head[0]) >= 0 {
		return "看起来是开头没复制全：第一个实字符是 " + string(head[0]) +
			"，说明它前面那段（含起始的 { 或 [）漏在外面。回源头把开头一起选中再复制。"
	}
	return ""
}

// repairLoose 把两种修复合成一道：先改宽松语法，再补被截断的闭合符。
// 只有"补完真能解析"才 ok——绝不为凑出一个结果而交付一段改坏了的文本。
//
// 刻意不做"对抽取出的每一段各自修复"：段级修复要给每个候选都跑一遍改写与重解析，
// 而抽取的输入通常是程序打出来的严格 JSON（不需要修）。等真遇到"一段日志里嵌着
// 宽松 JSON"的实例再加，不为想象中的需求写代码。
func repairLoose(src string) (fixed string, stats []RepairStat, tailAdded int, ok bool) {
	cand, stats, changed := relax(src)
	if !changed {
		cand = src
	}
	if _, perr, _ := analyze(cand); perr == nil {
		return cand, stats, 0, true
	}
	// 语法已严但结构缺尾：再试补闭合符
	f2, added, tc := completeTail([]byte(cand))
	if !tc {
		return "", nil, 0, false
	}
	if _, perr, _ := analyze(string(f2)); perr != nil {
		return "", nil, 0, false
	}
	return string(f2), stats, added, true
}

// repairNotice 把"改了什么"写成人话。两类必须分开说：
// 宽松语法修复不丢信息（只是换个合法写法），而补闭合符意味着**内容本身残缺**——
// 后者的输出看着完整，最容易骗人，所以单独点名。
func repairNotice(stats []RepairStat, tailAdded int) string {
	parts := make([]string, 0, len(stats)+1)
	total := 0
	for _, s := range stats {
		parts = append(parts, fmt.Sprintf("%s %d 处", s.Kind, s.Count))
		total += s.Count
	}
	if tailAdded > 0 {
		parts = append(parts, fmt.Sprintf("补了 %d 个闭合符", tailAdded))
	}
	if total == 0 && tailAdded == 0 {
		return ""
	}
	msg := "已自动修复后才解析成功：" + strings.Join(parts, "、")
	if tailAdded > 0 {
		msg += "。注意属「修复态」：末尾字段本身可能残缺，别当原文用。"
	}
	return msg
}
