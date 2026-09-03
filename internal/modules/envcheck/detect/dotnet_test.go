package detect

import (
	"testing"
)

// runtimeOnlyInfo 为 2026-09-03 本机（仅安装运行时与桌面运行时、无 SDK）的 dotnet --info 实测输出。
const runtimeOnlyInfo = `
Host:
  Version:      8.0.13
  Architecture: x64
  Commit:       eba546b0f0
  RID:          win-x64

.NET SDKs installed:
  No SDKs were found.

.NET runtimes installed:
  Microsoft.NETCore.App 8.0.13 [C:\Program Files\dotnet\shared\Microsoft.NETCore.App]
  Microsoft.WindowsDesktop.App 8.0.13 [C:\Program Files\dotnet\shared\Microsoft.WindowsDesktop.App]

Other architectures found:
  None

Environment variables:
  Not set

global.json file:
  Not found
`

// sdkInfo 模拟多 SDK 多运行时机器（含预览版与 ASP.NET 族）的输出。
const sdkInfo = `
.NET SDK:
 Version:           9.0.100
 Commit:            5918f216c7
 Workload version:  9.0.100-manifests.6ca643b8

------------------
Host:
  Version:      9.0.0
  Architecture: x64

.NET SDKs installed:
  8.0.404 [C:\Program Files\dotnet\sdk]
  9.0.100 [C:\Program Files\dotnet\sdk]
  10.0.100-preview.5.25277.114 [C:\Program Files\dotnet\sdk]

.NET runtimes installed:
  Microsoft.AspNetCore.App 8.0.11 [C:\Program Files\dotnet\shared\Microsoft.AspNetCore.App]
  Microsoft.AspNetCore.App 9.0.0 [C:\Program Files\dotnet\shared\Microsoft.AspNetCore.App]
  Microsoft.NETCore.App 8.0.11 [C:\Program Files\dotnet\shared\Microsoft.NETCore.App]
  Microsoft.NETCore.App 8.0.13 [C:\Program Files\dotnet\shared\Microsoft.NETCore.App]
  Microsoft.NETCore.App 9.0.0 [C:\Program Files\dotnet\shared\Microsoft.NETCore.App]
  Microsoft.WindowsDesktop.App 8.0.11 [C:\Program Files\dotnet\shared\Microsoft.WindowsDesktop.App]

Other architectures found:
  x86
`

func TestDotNetDetectorParse(t *testing.T) {
	d := dotnetDetector{}
	if got := d.Parse(sdkInfo); got != "10.0.100-preview.5.25277.114" {
		t.Fatalf("sdk machine version=%q", got)
	}
	if got := d.Parse(runtimeOnlyInfo); got != "8.0.13" {
		t.Fatalf("runtime-only version=%q", got)
	}
	if got := d.Parse("The application '--version' does not exist."); got != "" {
		t.Fatalf("garbage version=%q", got)
	}
}

func TestDotNetDetectorParseDetails(t *testing.T) {
	d := dotnetDetector{}
	details := d.ParseDetails(sdkInfo)
	if details == nil || details.DotNet == nil {
		t.Fatal("sdk machine details missing")
	}
	got := details.DotNet
	if !equalStrings(got.SDKs, []string{"8.0.404", "9.0.100", "10.0.100-preview.5.25277.114"}) {
		t.Fatalf("sdks=%v", got.SDKs)
	}
	if !equalStrings(got.Runtimes, []string{"8.0.11", "8.0.13", "9.0.0"}) {
		t.Fatalf("runtimes=%v", got.Runtimes)
	}
	if !equalStrings(got.Desktops, []string{"8.0.11"}) || !equalStrings(got.AspNetCore, []string{"8.0.11", "9.0.0"}) {
		t.Fatalf("families=%v %v", got.Desktops, got.AspNetCore)
	}
	got = d.ParseDetails(runtimeOnlyInfo).DotNet
	if len(got.SDKs) != 0 || !equalStrings(got.Runtimes, []string{"8.0.13"}) || !equalStrings(got.Desktops, []string{"8.0.13"}) || len(got.AspNetCore) != 0 {
		t.Fatalf("runtime-only details=%#v", *got)
	}
	if d.ParseDetails("no versions here") != nil {
		t.Fatal("garbage details should be nil")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCompareDotNetVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"8.0.13", "8.0.11", 1},
		{"8.0.11", "8.0.13", -1},
		{"9.0.0", "8.0.13", 1},
		{"10.0.0", "9.9.9", 1},
		{"9.0.100", "9.0.100", 0},
		{"9.0.0", "9.0.0-rc.1", 1},
		{"9.0.0-preview.2", "9.0.0-preview.5", -1},
		{"9.0.0-preview.5.25277.114", "9.0.0-preview.5", 1},
	}
	for _, c := range cases {
		if got := compareDotNetVersion(c.a, c.b); got != c.want {
			t.Fatalf("compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if validDotNetVersion("9.0") || validDotNetVersion("9.0.0.1") || validDotNetVersion("v9.0.0") {
		t.Fatal("invalid shapes should be rejected")
	}
}
