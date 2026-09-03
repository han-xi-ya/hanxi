package dotnetversion

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	linePattern    = regexp.MustCompile(`^(\d+)\.(\d+)$`)
)

type parsedVersion struct {
	major uint64
	minor uint64
	patch uint64
}

// parseVersion 严格解析正式版 X.Y.Z；预发布（-rc/-preview）一律拒绝。
func parseVersion(raw string) (parsedVersion, bool) {
	matches := versionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return parsedVersion{}, false
	}
	values := [3]uint64{}
	for i := range values {
		value, err := strconv.ParseUint(matches[i+1], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		values[i] = value
	}
	return parsedVersion{major: values[0], minor: values[1], patch: values[2]}, true
}

// VersionLine 返回版本的 major.minor 版本线标识（接受 "9.0" 与 "9.0.8"），无法解析时返回空串。
func VersionLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if linePattern.MatchString(trimmed) {
		return trimmed
	}
	parsed, ok := parseVersion(trimmed)
	if !ok {
		return ""
	}
	return strconv.FormatUint(parsed.major, 10) + "." + strconv.FormatUint(parsed.minor, 10)
}

// Compare 按 major.minor.patch 数值序严格比较正式版本，任一非法返回 ok=false。
func Compare(a, b string) (int, bool) {
	left, okLeft := parseVersion(a)
	right, okRight := parseVersion(b)
	if !okLeft || !okRight {
		return 0, false
	}
	lv := [...]uint64{left.major, left.minor, left.patch}
	rv := [...]uint64{right.major, right.minor, right.patch}
	for i := range lv {
		if lv[i] < rv[i] {
			return -1, true
		}
		if lv[i] > rv[i] {
			return 1, true
		}
	}
	return 0, true
}
