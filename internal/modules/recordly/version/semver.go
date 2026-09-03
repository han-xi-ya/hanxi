package version

import (
	"strconv"
	"strings"
)

// Compare 按 semver 2.0 规则比较版本号（a>b 返回 1，相等 0，a<b 返回 -1）。
// 与 ccswitch 的纯数值分段 versionCompare 不同：Recordly 的 beta 通道
// （v1.3.5-beta.2）与 stable（v1.3.5）混排时必须遵守预发布排序规则
// （1.3.4-beta.1 < 1.3.4 < 1.3.5-beta.2），字典序与"有横杠"直觉都会排错。
// 非规范版本（imported 时间戳兜底等）退化为字典序，永不 panic。
func Compare(a, b string) int {
	na, pa, okA := splitSemver(a)
	nb, pb, okB := splitSemver(b)
	if !okA || !okB {
		return strings.Compare(a, b)
	}
	for i := 0; i < 3; i++ {
		if na[i] != nb[i] {
			if na[i] > nb[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case pa == "" && pb == "":
		return 0
	case pa == "":
		return 1 // 正式版大于其任何预发布
	case pb == "":
		return -1
	default:
		return comparePreRelease(pa, pb)
	}
}

// splitSemver 拆 "v1.3.5-beta.2" 为数值核心与预发布串。
func splitSemver(v string) ([3]int, string, bool) {
	var core [3]int
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return core, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return core, "", false
		}
		core[i] = n
	}
	return core, pre, true
}

// comparePreRelease semver 11：点分标识符逐个比，数字段按数值且恒小于字母段，
// 全等时标识符少者小。
func comparePreRelease(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		switch {
		case errA == nil && errB == nil:
			if na != nb {
				if na > nb {
					return 1
				}
				return -1
			}
		case errA == nil:
			return -1 // 数字段 < 字母段
		case errB == nil:
			return 1
		default:
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	return 0
}
