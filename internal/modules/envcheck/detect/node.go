package detect

import "regexp"

// nodeDetector 探测 Node.js。
// 样本：v20.15.1（v 前缀保留在输出中，解析时去掉；可能 CRLF 行尾）
var nodeVersionRe = regexp.MustCompile(`(?m)^\s*v?(\d+\.\d+\.\d+)`)

type nodeDetector struct{}

func (nodeDetector) Name() string          { return "node" }
func (nodeDetector) Display() string       { return "Node.js" }
func (nodeDetector) VersionArgs() []string { return []string{"--version"} }
func (nodeDetector) Parse(out string) string {
	if m := nodeVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func init() { Register(nodeDetector{}) }
