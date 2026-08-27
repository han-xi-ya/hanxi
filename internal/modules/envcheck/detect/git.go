package detect

import "regexp"

// gitDetector 探测 Git。
// 样本：git version 2.46.0.windows.1 / git version 2.39.3 (Apple Git-145)
var gitVersionRe = regexp.MustCompile(`(?i)\bgit version\s+(\d+\.\d+\.\d+(?:\.[0-9A-Za-z]+)*)`)

type gitDetector struct{}

func (gitDetector) Name() string          { return "git" }
func (gitDetector) Display() string       { return "Git" }
func (gitDetector) VersionArgs() []string { return []string{"--version"} }
func (gitDetector) Parse(out string) string {
	if m := gitVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func init() { Register(gitDetector{}) }