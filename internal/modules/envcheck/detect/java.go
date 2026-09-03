package detect

import (
	"regexp"
	"strings"
)

// javaDetector 探测 Java（JRE/JDK）。
// -version 输出打在 **stderr** 上（runner 用 CombinedOutput 兜底合并）。
var (
	javaVersionRe = regexp.MustCompile(`(?im)^\s*(?:openjdk|java)\s+version\s+"([^"]+)"`)
	javaRuntimeRe = regexp.MustCompile(`(?im)^\s*(.+?Runtime Environment.*?)\s*\(build\s+[^)]+\)\s*$`)
	javaVMRe      = regexp.MustCompile(`(?im)^\s*(.+?\bVM(?:\s+[^()]*)?)\s*\(build\s+[^)]+\)\s*$`)
)

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
