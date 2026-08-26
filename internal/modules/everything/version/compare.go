package version

import (
	"strconv"
	"strings"
)

// CompareVersions 比较两个 Everything 版本号（无 v 前缀，如 "1.5.0.1422b"）。
// 返回 1（a 更新）/ 0（相等）/ -1（b 更新）。
// 规则：逐段数字作数值比较；段内尾部字母按字典序并视为比纯数字段更新
// （"1422" < "1422a" < "1422b"，与 voidtools 的修正版发布节奏一致）；
// 段数不同时先逐段比完，均相等则段多者胜。非规范段整体退化字典序（正常数据不可达）。
func CompareVersions(a, b string) int {
	sa, okA := splitVersion(a)
	sb, okB := splitVersion(b)
	if !okA || !okB {
		return strings.Compare(a, b)
	}
	n := min(len(sa), len(sb))
	for i := 0; i < n; i++ {
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

// splitVersion 把版本号切成段。段内格式：数字前缀 + 可选纯字母后缀（如 "1422b"）。
// 任一段不符合即整体返回 ok=false（调用方退化字典序兜底）。
func splitVersion(v string) ([]verSeg, bool) {
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