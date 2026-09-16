package detect

import "regexp"

// npmVersionRe 匹配 `npm --version` 输出中的版本号（兼容预发布后缀）。
// 样本：10.8.3（部分版本输出前有空行）
var npmVersionRe = regexp.MustCompile(`(?m)^\s*v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`)

// npmDetector 实现 Detector，探测 npm（Windows 下经 npm.cmd 分发，runner 自动 cmd /C 包装）。
type npmDetector struct{}

func (npmDetector) Name() string          { return "npm" }
func (npmDetector) Display() string       { return "npm" }
func (npmDetector) VersionArgs() []string { return []string{"--version"} }
func (npmDetector) Parse(out string) string {
	if m := npmVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func init() { Register(npmDetector{}) }
