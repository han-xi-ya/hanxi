// Package versioncmp 提供工具版本号通用比较：
// 多个托管模块（everything / bcu 及后续集成）都要按"哪个已装版本最新"排序，
// 抽取共享避免复制。从 everything 模块原样提取，行为向后一致。
package versioncmp

import (
	"strconv"
	"strings"
)

// Compare 比较两个版本号（无 v 前缀，如 "1.5.0.1422b" 或 "6.2.0" / "6.1.0.1"）。
// 返回 1（a 更新）/ 0（相等）/ -1（b 更新）。
// 规则：逐段数字作数值比较；段内尾部字母按字典序并视为比纯数字段更新
// （"1422" < "1422a" < "1422b"，与 voidtools 的修正版发布节奏一致）；
// 段数不同时先逐段比完，均相等则段多者胜（"6.2.0" < "6.2.0.1"）。
// 非规范段整体退化字典序（正常数据不可达）——本条是被 compare_test.go 钉住的既有
// 契约（Compare("v1.2.3","1.2.3")==1），不在此处改；要"兜底/非规范形态不得当最新"，
// 请比较方先用 IsCanonical 定序（artifact.Tree 的降序即如此），不要指望本函数替你分档。
func Compare(a, b string) int {
	// 非规范 vs 规范：规范方恒胜，先于任何字典序判定。
	sa, okA := split(a)
	sb, okB := split(b)
	if !okA || !okB { // 两侧同为非规范：维持历史行为（整串字典序）
		return strings.Compare(a, b)
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
// 判据与 Compare 同源（同一个 split），因此不会出现"一处认规范、另一处退化字典序"的双真相。
// 非规范形态包括：本地导入兜底目录名 imported-<sha256 前缀> / imported-<秒级时间戳>，
// 以及未剥 v 前缀的串。
// Compare 对这类值退化为整串字典序，因此"字典序恰好更大"的兜底目录会被排成最新——
// 需要"非规范形态恒劣后"的比较方（如 artifact.Tree 降序）必须先按本判据定序，别依赖字典序。
func IsCanonical(v string) bool {
	_, ok := split(v)
	return ok
}
