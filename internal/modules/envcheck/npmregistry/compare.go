package npmregistry

import (
	"regexp"
	"strconv"
	"strings"
)

// npmVersionPattern 解析可选 v 前缀的语义化版本：X.Y.Z，可带 -预发布 与 +构建元数据。
// Claude Code / Codex 当前发布均为裸 X.Y.Z，预发布分支为 beta 通道等场景预留正确比较。
var npmVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type parsedVersion struct {
	core       [3]uint64
	prerelease string
	hasPre     bool
}

func parseVersion(raw string) (parsedVersion, bool) {
	matches := npmVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return parsedVersion{}, false
	}
	var parsed parsedVersion
	for i := 1; i <= 3; i++ {
		value, err := strconv.ParseUint(matches[i], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		parsed.core[i-1] = value
	}
	if matches[4] != "" {
		parsed.prerelease = matches[4]
		parsed.hasPre = true
	}
	return parsed, true
}

// Compare 比较两个 npm semver 版本：主/次/补丁数值优先，预发布 < 正式版，
// 构建元数据（+xxx）忽略；任一侧不可解析返回 ok=false（上层据此落 unknown 关系）。
func Compare(a, b string) (int, bool) {
	left, okLeft := parseVersion(a)
	right, okRight := parseVersion(b)
	if !okLeft || !okRight {
		return 0, false
	}
	for i := range left.core {
		if left.core[i] != right.core[i] {
			if left.core[i] < right.core[i] {
				return -1, true
			}
			return 1, true
		}
	}
	// 主版本相同：正式版 > 预发布版。
	switch {
	case !left.hasPre && !right.hasPre:
		return 0, true
	case !left.hasPre:
		return 1, true
	case !right.hasPre:
		return -1, true
	}
	return comparePrerelease(left.prerelease, right.prerelease), true
}

// comparePrerelease 按 semver 规范逐点标识比较预发布串。
func comparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := compareIdentifier(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}

// compareIdentifier 数字标识恒 < 字母标识，同型则数值/字典序比较。
func compareIdentifier(a, b string) int {
	av, aErr := strconv.ParseUint(a, 10, 64)
	bv, bErr := strconv.ParseUint(b, 10, 64)
	aNumeric, bNumeric := aErr == nil, bErr == nil
	switch {
	case aNumeric && bNumeric:
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
		return 0
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	default:
		return strings.Compare(a, b)
	}
}
