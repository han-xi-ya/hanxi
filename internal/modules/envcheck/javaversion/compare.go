package javaversion

import (
	"regexp"
	"strconv"
	"strings"
)

var javaVersionPattern = regexp.MustCompile(`^(?:1\.)?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[._](\d+))?(?:(?:\+|-b)(\d+))?(?:-LTS)?$`)

type parsedVersion struct {
	feature uint64
	interim uint64
	update  uint64
	patch   uint64
	build   uint64
}

func parseVersion(raw string) (parsedVersion, bool) {
	value := strings.TrimSpace(raw)
	matches := javaVersionPattern.FindStringSubmatch(value)
	if matches == nil {
		return parsedVersion{}, false
	}
	legacy := strings.HasPrefix(value, "1.")
	parts := [5]uint64{}
	for i := 1; i < len(matches); i++ {
		if matches[i] == "" {
			continue
		}
		n, err := strconv.ParseUint(matches[i], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		parts[i-1] = n
	}
	if legacy {
		// 1.8.0_402 -> 8.0.402.0，避免按首段 1 比较。
		return parsedVersion{feature: parts[0], interim: parts[1], update: parts[3], build: parts[4]}, true
	}
	return parsedVersion{feature: parts[0], interim: parts[1], update: parts[2], patch: parts[3], build: parts[4]}, true
}

// VersionLine 返回版本的 feature 版本线标识（如 "21"，Java 8 的 1.8.0_x 归一为 "8"），无法解析时返回空串。
func VersionLine(raw string) string {
	parsed, ok := parseVersion(raw)
	if !ok {
		return ""
	}
	return strconv.FormatUint(parsed.feature, 10)
}

// Compare 按 JEP 223 的 feature/interim/update/patch/build 数值序严格比较正式版本。
func Compare(a, b string) (int, bool) {
	left, okLeft := parseVersion(a)
	right, okRight := parseVersion(b)
	if !okLeft || !okRight {
		return 0, false
	}
	lv := [...]uint64{left.feature, left.interim, left.update, left.patch, left.build}
	rv := [...]uint64{right.feature, right.interim, right.update, right.patch, right.build}
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
