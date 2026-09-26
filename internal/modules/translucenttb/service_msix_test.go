package translucenttb

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/translucenttb/version"
	"hanxi/internal/platform/apppackage"
)

// MSIX 生命周期线单测纪律：真实 appx 命令一律不进单测——apppackage.API 与版本线
// seam 均以假实现注入，覆盖成功/未装/无 msix 版本/PowerShell 失败四类路径，
// 外加降级透传、安装后回查不符与缓存删除拦截等语义钉。

// fakeMsixPackages apppackage.API 测试替身：Query 按脚本逐次消费（脚本耗尽即
// 报"意外查询"，把调用次数钉进断言），Install/Uninstall/Activate 留痕。
type fakeMsixPackages struct {
	queryScript        []fakeMsixQuery
	installOpts        []apppackage.InstallOptions
	installErr         error
	uninstallErr       error
	activateErr        error
	uninstallFullNames []string
	activateIdentities []apppackage.Identity
}

type fakeMsixQuery struct {
	pkg *apppackage.Package
	err error
}

func (f *fakeMsixPackages) Query(context.Context, apppackage.Identity) (*apppackage.Package, error) {
	if len(f.queryScript) == 0 {
		return nil, errors.New("fake: 意外的 Query 调用（脚本已耗尽）")
	}
	item := f.queryScript[0]
	f.queryScript = f.queryScript[1:]
	return item.pkg, item.err
}

func (f *fakeMsixPackages) Install(_ context.Context, options apppackage.InstallOptions) (*apppackage.Package, error) {
	f.installOpts = append(f.installOpts, options)
	if f.installErr != nil {
		return nil, f.installErr
	}
	return nil, nil
}

func (f *fakeMsixPackages) Uninstall(_ context.Context, _ apppackage.Identity, packageFullName string) error {
	f.uninstallFullNames = append(f.uninstallFullNames, packageFullName)
	return f.uninstallErr
}

func (f *fakeMsixPackages) Activate(_ context.Context, identity apppackage.Identity) error {
	f.activateIdentities = append(f.activateIdentities, identity)
	return f.activateErr
}

// fakeMsixSource msixPackageManager 测试替身：PreparePackage/删除等留痕。
type fakeMsixSource struct {
	hasRelease  bool
	preparePath string
	prepareErr  error
	prepared    []string
	caches      []version.PackageCached
	removed     []string
	removeErr   error
}

func (f *fakeMsixSource) HasMsixRelease(string) bool { return f.hasRelease }

func (f *fakeMsixSource) PreparePackage(_ context.Context, v string, progress func(float64, string)) (string, error) {
	f.prepared = append(f.prepared, v)
	if f.prepareErr != nil {
		return "", f.prepareErr
	}
	if progress != nil {
		progress(100, "done")
	}
	return f.preparePath, nil
}

func (f *fakeMsixSource) PackageCachePaths() []version.PackageCached { return f.caches }

func (f *fakeMsixSource) RemovePackageCache(v string) error {
	f.removed = append(f.removed, v)
	return f.removeErr
}

// newMsixTestService 最小装配：门未注入的 LeaseHolder（Enter 直通）+ 两枚假缝。
func newMsixTestService(packages apppackage.API, msix msixPackageManager) *TranslucentTBService {
	return &TranslucentTBService{packages: packages, msix: msix, holder: extapi.NewLeaseHolder(ID)}
}

func TestGetMsixStateInstalled(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: &apppackage.Package{
		Version: "2026.2.0.0", PackageFullName: "28017CharlesMilette.TranslucentTB_2026.2.0.0_x64__v826wp6bftszj",
	}}}}
	msix := &fakeMsixSource{caches: []version.PackageCached{{Version: "2026.2", Size: 4173869}}}
	svc := newMsixTestService(packages, msix)

	state, err := svc.GetMsixState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.Installed || state.Version != "2026.2" {
		t.Errorf("注册态失真: installed=%v version=%q（四段 2026.2.0.0 须归一为 2026.2）", state.Installed, state.Version)
	}
	if state.PackageFamily != MsixPackageFamily {
		t.Errorf("PackageFamily=%q want 常量 %q", state.PackageFamily, MsixPackageFamily)
	}
	if len(state.Cache) != 1 || state.Cache[0].Version != "2026.2" {
		t.Errorf("缓存清单透传失败: %+v", state.Cache)
	}
	if len(packages.queryScript) != 0 {
		t.Errorf("Query 未按脚本消费完: 剩 %d", len(packages.queryScript))
	}
}

func TestGetMsixStateNotInstalled(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: nil}}}
	svc := newMsixTestService(packages, &fakeMsixSource{})

	state, err := svc.GetMsixState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Installed || state.Version != "" {
		t.Errorf("未装态应 installed=false version 空: %+v", state)
	}
	if state.PackageFamily != MsixPackageFamily {
		t.Errorf("PackageFamily 应恒回常量身份: %q", state.PackageFamily)
	}
	if state.Cache != nil {
		t.Errorf("零缓存应为 nil: %+v", state.Cache)
	}
}

func TestGetMsixStatePowerShellFailure(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{err: &apppackage.Error{
		Code: apppackage.CodePowerShellAbsent, Message: "系统 Windows PowerShell 不可用", Cause: errors.New("stat missing"),
	}}}}
	svc := newMsixTestService(packages, &fakeMsixSource{})

	_, err := svc.GetMsixState()
	if err == nil {
		t.Fatal("通道失败必须如实上抛")
	}
	if !strings.Contains(err.Error(), "TranslucentTB 打包版注册状态查询失败") || !strings.Contains(err.Error(), "PowerShell") {
		t.Errorf("中文归因缺失: %v", err)
	}
	var perr *apppackage.Error
	if !errors.As(err, &perr) || perr.Code != apppackage.CodePowerShellAbsent {
		t.Errorf("包装后错误码分支丢失: %v", err)
	}
}

func TestInstallMsixSuccess(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.2.0.0"}}}}
	msix := &fakeMsixSource{hasRelease: true, preparePath: `C:\hanxi\versions\translucenttb\packages\2026.2\bundle.msixbundle`}
	svc := newMsixTestService(packages, msix)

	if err := svc.InstallMsix(" 2026.2 "); err != nil {
		t.Fatal(err)
	}
	if len(msix.prepared) != 1 || msix.prepared[0] != "2026.2" {
		t.Errorf("版本号未修剪/透传: %v", msix.prepared)
	}
	if len(packages.installOpts) != 1 {
		t.Fatalf("Install 调用次数=%d", len(packages.installOpts))
	}
	opts := packages.installOpts[0]
	if opts.PackagePath != msix.preparePath {
		t.Errorf("PackagePath=%q want 版本线交回的自家缓存路径", opts.PackagePath)
	}
	if opts.Expected != msixIdentity {
		t.Errorf("Expected 身份偏离常量: %+v", opts.Expected)
	}
	if opts.ExpectedVersion != "2026.2.0.0" {
		t.Errorf("ExpectedVersion=%q want 2026.2.0.0（tag→四段映射实证规则）", opts.ExpectedVersion)
	}
	if !opts.AllowDowngrade {
		t.Error("打包线降级是主场景，AllowDowngrade 必须恒真透传 -ForceUpdateFromAnyVersion")
	}
	if len(packages.queryScript) != 0 {
		t.Errorf("安装后回查未发生")
	}
}

func TestInstallMsixNoMsixRelease(t *testing.T) {
	packages := &fakeMsixPackages{}
	msix := &fakeMsixSource{hasRelease: false}
	svc := newMsixTestService(packages, msix)

	err := svc.InstallMsix("2024.4")
	if err == nil || !strings.Contains(err.Error(), "无可用打包形态") {
		t.Fatalf("无 msix 版本须中文报因: %v", err)
	}
	if len(msix.prepared) != 0 || len(packages.installOpts) != 0 {
		t.Errorf("判定为无打包形态后不得触碰下载/部署: prepared=%v installs=%d", msix.prepared, len(packages.installOpts))
	}
}

func TestInstallMsixDeployFailureAttribution(t *testing.T) {
	packages := &fakeMsixPackages{installErr: &apppackage.Error{
		Code:      apppackage.CodeDependency,
		Message:   "应用包依赖或兼容性检查失败",
		Detail:    "第一行杂讯\nDeploy failed because the framework package DependencyMissing 0x80073CF3",
		HResult:   "0x80073CF3",
		Retryable: false,
	}}
	msix := &fakeMsixSource{hasRelease: true, preparePath: `C:\cache\bundle.msixbundle`}
	svc := newMsixTestService(packages, msix)

	err := svc.InstallMsix("2026.2")
	if err == nil {
		t.Fatal("部署失败必须报错")
	}
	msg := err.Error()
	for _, want := range []string{"TranslucentTB 打包版安装失败", "0x80073CF3", "framework package", "WinUI 2.8"} {
		if !strings.Contains(msg, want) {
			t.Errorf("归因话术缺 %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "第一行杂讯") {
		t.Errorf("明细应按尾部归因不整段吞入: %s", msg)
	}
	var perr *apppackage.Error
	if !errors.As(err, &perr) || perr.Code != apppackage.CodeDependency {
		t.Errorf("错误码分支丢失: %v", err)
	}
	// 0x80073CF3 属包注册基础设施族：话术须同时给出依赖与开发者模式两条出路
	for _, want := range []string{"包注册基础设施", "开发者模式", "便携版即正解"} {
		if !strings.Contains(msg, want) {
			t.Errorf("基础设施指路话术缺 %q: %s", want, msg)
		}
	}
}

// TestInstallMsixThinSystemInfraFailure 机主瘦系统实跑事故的回归钉：
// Add-AppxPackage 报 0x80073CF6（注册包失败）+ 内部 0x80073D05（商店缺席/
// AppModelUnlock 侧载授权缺失/部署通道不存在的瘦系统指纹），归因必须指路
// 开发者模式并如实判"打包线不可用，便携版即正解"。
func TestInstallMsixThinSystemInfraFailure(t *testing.T) {
	packages := &fakeMsixPackages{installErr: &apppackage.Error{
		Code:    apppackage.CodeDeployment,
		Message: "Windows 包操作失败",
		Detail:  "注册包失败，HRESULT 0x80073cf6；内部错误 0x80073D05",
		HResult: "0x80073CF6",
	}}
	msix := &fakeMsixSource{hasRelease: true, preparePath: `C:\cache\bundle.msixbundle`}
	svc := newMsixTestService(packages, msix)

	err := svc.InstallMsix("2026.2")
	if err == nil {
		t.Fatal("瘦系统注册失败必须报错")
	}
	msg := err.Error()
	for _, want := range []string{"0x80073CF6", "包注册基础设施", "设置→系统→对于开发人员", "开发者模式", "便携版即正解"} {
		if !strings.Contains(msg, want) {
			t.Errorf("基础设施归因缺 %q: %s", want, msg)
		}
	}
	var perr *apppackage.Error
	if !errors.As(err, &perr) || perr.Code != apppackage.CodeDeployment {
		t.Errorf("错误码分支丢失: %v", err)
	}
}

// TestInstallMsixFailureLoggedAsWarn 可观测性纪律回归：装失败必落 slog.Warn
// 一行摘要含 HRESULT（toast 一闪而没时代的取证靠日志）。
func TestInstallMsixFailureLoggedAsWarn(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	packages := &fakeMsixPackages{installErr: &apppackage.Error{
		Code: apppackage.CodeDeployment, Message: "Windows 包操作失败", HResult: "0x80073CF6",
	}}
	svc := newMsixTestService(packages, &fakeMsixSource{hasRelease: true, preparePath: `C:\cache\bundle.msixbundle`})
	if err := svc.InstallMsix("2026.2"); err == nil {
		t.Fatal("应失败")
	}
	logged := buf.String()
	for _, want := range []string{"translucenttb 打包版操作失败", "op=安装", "hresult=0x80073CF6", "0x80073CF6"} {
		if !strings.Contains(logged, want) {
			t.Errorf("WARN 留痕缺 %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, "\n\n") {
		t.Errorf("WARN 摘要应压成一行: %q", logged)
	}
}

// TestUninstallMsixFailureLoggedAsWarn 卸失败同样必落 WARN。
func TestUninstallMsixFailureLoggedAsWarn(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	packages := &fakeMsixPackages{
		queryScript:  []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.2.0.0", PackageFullName: "full-name-x"}}},
		uninstallErr: &apppackage.Error{Code: apppackage.CodeInUse, Message: "目标应用或相关资源正在使用中", HResult: "0x80073D02", Retryable: true},
	}
	svc := newMsixTestService(packages, &fakeMsixSource{})
	if err := svc.UninstallMsix(); err == nil {
		t.Fatal("应失败")
	}
	logged := buf.String()
	for _, want := range []string{"op=卸载", "code=APP_PACKAGE_IN_USE", "hresult=0x80073D02"} {
		if !strings.Contains(logged, want) {
			t.Errorf("卸载 WARN 留痕缺 %q: %s", want, logged)
		}
	}
}

// TestPrepareFailureLoggedAsWarn 准备失败（版本线错误，非通道错误码）也要留痕，
// 话术不套双重前缀。
func TestPrepareFailureLoggedAsWarn(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	svc := newMsixTestService(&fakeMsixPackages{}, &fakeMsixSource{
		hasRelease: true, prepareErr: errors.New("下载 msixbundle 失败: 网络断"),
	})
	err := svc.InstallMsix("2026.2")
	if err == nil || strings.Count(err.Error(), "安装包准备失败") != 1 {
		t.Fatalf("准备失败话术应单前缀: %v", err)
	}
	if !strings.Contains(buf.String(), "op=安装包准备") || !strings.Contains(buf.String(), "网络断") {
		t.Errorf("准备失败 WARN 留痕缺失: %s", buf.String())
	}
}

func TestInstallMsixPostCheckMismatch(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.1.0.0"}}}}
	msix := &fakeMsixSource{hasRelease: true, preparePath: `C:\cache\bundle.msixbundle`}
	svc := newMsixTestService(packages, msix)

	err := svc.InstallMsix("2026.2")
	if err == nil || !strings.Contains(err.Error(), "系统注册版本不是 2026.2") {
		t.Fatalf("回查版本不符须如实报实际: %v", err)
	}
}

func TestInstallMsixPrepareFailure(t *testing.T) {
	packages := &fakeMsixPackages{}
	msix := &fakeMsixSource{hasRelease: true, prepareErr: errors.New("msixbundle sha256 校验失败：期望 x，实际 y")}
	svc := newMsixTestService(packages, msix)

	err := svc.InstallMsix("2026.2")
	if err == nil || !strings.Contains(err.Error(), "安装包准备失败") || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("准备失败话术应带版本线原始因: %v", err)
	}
	if len(packages.installOpts) != 0 {
		t.Error("准备失败后不得部署")
	}
}

func TestUninstallMsixSuccessKeepsCache(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{
		{pkg: &apppackage.Package{Version: "2026.2.0.0", PackageFullName: "full-name-x"}},
		{pkg: nil},
	}}
	msix := &fakeMsixSource{caches: []version.PackageCached{{Version: "2026.2"}}}
	svc := newMsixTestService(packages, msix)

	if err := svc.UninstallMsix(); err != nil {
		t.Fatal(err)
	}
	if len(packages.uninstallFullNames) != 1 || packages.uninstallFullNames[0] != "full-name-x" {
		t.Errorf("未按实查完整名称卸载: %v", packages.uninstallFullNames)
	}
	if len(msix.removed) != 0 {
		t.Errorf("卸载不得触碰缓存文件: %v", msix.removed)
	}
}

func TestUninstallMsixNotInstalled(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: nil}}}
	svc := newMsixTestService(packages, &fakeMsixSource{})

	err := svc.UninstallMsix()
	if err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("未装态卸载须报「尚未安装」: %v", err)
	}
	if len(packages.uninstallFullNames) != 0 {
		t.Error("未装态不得下发 Remove")
	}
}

func TestUninstallMsixStillRegisteredAfterRemove(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{
		{pkg: &apppackage.Package{Version: "2026.2.0.0", PackageFullName: "full-name-x"}},
		{pkg: &apppackage.Package{Version: "2026.2.0.0"}},
	}}
	svc := newMsixTestService(packages, &fakeMsixSource{})

	err := svc.UninstallMsix()
	if err == nil || !strings.Contains(err.Error(), "仍显示") {
		t.Fatalf("回查未掉注册须如实报错: %v", err)
	}
}

func TestRemoveMsixCacheBlockedWhenRegistered(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.2.0.0"}}}}
	msix := &fakeMsixSource{}
	svc := newMsixTestService(packages, msix)

	err := svc.RemoveMsixCache("2026.2")
	if err == nil || !strings.Contains(err.Error(), "正在系统中注册") {
		t.Fatalf("装用中版本缓存须拦截: %v", err)
	}
	if len(msix.removed) != 0 {
		t.Errorf("拦截后不得删文件: %v", msix.removed)
	}
}

func TestRemoveMsixCacheAllowsOtherVersions(t *testing.T) {
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.2.0.0"}}}}
	msix := &fakeMsixSource{}
	svc := newMsixTestService(packages, msix)

	if err := svc.RemoveMsixCache(" 2026.1 "); err != nil {
		t.Fatal(err)
	}
	if len(msix.removed) != 1 || msix.removed[0] != "2026.1" {
		t.Errorf("非在用版本应透传修剪后的版本号删除: %v", msix.removed)
	}
}

func TestLaunchMsix(t *testing.T) {
	notInstalled := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: nil}}}
	svc := newMsixTestService(notInstalled, &fakeMsixSource{})
	if err := svc.LaunchMsix(); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("未装启动须报「尚未安装」: %v", err)
	}

	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.2.0.0"}}}}
	svc = newMsixTestService(packages, &fakeMsixSource{})
	if err := svc.LaunchMsix(); err != nil {
		t.Fatal(err)
	}
	if len(packages.activateIdentities) != 1 || packages.activateIdentities[0].AppID != MsixMainAppID ||
		packages.activateIdentities[0].Family != MsixPackageFamily {
		t.Errorf("激活身份错误: %+v", packages.activateIdentities)
	}
	if len(packages.queryScript) != 0 {
		t.Error("启动前确认查询缺失")
	}
}

func TestNormalizeMsixVersion(t *testing.T) {
	cases := map[string]string{
		"2026.2.0.0":  "2026.2",
		"2026.10.0.0": "2026.10",
		"2026.2.5.0":  "2026.2.5.0", // CI 构建号漂移形态不猜、原样
		"2026.2.0.1":  "2026.2.0.1",
		"2026.2":      "2026.2",
		"":            "",
		"1.2.3.4.5.6": "1.2.3.4.5.6",
	}
	for in, want := range cases {
		if got := normalizeMsixVersion(in); got != want {
			t.Errorf("normalizeMsixVersion(%q)=%q want %q", in, got, want)
		}
	}
}

func TestLastLineDetail(t *testing.T) {
	if got := lastLine("头行\n  尾行明细 "); got != "尾行明细" {
		t.Errorf("lastLine=%q", got)
	}
	long := strings.Repeat("冗", 260)
	got := lastLine("a\n" + long)
	if r := []rune(got); len(r) != 201 || !strings.HasPrefix(got, "…") {
		t.Errorf("超长明细应按 rune 截尾 200: runeLen=%d", len(r))
	}
	if lastLine("   ") != "" {
		t.Error("空白明细应回空串")
	}
}
