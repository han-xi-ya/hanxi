//go:build windows

package version

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dotnetInstallRoot .NET 运行时标准安装根（与 .NET 官方安装器布局一致）。
// 不依赖 dotnet CLI（PATH 里未必有），直接读磁盘目录得出权威结论（bcu 同款策略）。
const dotnetInstallRoot = `C:\Program Files\dotnet`

// DesktopRuntimeVersions 返回已安装的 Windows 桌面运行时版本列表
// （Microsoft.WindowsDesktop.App，如 ["8.0.13", "10.0.2"]，按版本排序）。
// 空列表 = 未安装桌面运行时。no-runtime 变体的可用性判断依据。
func DesktopRuntimeVersions() []string {
	return desktopRuntimeVersionsUnder(filepath.Join(dotnetInstallRoot, "shared", "Microsoft.WindowsDesktop.App"))
}

// desktopRuntimeVersionsUnder 从指定目录枚举桌面运行时版本（注入路径便于单测）。
// 每个子目录名须形如 X.Y.Z（段内纯数字），其余噪声文件忽略。
func desktopRuntimeVersionsUnder(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 目录不存在 = 未安装
	}
	var vers []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		parts := strings.Split(name, ".")
		if len(parts) < 2 || len(parts) > 4 {
			continue
		}
		valid := true
		for _, p := range parts {
			if p == "" {
				valid = false
				break
			}
			for _, c := range p {
				if c < '0' || c > '9' {
					valid = false
					break
				}
			}
		}
		if valid {
			vers = append(vers, name)
		}
	}
	sort.Strings(vers)
	return vers
}

// HasDesktopRuntimeMajor 已装桌面运行时中是否存在主版本为 major 的（"10" → 10.0.x）。
// .NET 运行时不跨大版本回退：no-runtime 版需要 10.x 桌面运行时，装了 8.x/9.x 也跑不起来。
func HasDesktopRuntimeMajor(vers []string, major string) bool {
	prefix := major + "."
	for _, v := range vers {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}
