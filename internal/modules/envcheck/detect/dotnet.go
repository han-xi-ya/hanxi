package detect

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// dotnetDetector 探测 .NET（Core）环境。
// 版本命令用 --info 而非 --version：未安装 SDK 的纯运行时机器上 --version 会直接报错，
// 而 --info 恒定退出 0 并列出 SDK 与共享框架数据行。节标题存在本地化风险，
// 因此只按语言无关的数据行形状解析：
//
//	  Microsoft.NETCore.App 8.0.13 [C:\Program Files\dotnet\shared\...]
//	  9.0.100 [C:\Program Files\dotnet\sdk]
var (
	dotnetRuntimeRe = regexp.MustCompile(`(?m)^\s*Microsoft\.(NETCore|AspNetCore|WindowsDesktop)\.App\s+(\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?)\s*\[`)
	dotnetSDKRe     = regexp.MustCompile(`(?m)^\s+(\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?)\s+\[`)
)

type dotnetDetector struct{}

func (dotnetDetector) Name() string    { return "dotnet" }
func (dotnetDetector) Display() string { return ".NET" }
func (dotnetDetector) VersionArgs() []string {
	return []string{"--info"}
}

// Parse 返回 SDK 优先的展示版本：有 SDK 时与 dotnet --version 直觉一致，
// 纯运行时机器回退为最高 Microsoft.NETCore.App 版本。
func (d dotnetDetector) Parse(out string) string {
	info := parseDotNetInfo(out)
	if sdk := highestDotNetVersion(info.SDKs); sdk != "" {
		return sdk
	}
	return highestDotNetVersion(info.Runtimes)
}

func (d dotnetDetector) ParseDetails(out string) *ToolDetails {
	info := parseDotNetInfo(out)
	if len(info.SDKs) == 0 && len(info.Runtimes) == 0 {
		return nil
	}
	return &ToolDetails{DotNet: &info}
}

// parseDotNetInfo 归并 dotnet --info 数据行：每族收集全部已安装版本，
// 去重并按版本升序排列（.NET 并排安装，低版本线同样在运行）。
func parseDotNetInfo(out string) DotNetDetails {
	var info DotNetDetails
	add := func(versions *[]string, version string) {
		if !validDotNetVersion(version) || slices.Contains(*versions, version) {
			return
		}
		*versions = append(*versions, version)
		sort.Slice(*versions, func(i, j int) bool {
			return compareDotNetVersion((*versions)[i], (*versions)[j]) < 0
		})
	}
	for _, m := range dotnetRuntimeRe.FindAllStringSubmatch(out, -1) {
		switch m[1] {
		case "NETCore":
			add(&info.Runtimes, m[2])
		case "WindowsDesktop":
			add(&info.Desktops, m[2])
		case "AspNetCore":
			add(&info.AspNetCore, m[2])
		}
	}
	for _, m := range dotnetSDKRe.FindAllStringSubmatch(out, -1) {
		add(&info.SDKs, m[1])
	}
	return info
}

// highestDotNetVersion 返回列表末位（升序排列下的最高版本）；空列表返回空串。
func highestDotNetVersion(versions []string) string {
	if len(versions) == 0 {
		return ""
	}
	return versions[len(versions)-1]
}

var dotnetVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$`)

func dotnetVersionParts(raw string) (parts [4]string, ok bool) {
	m := dotnetVersionRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return parts, false
	}
	return [4]string{m[1], m[2], m[3], m[4]}, true
}

func validDotNetVersion(raw string) bool {
	_, ok := dotnetVersionParts(raw)
	return ok
}

// compareDotNetVersion 按 major.minor.patch 数值序比较，同号时正式版高于预发布版，
// 两个预发布版按标识串字典序。仅在双方均可解析时调用。
func compareDotNetVersion(a, b string) int {
	left, _ := dotnetVersionParts(a)
	right, _ := dotnetVersionParts(b)
	for i := range 3 {
		l, _ := strconv.Atoi(left[i])
		r, _ := strconv.Atoi(right[i])
		if l != r {
			if l < r {
				return -1
			}
			return 1
		}
	}
	switch {
	case left[3] == right[3]:
		return 0
	case left[3] == "":
		return 1
	case right[3] == "":
		return -1
	case left[3] < right[3]:
		return -1
	default:
		return 1
	}
}

func init() { Register(dotnetDetector{}) }
