package detect

import (
	"regexp"
	"strings"
)

// javaVersionRe 提取版本号；javaRuntimeRe/javaVMRe 从 "(build ...)" 行中截取运行时与 VM 名称。
var (
	javaVersionRe = regexp.MustCompile(`(?im)^\s*(?:openjdk|java)\s+version\s+"([^"]+)"`)
	javaRuntimeRe = regexp.MustCompile(`(?im)^\s*(.+?Runtime Environment.*?)\s*\(build\s+[^)]+\)\s*$`)
	javaVMRe      = regexp.MustCompile(`(?im)^\s*(.+?\bVM(?:\s+[^()]*)?)\s*\(build\s+[^)]+\)\s*$`)
)

// javaDetector 实现 Detector，探测 Java（JRE/JDK）。
// 注意：-version 类输出打在 **stderr** 上（runner 用 CombinedOutput 兜底合并）；
// 版本命令附带 -XshowSettings:properties，使 ParseDetails 能拿到厂商/运行时/VM 结构化详情。
type javaDetector struct{}

func (javaDetector) Name() string    { return "java" }
func (javaDetector) Display() string { return "Java (JRE/JDK)" }
func (javaDetector) VersionArgs() []string {
	return []string{"-XshowSettings:properties", "-version"}
}
func (javaDetector) Parse(out string) string {
	if m := javaVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// ParseDetails 提取发行商、运行时名称与 VM 名称；三项全空时返回 nil（不落空详情对象）。
func (javaDetector) ParseDetails(out string) *ToolDetails {
	runtimeName := captureTrimmed(javaRuntimeRe, out)
	vmName := captureTrimmed(javaVMRe, out)
	vendor := javaVendor(runtimeName, vmName, out)
	if runtimeName == "" && vmName == "" && vendor == "" {
		return nil
	}
	return &ToolDetails{Java: &JavaDetails{Vendor: vendor, Runtime: runtimeName, VM: vmName}}
}

func captureTrimmed(pattern *regexp.Regexp, raw string) string {
	if match := pattern.FindStringSubmatch(raw); match != nil {
		return strings.TrimSpace(match[1])
	}
	return ""
}

// javaVendor 按已知发行版标记（temurin/corretto/zulu...）在运行时名与原始输出中嗅探厂商，识别不了归 Unknown。
func javaVendor(runtimeName, vmName, raw string) string {
	text := strings.ToLower(strings.Join([]string{runtimeName, vmName, raw}, "\n"))
	vendors := []struct{ marker, name string }{
		{"temurin", "Eclipse Temurin"},
		{"adoptium", "Eclipse Temurin"},
		{"corretto", "Amazon Corretto"},
		{"zulu", "Azul Zulu"},
		{"graalvm", "GraalVM"},
		{"microsoft", "Microsoft"},
		{"liberica", "BellSoft Liberica"},
		{"sapmachine", "SAP SapMachine"},
		{"semeru", "IBM Semeru"},
		{"java(tm) se", "Oracle"},
		{"java hotspot(tm)", "Oracle"},
		{"oracle", "Oracle"},
	}
	for _, vendor := range vendors {
		if strings.Contains(text, vendor.marker) {
			return vendor.name
		}
	}
	return "Unknown"
}

func init() { Register(javaDetector{}) }
