package nodeversion

import (
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

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
	for i := 1; i <= 3; i++ {
		value, err := strconv.ParseUint(matches[i], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		values[i-1] = value
	}
	return parsedVersion{major: values[0], minor: values[1], patch: values[2]}, true
}

// VersionLine 返回版本的 major 版本线标识（如 "24"），无法解析时返回空串。
// Node 的 LTS/Current 通道以 major 为界，因此版本线粒度就是主版本号。
func VersionLine(raw string) string {
	parsed, ok := parseVersion(raw)
	if !ok {
		return ""
	}
	return strconv.FormatUint(parsed.major, 10)
}

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
