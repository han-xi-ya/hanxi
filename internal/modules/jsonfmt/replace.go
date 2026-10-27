package jsonfmt

import (
	"fmt"
	"strings"
)

// 结构化替换：按规范化路径改值或改键名，产出仍是合法 JSON。
//
// 三条约束都源于"这是第二处会改数据的地方"：
//  1. **只能显式触发**。格式化/修复/抽取可以自动（读不了或只是换写法），替换不行——
//     改动意图必须是用户按下按钮那一下，前端因此不给它挂防抖自动跑。
//  2. 新值必须是合法 JSON 字面量：填 `abc` 直接报错而不是"当字符串算了"。
//     悄悄改类型比不改更难查，而替换完多半要粘回原处用。
//  3. 路径写法与统计/查找**共用同一套规则**（数组折叠成 `items[].id`），
//     所以"查找命中里点一下复制的路径"能直接贴进替换框。两套写法功能就是摆设。

// Replace 一条替换规则。NewValue 与 NewKey 至少给一个；都给则先改名再改值。
type Replace struct {
	// Path 规范化路径，如 `data.items[].id`、`user.name`、`[]`（顶层数组每项）。
	Path string `json:"path"`
	// NewValue 非空时替换该路径的值（须为合法 JSON 文本）。
	NewValue string `json:"newValue"`
	// NewKey 非空时重命名命中的键。
	NewKey string `json:"newKey"`
}

// applyReplace 递归替换，返回新树与命中次数。不原地改：调用方要能撤销，
// 保留原树比记 diff 简单可靠，而撤销就是"不带 replace 再算一次"。
func applyReplace(n node, target Replace) (node, int) {
	return replaceAt(n, "", target)
}

// replaceAt 的路径拼装规则与 collect（统计/查找）逐字一致，这是两处能互用的前提。
func replaceAt(n node, prefix string, t Replace) (node, int) {
	count := 0
	switch n.kind {
	case kObject:
		members := make([]member, len(n.members))
		for i, m := range n.members {
			path := m.key
			if prefix != "" {
				path = prefix + "." + m.key
			}
			child, c := replaceAt(m.value, path, t)
			if path == t.Path {
				if t.NewKey != "" {
					m.key = t.NewKey
					m.keyLit = quoteLiteral(t.NewKey)
				}
				if t.NewValue != "" {
					child = mustParseValue(t.NewValue)
				}
				c++
			}
			m.value = child
			members[i] = m
			count += c
		}
		n.members = members
	case kArray:
		ipath := "[]"
		if prefix != "" {
			ipath = prefix + "[]"
		}
		items := make([]node, len(n.items))
		for i, it := range n.items {
			child, c := replaceAt(it, ipath, t)
			items[i] = child
			count += c
		}
		n.items = items
	}
	return n, count
}

// mustParseValue 只在 checkReplace 已验证过后调用；万一解析不了（理论上不可达）
// 就退回字符串节点，绝不 panic——RPC 里 panic 会带走整个 hanxi。
func mustParseValue(text string) node {
	if n, perr, _ := analyze(text); perr == nil {
		return n
	}
	return node{kind: kString, lit: quoteLiteral(text)}
}

// checkReplace 挡下"参数本身不合法"。它与"没命中"是两回事：
// 没命中是正常结果（Replaced=0，输出不变），要走 error 通道只有这里这些情况。
func checkReplace(r Replace) error {
	if strings.TrimSpace(r.Path) == "" {
		return fmt.Errorf("请填写要替换的路径（可在下方查找结果里点路径复制）")
	}
	if strings.TrimSpace(r.NewKey) == "" && r.NewValue == "" {
		return fmt.Errorf("请给出新键名或新值，至少一个")
	}
	if r.NewValue != "" {
		if _, perr, _ := analyze(r.NewValue); perr != nil {
			return fmt.Errorf("新值必须是合法 JSON 字面量（字符串得带引号，如 \"abc\"）：%v", perr.msg)
		}
	}
	if k := strings.TrimSpace(r.NewKey); k != "" && !validKeyChars(k) {
		return fmt.Errorf("新键名 %q 含空格或引号/反斜杠，请换个写法", k)
	}
	return nil
}

// quoteLiteral 把裸键写成 JSON 字符串字面量。
func quoteLiteral(k string) string {
	var b strings.Builder
	b.WriteByte(btQuote)
	for _, r := range k {
		switch r {
		case '"':
			b.WriteByte(btBackslash)
			b.WriteByte('"')
		case '\\':
			b.WriteByte(btBackslash)
			b.WriteByte(btBackslash)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(btQuote)
	return b.String()
}

func validKeyChars(k string) bool {
	return !strings.ContainsAny(k, " \t\n\r\"\\")
}

// replaceNotice 把"改了什么、改了几处、怎么撤"一句说完。
// 撤销不需要额外状态：原文从没被写过，去掉替换条件再算一次就是原样。
func replaceNotice(t Replace, count int) string {
	action := "改值"
	if t.NewKey != "" && t.NewValue != "" {
		action = "改键名并改值"
	} else if t.NewKey != "" {
		action = "改键名"
	}
	if count == 0 {
		return "路径 " + t.Path + " 没命中，输出与不替换时完全一致（没静默改动任何东西）"
	}
	return fmt.Sprintf("已按路径 %s %s %d 处；改动只在输出里，原文从未被写入——去掉替换条件重算即撤销",
		t.Path, action, count)
}
