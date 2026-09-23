// hostfeed 测试：curated 真名表驱动（全部出自 2026-09-23 对 28 家上游
// releases/latest 的实拉清单，缓存 .cache/w6probe/）+ 全量冒烟 + 元数据
// 过滤 + Notes 托管标记。新增上游命名形态时先在此补行，再动分类器。
package hostfeed

import "testing"

func TestClassifyCurated(t *testing.T) {
	cases := []struct {
		name     string
		wantPlat Platform
		wantForm Form
	}{
		// —— Windows 证据链 ——
		{"WindTerm_2.7.0_Windows_Portable_x86_64.zip", PlatformWindows, FormPortable},
		{"WindTerm_2.7.0_Windows_Portable_x86_32.zip", PlatformWindows, FormPortable},
		{"LiteMonitor_v1.3.6-win-x64.zip", PlatformWindows, FormArchive},
		{"ddns-go_6.17.7_windows_x86_64.zip", PlatformWindows, FormArchive},
		{"frp_0.71.0_windows_amd64.zip", PlatformWindows, FormArchive},
		{"CC-Switch-v3.20.4-Windows-Portable.zip", PlatformWindows, FormPortable},
		{"CC-Switch-v3.20.4-Windows.msi", PlatformWindows, FormPackage},
		{"keyviz_2.1.1_windows.msi", PlatformWindows, FormPackage},
		{"QuickLook-4.5.0.msi", PlatformWindows, FormPackage},
		{"QuickLook-4.5.0.appx", PlatformWindows, FormPackage},
		{"NanaZip_7.0.1843.0.msixbundle", PlatformWindows, FormPackage},
		{"TranslucentTB.appinstaller", PlatformWindows, FormPackage},
		{"TranslucentTB-portable-x64.zip", PlatformOther, FormPortable}, // 无 windows 字样：平台降级不猜标
		{"BCUninstaller_6.3.0_portable.7z", PlatformOther, FormPortable},
		{"BCUninstaller_6.3.0_net8.0-windows10.0.18362.0.zip", PlatformWindows, FormArchive},
		{"MarkerOn_2.10.1_x64-setup.nsis.zip", PlatformWindows, FormInstaller}, // nsis 字样=Windows
		{"MarkerOn_2.10.1_x64_zh-CN.msi.zip", PlatformWindows, FormArchive},    // .msi 字样=Windows
		// —— 裸 exe 三态：installer（setup 证据）/portable（自包含证据）/binary（无证据） ——
		{"Douzy-Setup-0.12.0.exe", PlatformWindows, FormInstaller},
		{"Paseo-Setup-0.9.1-x64.exe", PlatformWindows, FormInstaller},
		{"BCUninstaller_6.3.0_setup.exe", PlatformWindows, FormInstaller},
		{"Bili23-Downloader_2.15.0_windows_x64.exe", PlatformWindows, FormBinary},
		{"Bili23-Downloader_2.15.0_windows_x64_for_win7.exe", PlatformWindows, FormBinary},
		{"rustdesk-1.4.9-x86_64.exe", PlatformWindows, FormBinary},
		{"rufus-4.15.exe", PlatformWindows, FormBinary},
		{"rufus-4.15p.exe", PlatformWindows, FormBinary},
		{"MangoDisk-1.1.3-windows-portable.exe", PlatformWindows, FormPortable},
		{"PaperTodo-v3.31-win-x64-self-contained.exe", PlatformWindows, FormPortable},
		{"PaperTodo-v3.31-win-x64-no-runtime.exe", PlatformWindows, FormPortable},
		{"Termora-2.0.0-beta.16-windows-x86-64.exe", PlatformWindows, FormBinary},
		{"Recordly-windows-x64.exe", PlatformWindows, FormBinary}, // 实为 NSIS 无字样：诚实 binary
		// —— macOS ——
		{"keyviz_2.1.1_macos.dmg", PlatformMacOS, FormInstaller},
		{"termora-1.0.17-osx-x86-64.dmg", PlatformMacOS, FormInstaller},
		{"Douzy-0.12.0-arm64-mac.zip", PlatformMacOS, FormArchive},
		{"MangoDisk-1.1.3-macos.dmg", PlatformMacOS, FormInstaller},
		{"MarkerOn_x64.app.tar.gz", PlatformMacOS, FormPortable},
		{"Termora-2.0.0-beta.16-osx-x86-64.tar.gz", PlatformMacOS, FormArchive},
		// —— Linux ——
		{"CC-Switch-v3.20.4-Linux-x86_64.AppImage", PlatformLinux, FormPortable},
		{"Recordly-linux-x64.AppImage", PlatformLinux, FormPortable},
		{"Paseo-x86_64.AppImage", PlatformLinux, FormPortable},
		{"Bili23-Downloader_2.15.0_linux_amd64.deb", PlatformLinux, FormPackage},
		{"rustdesk-1.4.9-0.x86_64-suse.rpm", PlatformLinux, FormPackage},
		{"rustdesk-1.4.9-x86_64.flatpak", PlatformLinux, FormPackage},
		{"rustdesk-1.4.9-0-x86_64.pkg.tar.zst", PlatformLinux, FormPackage},
		{"Bili23-Downloader_2.15.0_linux_amd64_portable.tar.gz", PlatformLinux, FormPortable},
		{"termora-1.0.17-linux-x86-64.tar.gz", PlatformLinux, FormArchive},
		{"frp_0.71.0_linux_amd64.tar.gz", PlatformLinux, FormArchive},
		// —— Android ——
		{"FlClash-0.8.98-android-x86_64.apk", PlatformAndroid, FormPackage},
		{"paseo-v0.9.1-android.apk", PlatformAndroid, FormPackage},
		{"rustdesk-1.4.9-universal-signed.apk", PlatformAndroid, FormPackage},
		{"ddns-go_6.17.7_android_arm64.tar.gz", PlatformAndroid, FormArchive},
		// —— 其它平台/降级面 ——
		{"frp_0.71.0_freebsd_amd64.tar.gz", PlatformFreeBSD, FormArchive},
		{"Localisation_Pack_6.3.0.zip", PlatformOther, FormArchive},
		{"QuickLook-4.5.0.zip", PlatformOther, FormArchive},
		{"rustdesk-1.4.9-unsigned.tar.gz", PlatformOther, FormArchive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, f := Classify(c.name)
			if p != c.wantPlat || f != c.wantForm {
				t.Fatalf("Classify(%q) = (%s,%s)，期望 (%s,%s)", c.name, p, f, c.wantPlat, c.wantForm)
			}
		})
	}
}

func TestMetadataFiltering(t *testing.T) {
	meta := []string{
		"MarkerOn_2.10.1_x64-setup.exe.sig",
		"Paseo-Setup-0.9.1.exe.blockmap",
		"SHA256SUMS", "SHA256SUMS.txt", "checksums.txt", "frp_sha256_checksums.txt",
		"latest-linux.yml", "latest-mac.yml", "latest.yml", "latest.json",
		"version-policy.json", "macos-provenance.json",
		"NanaZip_7.0.1843.0.xml",
	}
	for _, n := range meta {
		if !IsMetadata(n) {
			t.Errorf("附属元数据必须被过滤: %s", n)
		}
	}
	real := []string{"QuickLook-4.5.0.msi", "NanaZip_7.0.1843.0_Binaries.zip", "latest-linux.yml.sig"}
	for i, n := range real {
		if i == 2 {
			continue // .sig 仍属元数据
		}
		if IsMetadata(n) {
			t.Errorf("真发布物不得被误滤: %s", n)
		}
	}
	if !IsMetadata("latest-linux.yml.sig") {
		t.Error("签名包裹的更新清单仍是元数据")
	}
}

// 全量冒烟：28 家实拉清单里所有名字都必须落在枚举内（分类可降级，平台/形态不许逃逸）。
func TestAllRealNamesStayInEnum(t *testing.T) {
	plats := map[Platform]bool{PlatformWindows: true, PlatformMacOS: true, PlatformLinux: true,
		PlatformAndroid: true, PlatformFreeBSD: true, PlatformOther: true}
	forms := map[Form]bool{FormPortable: true, FormInstaller: true, FormBinary: true,
		FormPackage: true, FormArchive: true, FormMeta: true}
	for _, n := range allRealAssets {
		if IsMetadata(n) {
			continue
		}
		p, f := Classify(n)
		if !plats[p] {
			t.Errorf("平台逃逸枚举: %s → %q", n, p)
		}
		if !forms[f] {
			t.Errorf("形态逃逸枚举: %s → %q", n, f)
		}
	}
}

func TestNotesManagedMark(t *testing.T) {
	names := []string{
		"WindTerm_2.7.0_Windows_Portable_x86_64.zip",
		"WindTerm_2.7.0_Mac_Portable_x86_64.dmg",
		"WindTerm_2.7.0_Linux_Portable_x86_64.zip",
		"SHASUMS256.txt",
	}
	notes := Notes(names, "WindTerm_2.7.0_Windows_Portable_x86_64.zip")
	if len(notes) != 3 {
		t.Fatalf("元数据必须过滤: %+v", notes)
	}
	var managed int
	for _, nt := range notes {
		if nt.Managed {
			managed++
			if nt.Label != names[0] {
				t.Fatalf("Managed 标错条目: %+v", nt)
			}
		}
	}
	if managed != 1 {
		t.Fatalf("恰一条本托管: %d", managed)
	}
}
