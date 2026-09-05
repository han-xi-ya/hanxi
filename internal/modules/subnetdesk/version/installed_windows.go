//go:build windows

package version

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"hanxi/internal/platform/versioninfo"
)

// 安装版身份常量（上游 WiX 工程源码实证 + 真机交叉验证，勿凭直觉改动）：
//   - DisplayName：preprocess.py 按产品名原样写入卸载注册表。真机在
//     HKLM\...\Uninstall 下命中两个键（GUID 键 + 遗留产品名键），GUID 键
//     无 InstallLocation（真机实证），故候选按"exe 真实存在"裁决；
//   - 主程序名：组件 File 元素 Name=$(var.Product).exe → SubnetDesk.exe，
//     与便携 packer 定名 subnetdesk.exe 大小写互补（EqualFold 比较处需留意）；
//   - DisplayVersion：四段"版本.构建修订号"（如 1.3.0.29797570），前
//     三段才是发布版本。
const (
	systemDisplayName = "SubnetDesk"
	installedExeName  = "SubnetDesk.exe"
)

// DetectSystemInstall 探测系统级安装的 SubnetDesk（MSI perMachine → Program Files）。
// 只读 HKLM 卸载注册表 64 位视图（x64 perMachine 包在原生视图可见）；
// 任何注册表异常一律按"未安装"处理（探测只认确定证据）。
func DetectSystemInstall() (SystemInstall, bool) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, registry.READ)
	if err != nil {
		return SystemInstall{}, false
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return SystemInstall{}, false
	}
	for _, name := range names {
		sub, err := registry.OpenKey(key, name, registry.READ)
		if err != nil {
			continue
		}
		display, _, dnErr := sub.GetStringValue("DisplayName")
		loc, _, lcErr := sub.GetStringValue("InstallLocation")
		dispVer, _, dvErr := sub.GetStringValue("DisplayVersion")
		sub.Close()
		if dnErr != nil || !strings.EqualFold(strings.TrimSpace(display), systemDisplayName) {
			continue
		}
		loc = strings.Trim(strings.TrimSpace(loc), `"`)
		if loc == "" || lcErr != nil {
			continue
		}
		exe := filepath.Join(loc, installedExeName)
		if fi, statErr := os.Stat(exe); statErr != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue // 注册表残留/键位不含路径：非可信候选
		}
		ver := normalizeDisplayVersion(dispVer)
		if dvErr != nil || ver == "" {
			// DisplayVersion 缺失/畸形（理论不可达）：退读 PE 版本资源兜底
			fv, fvErr := versioninfo.FileVersion(exe)
			if fvErr != nil || !plainVersionRe.MatchString(fv) {
				continue
			}
			ver = fv
		}
		return SystemInstall{Version: "v" + ver, ExePath: exe, Dir: loc}, true
	}
	return SystemInstall{}, false
}
