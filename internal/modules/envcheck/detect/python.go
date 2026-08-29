package detect

import (
	"regexp"
	"strings"
)

// pythonDetector 探测 Python。
// 样本：Python 3.12.4（Python 3.4+ 输出到 stdout；可能 CRLF 行尾）
var pythonVersionRe = regexp.MustCompile(`(?m)^\s*[Pp]ython\s+(\d+\.\d+(?:\.\d+)?)`)

type pythonDetector struct{}

func (pythonDetector) Name() string          { return "python" }
func (pythonDetector) Display() string       { return "Python" }
func (pythonDetector) VersionArgs() []string { return []string{"--version"} }
func (pythonDetector) Parse(out string) string {
	if m := pythonVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// IsStoreStub 判定商店存根：真 python 未安装时 Windows 默认 PATH 含
// %LOCALAPPDATA%\Microsoft\WindowsApps\python.exe，该别名进程执行无输出或
// 异常退出（或直接唤起商店），从路径特征即可识别为"假 python"。
func (pythonDetector) IsStoreStub(exePath string) bool {
	return strings.Contains(strings.ToLower(exePath), `windowsapps`)
}

func init() { Register(pythonDetector{}) }
