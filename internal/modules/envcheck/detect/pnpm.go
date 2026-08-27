package detect

import "regexp"

// pnpmDetector 探测 pnpm（Windows 下经 pnpm.cmd 分发，runner 自动 cmd /C 包装）。
// 样本：9.7.1
var pnpmVersionRe = regexp.MustCompile(`(?m)^\s*v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`)

type pnpmDetector struct{}

func (pnpmDetector) Name() string          { return "pnpm" }
func (pnpmDetector) Display() string       { return "pnpm" }
func (pnpmDetector) VersionArgs() []string { return []string{"--version"} }
func (pnpmDetector) Parse(out string) string {
	if m := pnpmVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func init() { Register(pnpmDetector{}) }