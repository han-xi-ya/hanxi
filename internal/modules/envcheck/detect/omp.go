package detect

import "regexp"

// ompVersionRe 匹配 `omp --version` 输出中的版本号。
// 样本：omp/18.16.0（斜杠前缀不在通用词类内，实测形态单独锚定）。
// oh-my-pi 正主 npm 包是 scoped 的 @oh-my-pi/pi-coding-agent；裸名 oh-my-pi 与
// omp 均为无关占名包，本模块不走 npm 安装面，只对比官方 GitHub Releases。
var ompVersionRe = regexp.MustCompile(`(?m)^omp/v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)`)

// ompDetector 实现 Detector，探测 Oh My Pi（omp）。
type ompDetector struct{}

func (ompDetector) Name() string          { return "omp" }
func (ompDetector) Display() string       { return "Oh My Pi" }
func (ompDetector) VersionArgs() []string { return []string{"--version"} }

func (ompDetector) Parse(out string) string {
	if m := ompVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// MissingHint 未安装时给出官方两条主路（Windows 脚本装是首推，更新自带 omp update）。
func (ompDetector) MissingHint() string {
	return "未在 PATH 中找到 Oh My Pi。Windows 推荐官方脚本：在 PowerShell 执行 irm https://omp.sh/install.ps1 | iex；" +
		"或经 bun/npm 全局安装 @oh-my-pi/pi-coding-agent（裸名 oh-my-pi、omp 均为无关占名包，勿装）"
}

func init() { Register(ompDetector{}) }
