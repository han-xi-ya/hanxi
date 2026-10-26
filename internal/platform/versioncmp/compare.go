// Package versioncmp 提供工具版本号通用比较：
// 多个托管模块（everything / bcu 及后续集成）都要按"哪个已装版本最新"排序，
// 抽取共享避免复制。从 everything 模块原样提取，行为向后一致。
package versioncmp

import (
	"strconv"
	"strings"
)

// Compare 比较两个版本号，返回 1（a 更新）/ 0（相等）/ -1（b 更新）。
//
// 规则（自上而下依次生效）：
//  1. 先归一：去首尾空白，并剥掉**单个前导 v/V**（当且仅当其后紧跟数字）。各托管族
//     把已装版本以带 v 前缀的展示令牌存进账本并直接喂给本函数（windterm/termora/
//     piclite/vscode 等），若不作归一，"v2.16.1" 会被判为非规范串、与"v2.9.0"一起
//     退化成字典序，于是 2.9.0 被当成比 2.16.1 更新——Termora 真实版本线已到 2.16，
//     属现行缺陷（2026-10-26 审查批次 T2 取证）。
//  2. 再分档：**规范串恒优先于非规范串**。非规范形态包括本地导入读不出版本时的兜底
//     目录名 `imported-<sha256 前缀>` 与历史 `imported-<秒级时间戳>`。不分档则退化为
//     整串字典序，而 'i'(0x69) 的码点大于数字，兜底目录会被排成"最新版"，凡取首个
//     当"最新已装"的调用方都会因此选中导入件、或误判"已是最新"而漏报更新
//     （`artifact.Tree` 与多家模块的本地排序同受此害，2026-10-26 审查批次坐实）。
//  3. 规范串之间：逐段数字作数值比较；段内尾部字母按字典序并视为比纯数字段更新
//     （"1422" < "1422a" < "1422b"，与 voidtools 的修正版发布节奏一致）；段数不同时
//     先逐段比完，均相等则段多者胜（"6.2.0" < "6.2.0.1"）。
//  4. 两侧同为非规范：维持历史行为，整串字典序（不 panic、有确定序）。
//
// 契约变更说明：规则 1/2 使两处旧结果改变——`Compare("v1.2.3","1.2.3")` 由 1 变 0
// （旧语义把带 v 前缀当成"文本更大"，正是规则 1 要修的错），`Compare("imported-x","1.2.3")`
// 由 1 变 -1（旧语义让兜底目录冒充最新版）。`compare_test.go` 已同步订正并注明理由。
func Compare(a, b string) int {
	na, nb := normalize(a), normalize(b)
	ca, cb := IsCanonical(na), IsCanonical(nb)
	if ca != cb { // 规范方恒胜，先于任何字典序判定
		if ca {
			return 1
		}
		return -1
	}
	sa, okA := split(na)
	sb, okB := split(nb)
	if !okA || !okB { // 两侧同为非规范：维持历史行为（整串字典序）
		return strings.Compare(na, nb)
	}
	for i := range min(len(sa), len(sb)) {
		if sa[i].num != sb[i].num {
			if sa[i].num > sb[i].num {
				return 1
			}
			return -1
		}
		if sa[i].alpha != sb[i].alpha {
			if sa[i].alpha > sb[i].alpha {
				return 1
			}
			return -1
		}
	}
	if len(sa) != len(sb) {
		if len(sa) > len(sb) {
			return 1
		}
		return -1
	}
	return 0
}

// normalize 去首尾空白并剥掉单个前导 v/V（仅当其后紧跟数字，避免误伤 "vim" 之类
// 非版本串）。账本展示令牌与远程 tag 常分别以 "v1.2.3" 与 "1.2.3" 形态进入比较，
// 不先归一两者永不相等，去重与"是否已装"判定都会落空。
func normalize(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 2 && (v[0] == 'v' || v[0] == 'V') && v[1] >= '0' && v[1] <= '9' {
		return v[1:]
	}
	return v
}

type verSeg struct {
	num   int64  // 段内数字前缀
	alpha string // 段内尾部纯字母（纯数字段为空串，字典序最小）
}

// split 把版本号切成段。段内格式：数字前缀 + 可选纯字母后缀（如 "1422b"）。
// 任一段不符合即整体返回 ok=false（调用方退化字典序兜底）。
func split(v string) ([]verSeg, bool) {
	parts := strings.Split(v, ".")
	segs := make([]verSeg, 0, len(parts))
	for _, p := range parts {
		i := 0
		for i < len(p) && p[i] >= '0' && p[i] <= '9' {
			i++
		}
		if i == 0 {
			return nil, false // 空段或非数字开头均视为非规范
		}
		for j := i; j < len(p); j++ {
			c := p[j]
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
				return nil, false
			}
		}
		num, err := strconv.ParseInt(p[:i], 10, 64)
		if err != nil {
			return nil, false // 超长数字段溢出：退化字典序
		}
		segs = append(segs, verSeg{num: num, alpha: p[i:]})
	}
	if len(segs) == 0 {
		return nil, false
	}
	return segs, true
}

// IsCanonical 报告 v 是否为可逐段数值比较的规范版本号：每段形如"数字前缀 + 可选纯字母后缀"。
// 判据与 Compare 同源（同一个 normalize + split），因此不会出现"一处认规范、另一处退化
// 字典序"的双真相；前导 v/V 已由 normalize 剥除，带 v 的展示令牌同样判规范。
// 非规范形态包括：本地导入兜底目录名 imported-<sha256 前缀> / imported-<秒级时间戳>，
// 以及未剥 v 前缀的串。
// Compare 对这类值退化为整串字典序，因此"字典序恰好更大"的兜底目录会被排成最新——
// 需要"非规范形态恒劣后"的比较方（如 artifact.Tree 降序）必须先按本判据定序，别依赖字典序。
func IsCanonical(v string) bool {
	_, ok := split(v)
	return ok
}
