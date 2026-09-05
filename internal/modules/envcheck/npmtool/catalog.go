package npmtool

import (
	"regexp"

	"hanxi/internal/modules/envcheck/detect"
)

// ToolSpec 单个受管 npm 全局 CLI 的完整配置：既驱动检测（detector.go 适配
// detect.Detector），又驱动操作（manager.go 拼 npm 参数）。包名与命令一律来自
// 此处常量，service 只接受 Command 作为 ID，绝不接受前端传入包名/版本/路径。
type ToolSpec struct {
	Command       string   // 可执行文件名，兼目录 ID 与 detect 注册键，如 "claude"
	Display       string   // 前端展示名，如 "Claude Code"
	Package       string   // npm 包名（操作参数唯一来源），如 "@anthropic-ai/claude-code"
	VersionArgs   []string // 版本命令参数，如 ["--version"]
	VersionRegexp string   // "" = 使用默认宽松 semver 行式；仅输出怪异工具才需覆盖
}

// specs 目录：当前接入 Claude Code 与 Codex CLI，后续加工具在此追加一行即可。
var specs = []ToolSpec{
	{Command: "claude", Display: "Claude Code", Package: "@anthropic-ai/claude-code", VersionArgs: []string{"--version"}},
	{Command: "codex", Display: "Codex CLI", Package: "@openai/codex", VersionArgs: []string{"--version"}},
}

// defaultVersionPattern 行首可选单词前缀 + 可选 v + semver，同时吃下
// "2.1.260 (Claude Code)" 与 "codex-cli 0.153.2" 两类真实输出。
var defaultVersionPattern = regexp.MustCompile(`(?m)^\s*(?:[A-Za-z][A-Za-z0-9._-]*\s+)?v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`)

// packageNamePattern 合法 npm 包名（含 scoped），杜绝任何 shell 语义字符进入命令参数。
var packageNamePattern = regexp.MustCompile(`^(@[A-Za-z0-9-~][A-Za-z0-9-._~]*/)?[A-Za-z0-9-~][A-Za-z0-9-._~]*$`)

// Catalog 返回目录只读快照。
func Catalog() []ToolSpec {
	return append([]ToolSpec(nil), specs...)
}

// Spec 按 ID（= Command）精确查找，service 入参校验入口。
func Spec(id string) (ToolSpec, bool) {
	for _, s := range specs {
		if s.Command == id {
			return s, true
		}
	}
	return ToolSpec{}, false
}

// commandPattern 返回条目用于版本解析的正则（默认或覆盖式）。
func commandPattern(s ToolSpec) *regexp.Regexp {
	if s.VersionRegexp == "" {
		return defaultVersionPattern
	}
	return regexp.MustCompile(s.VersionRegexp)
}

// init 校验目录自洽性：目录错误属编程错误，启动即炸（与 nodeversion URL panic 同一哲学）。
func init() {
	seen := make(map[string]struct{}, len(specs))
	for _, s := range specs {
		if _, dup := seen[s.Command]; dup {
			panic("envcheck/npmtool: duplicate command in catalog: " + s.Command)
		}
		seen[s.Command] = struct{}{}
		if detect.Registered(s.Command) {
			panic("envcheck/npmtool: command collides with existing detector: " + s.Command)
		}
		if !packageNamePattern.MatchString(s.Package) {
			panic("envcheck/npmtool: invalid npm package name: " + s.Package)
		}
		if s.VersionRegexp != "" {
			if _, err := regexp.Compile(s.VersionRegexp); err != nil {
				panic("envcheck/npmtool: invalid version regexp for " + s.Command + ": " + err.Error())
			}
		}
	}
}
