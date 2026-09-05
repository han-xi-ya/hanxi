package npmtool

import (
	"regexp"

	"hanxi/internal/modules/envcheck/detect"
)

// specDetector 目录条目的通用探测器适配：把 ToolSpec 转成 detect.Detector，
// 复用 detect 统一流程（LookPath、5s 超时、cmd/C 包装、状态机）与卡片流。
type specDetector struct {
	spec ToolSpec
	re   *regexp.Regexp
}

func newDetector(s ToolSpec) specDetector {
	return specDetector{spec: s, re: commandPattern(s)}
}

func (d specDetector) Name() string          { return d.spec.Command }
func (d specDetector) Display() string       { return d.spec.Display }
func (d specDetector) VersionArgs() []string { return d.spec.VersionArgs }

func (d specDetector) Parse(out string) string {
	if m := d.re.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// MissingHint 未安装时给出可操作指引：一键安装或手动 npm 命令。
func (d specDetector) MissingHint() string {
	return "未在 PATH 中找到 " + d.spec.Display + "。可点击下方「安装」由 Hanxi 经 npm 全局安装，或在终端执行 npm install -g " + d.spec.Package + "@latest"
}

// init 把每个目录条目注册进 detect 注册表，自动并入 DetectAll 卡片流。
// 本包被 envcheck 包 import，随模块装载触发；detect 包自身测试不会链接到此，
// 故 detect 包内注册数断言不受影响。
func init() {
	for _, s := range specs {
		detect.Register(newDetector(s))
	}
}
