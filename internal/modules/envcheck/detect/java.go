package detect

import "regexp"

// javaDetector 探测 Java（JRE/JDK）。
// -version 输出打在 **stderr** 上（runner 用 CombinedOutput 兜底合并）。
// 样本：openjdk version "17.0.9" 2023-10-17 / java version "1.8.0_151"
var javaVersionRe = regexp.MustCompile(`(?i)(?:openjdk|java)\s+version\s+"([^"]+)"`)

type javaDetector struct{}

func (javaDetector) Name() string          { return "java" }
func (javaDetector) Display() string       { return "Java (JRE/JDK)" }
func (javaDetector) VersionArgs() []string { return []string{"-version"} }
func (javaDetector) Parse(out string) string {
	if m := javaVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func init() { Register(javaDetector{}) }