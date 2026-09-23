// Package hostfeed 提供"发布物（release asset）平台/形态"的机械分类器与
// 展示元数据结构（W6/N13 全平台标注底座的共享件）。
//
// 定位（Plan PLAN_W6_HOST_CONTRACT §2.1）：只服务**展示**——各托管模块的
// 下载/校验路径继续走自家 remote.go 的资产判据，本包绝不反向指挥选包；
// "本托管"标（Managed）由调用方拿自己选中的资产名比对得出，本包不猜测。
//
// 规则来源是 2026-09-23 对 28 家上游 releases/latest 实拉的 273 个真实资产
// 名（缓存 .cache/w6probe/，测试 fixture 即出自该表）。诚实边界：按文件名
// 机械判型必然有认不出的（裸 .zip 无平台字样 → other/archive），宁降级
// 不猜标；新上游接入时先补 fixture 表再扩规则。
package hostfeed

import (
	"path/filepath"
	"strings"
)

// Platform 发布物目标平台。
type Platform string

const (
	PlatformWindows Platform = "windows"
	PlatformMacOS   Platform = "macos"
	PlatformLinux   Platform = "linux"
	PlatformAndroid Platform = "android"
	PlatformFreeBSD Platform = "freebsd"
	PlatformOther   Platform = "other"
)

// Form 发布物形态。portable=文件名有便携证据（portable/self-contained 等
// 字样）；installer=有安装证据（setup/install 字样或安装介质扩展名）；
// binary=可执行单文件但文件名给不出便携/安装证据（裸 .exe 两可，宁降级
// 不猜标）；package=系统包格式（msi/deb/rpm/apk 等）；archive=纯归档；
// meta=签名/清单/更新元数据（展示层过滤）。
type Form string

const (
	FormPortable  Form = "portable"
	FormInstaller Form = "installer"
	FormBinary    Form = "binary"
	FormPackage   Form = "package"
	FormArchive   Form = "archive"
	FormMeta      Form = "meta"
)

// AssetNote 一条发布物的展示元数据（JSON 契约进各模块 Release.Assets）。
type AssetNote struct {
	Platform Platform `json:"platform"`
	Form     Form     `json:"form"`
	Label    string   `json:"label"`   // 上游原始资产名（可复制/可查）
	Managed  bool     `json:"managed"` // 本条是否即 Hanxi 托管所选（调用方置）
}

// Notes 由资产名列表产出展示矩阵；managedName 为模块自家选定的托管资产
// （空串=无）。元数据条目（签名/清单/blockmap 等）整体过滤不进矩阵。
func Notes(names []string, managedName string) []AssetNote {
	out := make([]AssetNote, 0, len(names))
	for _, n := range names {
		if IsMetadata(n) {
			continue
		}
		p, f := Classify(n)
		if f == FormMeta {
			continue // 词表外清单类（*.txt 等）由形态判定兜底过滤
		}
		out = append(out, AssetNote{
			Platform: p, Form: f, Label: n,
			Managed: managedName != "" && strings.EqualFold(n, managedName),
		})
	}
	return out
}

// IsMetadata 签名/校验和/更新清单类附属文件——它们不是"发布物"，
// 进列表只会污染视觉（SHA256SUMS、*.sig、*.blockmap、latest*.yml 等）。
func IsMetadata(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch filepath.Ext(n) {
	case ".sig", ".blockmap", ".sbom":
		return true
	}
	base := strings.TrimSuffix(n, filepath.Ext(n))
	switch filepath.Ext(base) { // 双后缀：.tar.gz.sig 之类已被上面捕获
	case ".blockmap":
		return true
	}
	if strings.Contains(n, "sha256sums") || strings.Contains(n, "checksums") || strings.Contains(n, "checksum.txt") {
		return true
	}
	if strings.HasPrefix(n, "latest") && (strings.HasSuffix(n, ".yml") || n == "latest.json") {
		return true // tauri/electron 更新通道清单
	}
	switch n {
	case "version-policy.json", "macos-provenance.json":
		return true
	}
	if strings.HasSuffix(n, ".xml") && !strings.Contains(n, ".msi") {
		return true // msix 配套清单（NanaZip *.xml）
	}
	return false
}

// Classify 按文件名机械判定平台与形态。判不出的平台归 other、
// 形态归 archive——宁降级，不猜标。
//
// 次序即优先级：扩展名硬证据（.apk/.dmg/.msi 等）先走，平台字样
// （windows/macos/darwin/osx/linux/android/freebsd）次之，裸归档最后。
func Classify(name string) (Platform, Form) {
	n := strings.ToLower(strings.TrimSpace(name))
	ext := suffixOf(n) // 复合后缀（.tar.gz/.pkg.tar.zst）先归并，再取单段
	stem := strings.TrimSuffix(n, ext)

	switch ext {
	case ".apk":
		return PlatformAndroid, FormPackage
	case ".dmg":
		return PlatformMacOS, FormInstaller
	case ".msi", ".appx", ".msix", ".appinstaller", ".msixbundle":
		return PlatformWindows, FormPackage
	case ".deb", ".rpm", ".flatpak":
		return PlatformLinux, FormPackage
	case ".app":
		return PlatformMacOS, FormPortable
	case ".pkg", ".pkg.tar.zst":
		return PlatformLinux, FormPackage // Arch 包（rustdesk/subnetdesk .pkg.tar.zst）
	}

	// 平台字样信号（对 zip/tar.gz/exe/appimage 等跨平台容器生效）
	p := PlatformOther
	switch {
	case containsAny(stem, "windows", "-win.", "_win.", "win-x", "-win_", "net8.0-windows", "win7", "nsis", ".msi"):
		p = PlatformWindows
	case containsAny(stem, "macos", "-mac", "_mac", "osx", "darwin"):
		p = PlatformMacOS
	case containsAny(stem, "android"):
		p = PlatformAndroid
	case containsAny(stem, "freebsd"):
		p = PlatformFreeBSD
	case containsAny(stem, "linux", "appimage"):
		p = PlatformLinux
	}

	// 形态信号
	switch ext {
	case ".exe":
		if p == PlatformOther {
			p = PlatformWindows // 裸 .exe 上游惯例即 Windows（rufus/papertodo 形）
		}
		if containsAny(stem, "setup", "install", "安装") {
			return p, FormInstaller
		}
		if containsAny(stem, "portable", "self-contained", "no-runtime", "独立") {
			return p, FormPortable
		}
		return p, FormBinary // 裸 exe 无证据（rufus 便携 vs rustdesk 安装器两可）
	case ".appimage":
		return orLinux(p), FormPortable
	case ".zip", ".7z", ".rar":
		if containsAny(stem, "portable", "green", "self-contained", "no-runtime", "独立") {
			return p, FormPortable
		}
		if containsAny(stem, "setup", "nsis") {
			return p, FormInstaller // tauri nsis 归档载荷（MarkerOn x64-setup.nsis.zip）
		}
		// 刻意不以 "install" 子串定安装器：会误伤产品名（BCUninstaller 的
		// zip 是便携归档）；安装器 zip 上游必带 setup/nsis 强标记，够窄。
		return p, FormArchive
	case ".tar.gz", ".tgz":
		if strings.Contains(stem, ".app") {
			return orMac(p), FormPortable // tauri macOS 更新归档（MarkerOn_x64.app.tar.gz）
		}
		if containsAny(stem, "portable", "self-contained") {
			return p, FormPortable // bili23 linux_amd64_portable.tar.gz 形
		}
		return p, FormArchive
	case ".txt", ".json", ".yml", ".yaml":
		return p, FormMeta
	}
	return p, FormArchive
}

// suffixOf 发布物常见复合后缀归并（防 .tar.gz 被拆成 .gz 误判平台无关分支）。
func suffixOf(n string) string {
	for _, s := range []string{".pkg.tar.zst", ".tar.gz", ".tar.zst", ".tar.xz", ".tgz"} {
		if strings.HasSuffix(n, s) {
			return s
		}
	}
	return filepath.Ext(n)
}

func orLinux(p Platform) Platform {
	if p == PlatformOther {
		return PlatformLinux
	}
	return p
}

func orMac(p Platform) Platform {
	if p == PlatformOther {
		return PlatformMacOS
	}
	return p
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
