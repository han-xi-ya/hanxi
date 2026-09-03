package eartrumpet

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"hanxi/internal/platform/apppackage"
)

// fakePackages 按 PFN 区分直装/商店身份的注册结果，并记录操作。
type fakePackages struct {
	byFamily          map[string]*apppackage.Package
	queryErr          error
	activatedFamily   string
	uninstalledByName map[string]string
	installOpts       *apppackage.InstallOptions
}

func (f *fakePackages) Query(_ context.Context, id apppackage.Identity) (*apppackage.Package, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.byFamily[id.Family], nil
}

func (f *fakePackages) Activate(_ context.Context, id apppackage.Identity) error {
	// 与真实脚本一致：activate 内部查包，未安装返回类型化错误
	if f.byFamily[id.Family] == nil {
		return &apppackage.Error{Code: apppackage.CodeNotInstalled, Message: "应用包尚未安装"}
	}
	f.activatedFamily = id.Family
	return nil
}

func (f *fakePackages) Install(_ context.Context, opts apppackage.InstallOptions) (*apppackage.Package, error) {
	f.installOpts = &opts
	return &apppackage.Package{Version: opts.ExpectedVersion, Family: opts.Expected.Family}, nil
}

func (f *fakePackages) Uninstall(_ context.Context, id apppackage.Identity, packageFullName string) error {
	if f.uninstalledByName == nil {
		f.uninstalledByName = map[string]string{}
	}
	f.uninstalledByName[id.Family] = packageFullName
	return nil
}

type fakeOpener struct{ urls []string }

func (f *fakeOpener) OpenURL(url string) error {
	f.urls = append(f.urls, url)
	return nil
}

// fakeFetch 按 URL 返回预置响应；未配置的 URL 一律报错（视为不可用）。
type fakeFetch struct {
	bodies map[string][]byte
	calls  []string
}

func (f *fakeFetch) get(_ context.Context, rawURL string) ([]byte, error) {
	f.calls = append(f.calls, rawURL)
	if body, ok := f.bodies[rawURL]; ok {
		return body, nil
	}
	return nil, errors.New("offline")
}

type fakeDownload struct {
	called int
	err    error
}

func (f *fakeDownload) save(_ context.Context, _, dst string) error {
	f.called++
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(dst, []byte("bundle-content"), 0o600)
}

func installedPkg(version, family, arch string) *apppackage.Package {
	return &apppackage.Package{
		Version:         version,
		PackageFullName: PackageName + "_" + version + "_" + arch + "__" + familySuffix(family),
		Family:          family,
		Architecture:    arch,
		InstallLocation: `C:\Program Files\WindowsApps`,
		Status:          "Ok",
	}
}

func familySuffix(family string) string {
	if idx := strings.LastIndex(family, "_"); idx >= 0 {
		return family[idx+1:]
	}
	return family
}

const sampleAppInstaller = `<?xml version="1.0" encoding="utf-8"?>
<AppInstaller Uri="https://install.eartrumpet.app/master/EarTrumpet.Package.appinstaller" Version="2.3.0.20" xmlns="http://schemas.microsoft.com/appx/appinstaller/2017/2">
  <MainBundle Name="40459File-New-Project.EarTrumpet" Version="2.3.0.20" Publisher="CN=File-New-Project, O=File-New-Project, L=Purcellville, S=Virginia, C=US" Uri="https://install.eartrumpet.app/master/EarTrumpet.Package_2.3.0.20_x86.appxbundle" />
  <Dependencies>
    <Package Name="Microsoft.VCLibs.140.00.UWPDesktop" Publisher="CN=Microsoft Corporation, O=Microsoft Corporation, L=Redmond, S=Washington, C=US" Version="14.0.30704.0" ProcessorArchitecture="x86" Uri="https://aka.ms/Microsoft.VCLibs.x86.14.00.Desktop.appx" />
  </Dependencies>
  <UpdateSettings>
    <OnLaunch HoursBetweenUpdateChecks="0" />
  </UpdateSettings>
</AppInstaller>`

func remoteFetch() *fakeFetch {
	return &fakeFetch{bodies: map[string][]byte{appInstallerURL: []byte(sampleAppInstaller)}}
}

func newTestService(pkgs *fakePackages, opener URLOpener, fetch *fakeFetch, dl *fakeDownload) *EarTrumpetService {
	return newEarTrumpetService(pkgs, opener, fetch.get, dl.save)
}

func TestGetStatusReportsManagedAndStoreCoexist(t *testing.T) {
	pkgs := &fakePackages{byFamily: map[string]*apppackage.Package{
		managedIdentity.Family: installedPkg("2.3.0.20", managedIdentity.Family, "X86"),
		storeIdentity.Family:   installedPkg("2.3.0.0", storeIdentity.Family, "X86"),
	}}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), &fakeDownload{})

	snap, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Installed || snap.Version != "2.3.0.20" {
		t.Fatalf("直装状态错误: %+v", snap)
	}
	if !snap.StoreCoexist || snap.StoreVersion != "2.3.0.0" {
		t.Fatalf("并存标记错误: %+v", snap)
	}
}

func TestGetStatusCleanInstall(t *testing.T) {
	pkgs := &fakePackages{byFamily: map[string]*apppackage.Package{
		managedIdentity.Family: installedPkg("2.3.0.20", managedIdentity.Family, "X86"),
	}}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), &fakeDownload{})

	snap, err := svc.GetStatus()
	if err != nil || snap.StoreCoexist || !snap.Installed {
		t.Fatalf("got %+v err=%v", snap, err)
	}
}

func TestLaunch(t *testing.T) {
	pkgs := &fakePackages{byFamily: map[string]*apppackage.Package{
		managedIdentity.Family: installedPkg("2.3.0.20", managedIdentity.Family, "X86"),
	}}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), &fakeDownload{})

	if err := svc.Launch(); err != nil {
		t.Fatal(err)
	}
	if pkgs.activatedFamily != managedIdentity.Family {
		t.Fatalf("应激活直装身份: %s", pkgs.activatedFamily)
	}

	notInstalled := newTestService(&fakePackages{}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	if err := notInstalled.Launch(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("未安装时应返回友好错误: %v", err)
	}
}

func TestUninstall(t *testing.T) {
	pkg := installedPkg("2.3.0.20", managedIdentity.Family, "X86")
	pkgs := &fakePackages{byFamily: map[string]*apppackage.Package{managedIdentity.Family: pkg}}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), &fakeDownload{})

	if err := svc.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if pkgs.uninstalledByName[managedIdentity.Family] != pkg.PackageFullName {
		t.Fatalf("应卸载 Query 返回的实际全名: %v", pkgs.uninstalledByName)
	}

	notInstalled := newTestService(&fakePackages{}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	if err := notInstalled.Uninstall(); err == nil {
		t.Fatal("未安装无需卸载")
	}
}

func TestRemoteVersion(t *testing.T) {
	svc := newTestService(&fakePackages{}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	version, err := svc.GetRemoteVersion()
	if err != nil || version != "2.3.0.20" {
		t.Fatalf("got %q err %v", version, err)
	}
}

func TestInstallHappyPath(t *testing.T) {
	pkgs := &fakePackages{}
	dl := &fakeDownload{}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), dl)

	version, err := svc.Install()
	if err != nil {
		t.Fatal(err)
	}
	if version != "2.3.0.20" {
		t.Fatalf("version=%q", version)
	}
	if dl.called != 1 || pkgs.installOpts == nil {
		t.Fatalf("download=%d installOpts=%v", dl.called, pkgs.installOpts)
	}
	opts := pkgs.installOpts
	if opts.Expected.Family != managedIdentity.Family {
		t.Fatalf("应安装到直装身份: %+v", opts.Expected)
	}
	if !strings.HasSuffix(opts.PackagePath, ".appxbundle") || opts.ExpectedVersion != "2.3.0.20" {
		t.Fatalf("安装参数错误: %+v", opts)
	}
	if len(opts.Dependencies) != 1 || !strings.HasPrefix(opts.Dependencies[0], "https://aka.ms/") {
		t.Fatalf("依赖未透传: %+v", opts.Dependencies)
	}
}

func TestInstallBlockedWhenStoreInstalled(t *testing.T) {
	pkgs := &fakePackages{byFamily: map[string]*apppackage.Package{
		storeIdentity.Family: installedPkg("2.3.0.0", storeIdentity.Family, "X86"),
	}}
	dl := &fakeDownload{}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), dl)

	if _, err := svc.Install(); err == nil {
		t.Fatal("与商店版并存应被拒绝")
	}
	if dl.called != 0 || pkgs.installOpts != nil {
		t.Fatal("拒绝时不应发生下载或安装")
	}
}

func TestInstallIdempotentWhenLatest(t *testing.T) {
	pkgs := &fakePackages{byFamily: map[string]*apppackage.Package{
		managedIdentity.Family: installedPkg("2.3.0.20", managedIdentity.Family, "X86"),
	}}
	dl := &fakeDownload{}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), dl)

	version, err := svc.Install()
	if err != nil || version != "2.3.0.20" {
		t.Fatalf("got %q err %v", version, err)
	}
	if dl.called != 0 || pkgs.installOpts != nil {
		t.Fatal("已最新时不应重复下载安装")
	}
}

func TestInstallRejectsChecksumMismatch(t *testing.T) {
	wingetURL := "https://raw.githubusercontent.com/microsoft/winget-pkgs/master/manifests/f/File-New-Project/EarTrumpet/2.3.0.20/File-New-Project.EarTrumpet.installer.yaml"
	fetcher := remoteFetch()
	fetcher.bodies[wingetURL] = []byte("InstallerSha256: 0000000000000000000000000000000000000000000000000000000000000000\n")
	pkgs := &fakePackages{}
	svc := newTestService(pkgs, &fakeOpener{}, fetcher, &fakeDownload{})

	if _, err := svc.Install(); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("交叉校验失败应阻断安装: %v", err)
	}
	if pkgs.installOpts != nil {
		t.Fatal("校验失败后不应调用 Install")
	}
}

func TestInstallPropagatesOffline(t *testing.T) {
	fetcher := &fakeFetch{bodies: map[string][]byte{}} // 清单也取不到，且无缓存
	svc := newTestService(&fakePackages{}, &fakeOpener{}, fetcher, &fakeDownload{})
	if _, err := svc.Install(); err == nil {
		t.Fatal("离线且无缓存快照时应报错")
	}
}

func TestOpenRepo(t *testing.T) {
	opener := &fakeOpener{}
	svc := newTestService(&fakePackages{}, opener, remoteFetch(), &fakeDownload{})

	if err := svc.OpenRepo(); err != nil {
		t.Fatal(err)
	}
	if len(opener.urls) != 1 || opener.urls[0] != "https://github.com/File-New-Project/EarTrumpet" {
		t.Fatalf("URL 不符: %v", opener.urls)
	}
}
