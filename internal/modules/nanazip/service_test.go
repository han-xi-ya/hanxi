package nanazip

import (
	"context"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/nanazip/version"
	"hanxi/internal/platform/apppackage"
)

type fakePackages struct {
	pkg         *apppackage.Package
	installed   apppackage.InstallOptions
	uninstalled string
	activated   bool
}

func (f *fakePackages) Query(context.Context, apppackage.Identity) (*apppackage.Package, error) {
	if f.pkg == nil {
		return nil, nil
	}
	copy := *f.pkg
	return &copy, nil
}
func (f *fakePackages) Install(_ context.Context, options apppackage.InstallOptions) (*apppackage.Package, error) {
	f.installed = options
	f.pkg = &apppackage.Package{Name: PackageName, Family: PackageFamily, Publisher: Publisher, Version: options.ExpectedVersion, PackageFullName: "full", Architecture: "x64", Status: "Ok"}
	return f.pkg, nil
}
func (f *fakePackages) Uninstall(_ context.Context, _ apppackage.Identity, fullName string) error {
	f.uninstalled = fullName
	f.pkg = nil
	return nil
}
func (f *fakePackages) Activate(context.Context, apppackage.Identity) error {
	f.activated = true
	return nil
}

type fakeVersions struct{ cached version.CachedPackage }

func (f *fakeVersions) ListReleases() ([]version.Release, error) {
	return []version.Release{{Version: "6.5.1800.0"}}, nil
}
func (f *fakeVersions) ListCached() ([]version.CachedPackage, error) {
	return []version.CachedPackage{f.cached}, nil
}

// EnsureCachedContext 替身：验收入口 ctx 已取消（与生产 Manager 同先验语义），
// 未取消才落缓存桩值——这样"停用即取消"在 service 层也有外部面可断言。
func (f *fakeVersions) EnsureCachedContext(ctx context.Context, v string, _ func(version.DownloadProgress)) (version.CachedPackage, error) {
	if err := ctx.Err(); err != nil {
		return version.CachedPackage{}, err
	}
	f.cached = version.CachedPackage{Version: v, Path: `C:\cache\NanaZip.msixbundle`}
	return f.cached, nil
}
func (f *fakeVersions) RemoveCached(string) error { return nil }

func TestPackageSnapshotUsesSystemState(t *testing.T) {
	packages := &fakePackages{pkg: &apppackage.Package{Version: "6.5.1800.0", PackageFullName: "full", Family: PackageFamily, Architecture: "x64", Status: "Ok"}}
	svc := newNanaZipService(nil, packages, &fakeVersions{}, extapi.NewLeaseHolder(ID))
	snapshot, err := svc.GetPackageSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Installed || snapshot.Version != "6.5.1800.0" || snapshot.PackageFullName != "full" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestLaunchUsesPackageAPI(t *testing.T) {
	packages := &fakePackages{pkg: &apppackage.Package{Version: "6.5.1800.0"}}
	svc := newNanaZipService(nil, packages, &fakeVersions{}, extapi.NewLeaseHolder(ID))
	if err := svc.Launch(); err != nil {
		t.Fatal(err)
	}
	if !packages.activated {
		t.Fatal("expected activation")
	}
}
