package pythonversion

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	versionPattern = regexp.MustCompile(`^(?:Python\s+)?(\d+)\.(\d+)(?:\.(\d+))?$`)
	finalPattern   = regexp.MustCompile(`^Python (\d+)\.(\d+)\.(\d+)$`)
	minorPattern   = regexp.MustCompile(`^(\d+)\.(\d+)$`)
)

type parsedVersion struct {
	major uint64
	minor uint64
	patch uint64
}

func parseVersion(raw string) (parsedVersion, bool) {
	matches := versionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return parsedVersion{}, false
	}
	values := [3]uint64{}
	for i := 1; i <= 2; i++ {
		value, err := strconv.ParseUint(matches[i], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		values[i-1] = value
	}
	if matches[3] != "" {
		value, err := strconv.ParseUint(matches[3], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		values[2] = value
	}
	return parsedVersion{major: values[0], minor: values[1], patch: values[2]}, true
}

func parseFinalName(raw string) (parsedVersion, bool) {
	matches := finalPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return parsedVersion{}, false
	}
	values := [3]uint64{}
	for i := 1; i <= 3; i++ {
		value, err := strconv.ParseUint(matches[i], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		values[i-1] = value
	}
	return parsedVersion{major: values[0], minor: values[1], patch: values[2]}, true
}

func parseMinor(raw string) (major, minor uint64, ok bool) {
	matches := minorPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return 0, 0, false
	}
	major, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func canonicalVersion(v parsedVersion) string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func canonicalMinor(major, minor uint64) string {
	return fmt.Sprintf("%d.%d", major, minor)
}

// Compare 严格比较 Python 正式版本。本机 X.Y 输出按 X.Y.0 处理；
// alpha、beta、rc、dev、vendor 后缀一律拒绝，避免把预发布误判为正式版。
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
