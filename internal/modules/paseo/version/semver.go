package version

import (
	"strconv"
	"strings"
)

// Compare 按 semver 2.0 规则比较版本号（a>b 返回 1，相等 0，a<b 返回 -1）。
// Paseo 的 beta 通道（0.8.0-beta.1）与 stable（0.8.0）混排时必须遵守预发布
// 排序规则（0.7.2-beta.1 < 0.7.2 < 0.8.0-beta.1），字典序与"有横杠"直觉都会
// 排错（recordly 同款纪律）。非规范版本（imported 时间戳兜底等）退化为字典序，
// 永不 panic。
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

// IsCanonical 版本号是否为规范 semver（可被 Compare 数值排序）。
// 排序方（service 层"自动最新已装"）以此把 imported- 时间戳目录恒排最后——
// 非规范串退化字典序时 "imported-…" 字典序大于一切数字版本，直接喂给
// Compare 会把导入目录误判为最新。
func IsCanonical(v string) bool {
	_, _, ok := splitSemver(v)
	return ok
}

// splitSemver 拆 "v1.2.3-beta.2" 为数值核心与预发布串（容忍 v 前缀）。
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

// comparePreRelease semver §11：点分标识符逐个比，数字段按数值且恒小于字母段，
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
