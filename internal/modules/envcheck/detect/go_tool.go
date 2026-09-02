package detect

import (
	"regexp"
	"strings"
)

// goDetector 探测 Go 工具链。
// 样本：go version go1.22.5 windows/amd64
// 开发版：go version devel go1.24-8a0e33a linux/amd64（捕获 1.24，可接受）
var goVersionRe = regexp.MustCompile(`(?i)\bgo(\d+(?:\.\d+){1,2})\b`)

type goDetector struct{}

func (goDetector) Name() string          { return "go" }
func (goDetector) Display() string       { return "Go" }
func (goDetector) VersionArgs() []string { return []string{"version"} }
func (goDetector) Parse(out string) string {
	if m := goVersionRe.FindStringSubmatch(out); m != nil {
		if strings.Contains(strings.ToLower(out), "version devel ") {
			return m[1] + "-devel"
		}
		return m[1]
	}
	return ""
}

func init() { Register(goDetector{}) }
