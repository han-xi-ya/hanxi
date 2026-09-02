package gitversion

import (
	"regexp"
	"strconv"
	"strings"
)

var gitVersionPattern = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)\.(\d+)(?:\.windows\.(\d+))?$`)

type parsedVersion struct {
	major    uint64
	minor    uint64
	patch    uint64
	revision uint64
}

func parseVersion(raw string) (parsedVersion, bool) {
	matches := gitVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return parsedVersion{}, false
	}
	values := [4]uint64{}
	for i := 1; i <= 3; i++ {
		value, err := strconv.ParseUint(matches[i], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		values[i-1] = value
	}
	if matches[4] != "" {
		value, err := strconv.ParseUint(matches[4], 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		values[3] = value
	}
	return parsedVersion{
		major: values[0], minor: values[1], patch: values[2], revision: values[3],
	}, true
}

// Compare 严格比较 Git 与 Git for Windows 版本。
// 返回值与 strings.Compare 一致；任一版本非法时 ok 为 false。
func Compare(a, b string) (result int, ok bool) {
	left, okLeft := parseVersion(a)
	right, okRight := parseVersion(b)
	if !okLeft || !okRight {
		return 0, false
	}
	leftValues := [...]uint64{left.major, left.minor, left.patch, left.revision}
	rightValues := [...]uint64{right.major, right.minor, right.patch, right.revision}
	for i := range leftValues {
		if leftValues[i] < rightValues[i] {
			return -1, true
		}
		if leftValues[i] > rightValues[i] {
			return 1, true
		}
	}
	return 0, true
}

// compareRelation 根据本机探测状态和官网最新稳定版计算版本关系。
func compareRelation(local detectVersion, latest string) Relation {
	if !local.installed {
		return RelationNotInstalled
	}
	result, ok := Compare(local.version, latest)
	if !ok {
		return RelationUnknown
	}
	switch {
	case result < 0:
		return RelationUpdateAvailable
	case result > 0:
		return RelationAhead
	default:
		return RelationLatest
	}
}

type detectVersion struct {
	version   string
	installed bool
}

// RelationForLocal 是 Service 组合结果时使用的关系计算入口。
func RelationForLocal(version string, installed bool, latest string) Relation {
	if strings.TrimSpace(latest) == "" {
		return RelationUnknown
	}
	return compareRelation(detectVersion{version: version, installed: installed}, latest)
}
