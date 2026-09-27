package translucenttb

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/translucenttb/version"
	"hanxi/internal/platform/apppackage"
)

// MSIX 生命周期线单测纪律：真实 appx 命令一律不进单测——apppackage.API 与版本线
// seam 均以假实现注入，覆盖成功/未装/无 msix 版本/PowerShell 失败四类路径，
// 外加降级透传、安装后回查不符与缓存删除拦截等语义钉。

// fakeMsixPackages apppackage.API 测试替身：Query 按脚本逐次消费（脚本耗尽即
// 报"意外查询"，把调用次数钉进断言），Install/Uninstall/Activate 留痕；
// IsRegisteredAllUsers 模拟可选全用户探测能力（次数留痕供"成本递增序"钉）。
type fakeMsixPackages struct {
	queryScript        []fakeMsixQuery
	installOpts        []apppackage.InstallOptions
	installErr         error
	uninstallErr       error
	activateErr        error
	uninstallFullNames []string
	activateIdentities []apppackage.Identity
	allUsersRegistered *bool
	allUsersErr        error
	allUsersCalls      int
}

func (f *fakeMsixPackages) IsRegisteredAllUsers(context.Context, apppackage.Identity) (bool, error) {
	f.allUsersCalls++
	if f.allUsersErr != nil {
		return false, f.allUsersErr
	}
	if f.allUsersRegistered == nil {
		return false, errors.New("fake: 未编排全用户在册结果")
	}
	return *f.allUsersRegistered, nil
}

// noProberPackages 只实现 apppackage.API 必选面的通道替身——模拟孤儿闸①
// "可选能力缺席"形态（能力未提供必须按探测失败拒动，不得静默当无册）。
type noProberPackages struct{}

func (noProberPackages) Query(context.Context, apppackage.Identity) (*apppackage.Package, error) {
	return nil, nil // 未注册形：孤儿探测走"目录在场但能力缺席"分支
}
func (noProberPackages) Install(context.Context, apppackage.InstallOptions) (*apppackage.Package, error) {
	return nil, errors.New("fake: 不应触达")
}
func (noProberPackages) Uninstall(context.Context, apppackage.Identity, string) error {
	return errors.New("fake: 不应触达")
}
func (noProberPackages) Activate(context.Context, apppackage.Identity) error {
	return errors.New("fake: 不应触达")
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

// fakeMsixSource msixPackageManager 测试替身：PreparePackage/删除等留痕；
// progressScript 编排版本线进度回调（空=保留一笔 done 的既有用例基线）。
type fakeMsixSource struct {
	hasRelease     bool
	preparePath    string
	prepareErr     error
	prepared       []string
	progressScript []fakeMsixProgressEvent
	caches         []version.PackageCached
	removed        []string
	removeErr      error
}

type fakeMsixProgressEvent struct {
	percent float64
	stage   string
}

func (f *fakeMsixSource) HasMsixRelease(string) bool { return f.hasRelease }

func (f *fakeMsixSource) PreparePackage(_ context.Context, v string, progress func(float64, string)) (string, error) {
	f.prepared = append(f.prepared, v)
	if f.prepareErr != nil {
		return "", f.prepareErr
	}
	if progress != nil {
		if len(f.progressScript) == 0 {
			progress(100, "done")
		}
		for _, ev := range f.progressScript {
			progress(ev.percent, ev.stage)
		}
	}
	return f.preparePath, nil
}

func (f *fakeMsixSource) PackageCachePaths() []version.PackageCached { return f.caches }

func (f *fakeMsixSource) RemovePackageCache(v string) error {
	f.removed = append(f.removed, v)
	return f.removeErr
}

// newMsixTestService 最小装配：门未注入的 LeaseHolder（Enter 直通）+ 两枚假缝；
// orphan 缝注入"目录恒不在场"替身——既有用例不真触盘、不起全用户探测，
// 孤儿主题用例各自覆写 svc.orphan。
func newMsixTestService(packages apppackage.API, msix msixPackageManager) *TranslucentTBService {
	return &TranslucentTBService{packages: packages, msix: msix, holder: extapi.NewLeaseHolder(ID), orphan: orphanHooksAbsent()}
}

// orphanHooksAbsent 恒"无孤儿"的孤儿缝替身（dir 给常量形制路径但 exists 恒假）。
func orphanHooksAbsent() orphanHooks {
	return orphanHooks{
		dir:    func() (string, error) { return `C:\fake\AppData\Local\Packages\` + MsixPackageFamily, nil },
		exists: func(string) (bool, error) { return false, nil },
		walk:   func(string) (*orphanInventory, error) { return nil, errors.New("fake: 不应触达") },
		rename: func(string, string) error { return errors.New("fake: 不应触达") },
		now:    func() time.Time { return time.Date(2026, 9, 27, 15, 4, 5, 0, time.Local) },
	}
}

// fakeOrphanFS 孤儿文件系统替身：dir 在场、extra 编排撞名/缺席、rename 留痕。
// exists 缺省口径：dir 恒真，extra 显式编排的路径按表回，其余恒假。
type fakeOrphanFS struct {
	dir       string
	extra     map[string]bool
	inv       *orphanInventory
	walkErr   error
	renamed   [][2]string
	renameErr error
	nowTime   time.Time
}

func (f *fakeOrphanFS) hooks() orphanHooks {
	now := f.nowTime
	if now.IsZero() {
		now = time.Date(2026, 9, 27, 15, 4, 5, 0, time.Local)
	}
	return orphanHooks{
		dir: func() (string, error) { return f.dir, nil },
		exists: func(p string) (bool, error) {
			if v, ok := f.extra[p]; ok {
				return v, nil
			}
			return p == f.dir, nil
		},
		walk: func(string) (*orphanInventory, error) {
			if f.walkErr != nil {
				return nil, f.walkErr
			}
			inv := f.inv
			if inv == nil {
				inv = &orphanInventory{}
			}
			return inv, nil
		},
		rename: func(oldPath, newPath string) error {
			f.renamed = append(f.renamed, [2]string{oldPath, newPath})
			return f.renameErr
		},
		now: func() time.Time { return now },
	}
}

func boolPtr(v bool) *bool { return &v }

// captureMsixLogs 捕获 slog 输出（指定级别起）；返回缓冲与还原函数。
func captureMsixLogs(t *testing.T, level slog.Level) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	return &buf, func() { slog.SetDefault(restore) }
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
	// 依赖档兜底附带基础设施归因（"也可能是"从句），但 2026-09-27 真机实证
	// 改判后不再开"开发者模式"冤枉路，也不得误触孤儿数据档。
	for _, want := range []string{"包注册基础设施", "便携版即正解"} {
		if !strings.Contains(msg, want) {
			t.Errorf("基础设施指路话术缺 %q: %s", want, msg)
		}
	}
	for _, bad := range []string{"开发者模式", "检测到上次安装的孤儿数据阻碍注册"} {
		if strings.Contains(msg, bad) {
			t.Errorf("不该出现的指路（冤枉路/串档）%q: %s", bad, msg)
		}
	}
}

// TestInstallMsixThinSystemInfraFailure 纯基础设施档回归钉（2026-09-27 两档
// 分流改判后口径）：CF6+D05 无删数据伴生指纹 → 维持"缺包注册基础设施"档，
// 话术只陈述事实判定与便携正解兜底；"开启开发者模式"系原话术实锤冤枉路
// （同机孤儿隔离后未开开发者模式即 INSTALL-OK），必须不再出现。
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
	for _, want := range []string{"0x80073CF6", "包注册基础设施", "便携版即正解"} {
		if !strings.Contains(msg, want) {
			t.Errorf("基础设施归因缺 %q: %s", want, msg)
		}
	}
	for _, bad := range []string{"开发者模式", "设置→系统→对于开发人员", "检测到上次安装的孤儿数据阻碍注册"} {
		if strings.Contains(msg, bad) {
			t.Errorf("冤枉路/孤儿档话术不得残留: %q → %s", bad, msg)
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

// ---------- 归因表两档分流（2026-09-27 孤儿数据档扩列，缺陷一） ----------

// msixOrphanMarker 与 msixOrphanGuide/前端 MSIX_ORPHAN_MARKER 同文钉的判档短语。
const msixOrphanMarker = "检测到上次安装的孤儿数据阻碍注册"

// TestDescribeMsixFailureTwoTierSplit 两档分流判据表：CF6/D05 单独在场走
// 基础设施档（无冤枉路新话术）；伴生删数据指纹（0x800703FA / "删除…应用程序
// 数据" / "注册 windows.stateExtension"）任一即升孤儿档。Detail 全文匹配口径
// ——证据埋进超长明细尾部（超出话术 200-rune 截尾）也不掉档。
func TestDescribeMsixFailureTwoTierSplit(t *testing.T) {
	cases := []struct {
		name string
		err  *apppackage.Error
		// tier: orphan|infra|other
		tier       string
		wantAbsent []string
	}{
		{
			name: "CF6+D05 无伴生 → 基础设施档",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Windows 包操作失败",
				Detail: "部署失败：注册包失败", HResult: "0x80073CF6"},
			tier:       "infra",
			wantAbsent: []string{"开发者模式", msixOrphanMarker},
		},
		{
			name: "CF6 伴生 0x800703FA → 孤儿档",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Windows 包操作失败",
				Detail: "删除现有应用数据失败 0x800703fa（找不到指定的模块）", HResult: "0x80073CF6"},
			tier:       "orphan",
			wantAbsent: []string{"开发者模式", "包注册基础设施"},
		},
		{
			name: "D05 伴生「删除…应用程序数据」真机原文 → 孤儿档",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Windows 包操作失败",
				Detail: "部署失败: 删除程序包先前已有的应用程序数据时出错，内部错误 0x80073D05", HResult: ""},
			tier:       "orphan",
			wantAbsent: []string{"开发者模式"},
		},
		{
			name: "CF6 伴生「注册 windows.stateExtension」→ 孤儿档",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Windows 包操作失败",
				Detail: "注册扩展 windows.stateExtension 失败", HResult: "0x80073CF6"},
			tier:       "orphan",
			wantAbsent: []string{"开发者模式"},
		},
		{
			name: "英文 Register windows.stateExtension → 孤儿档",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Deployment failed",
				Detail: "Failed to register windows.stateExtension for the package", HResult: "0x80073CF6"},
			tier:       "orphan",
			wantAbsent: nil,
		},
		{
			name: "stateExtension 无注册锚 → 维持基础设施档（判据收紧不宽松）",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Deployment failed",
				Detail: "windows.stateExtension is disabled by policy", HResult: "0x80073CF6"},
			tier:       "infra",
			wantAbsent: []string{msixOrphanMarker, "开发者模式"},
		},
		{
			name: "0x800703FA 无 CF6/D05 底座 → 不升孤儿档（两档判据均以基础设施码为底）",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Deployment failed",
				Detail: "0x800703FA 找不到指定的模块", HResult: "0x800703FA"},
			tier:       "other",
			wantAbsent: []string{msixOrphanMarker, "包注册基础设施"},
		},
		{
			name: "孤儿证据埋超长 Detail 尾部（全文口径不吃话术截尾）",
			err: &apppackage.Error{Code: apppackage.CodeDeployment, Message: "Windows 包操作失败",
				Detail:  strings.Repeat("冗长部署噪声", 120) + "；删除程序包先前已有的应用程序数据时出错 0x800703FA",
				HResult: "0x80073D05"},
			tier:       "orphan",
			wantAbsent: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := describeMsixFailure("安装", tc.err).Error()
			switch tc.tier {
			case "orphan":
				if !strings.Contains(msg, msixOrphanMarker) {
					t.Errorf("孤儿档短语缺失: %s", msg)
				}
				if !strings.Contains(msg, "🧹 清理后重试") {
					t.Errorf("孤儿档须指路清理钮: %s", msg)
				}
			case "infra":
				if !strings.Contains(msg, "包注册基础设施") || !strings.Contains(msg, "便携版即正解") {
					t.Errorf("基础设施档话术缺失: %s", msg)
				}
			}
			for _, bad := range tc.wantAbsent {
				if strings.Contains(msg, bad) {
					t.Errorf("不该出现 %q: %s", bad, msg)
				}
			}
		})
	}
}

// msixIncidentActivityChain 现网事故部署事件日志 ActivityID
// {b7c91a2e-…} 链三条 message 原文样本（2026-09-27 机主瘦系统，
// Add-AppxPackage 拒装）：删旧应用数据缺组件 0x800703FA → 内部错误
// 0x80073D05 → 注册拒绝 0x80073CF6；孤儿目录改名挪开后即 INSTALL-OK。
// 回归钉：链中携带删数据指纹的 message 单独入 Detail 也必进孤儿档。
var msixIncidentActivityChain = []string{
	"部署操作失败: 删除包 28017CharlesMilette.TranslucentTB_v826wp6bftszj 先前已有的应用程序数据时出错，错误 0x800703FA (找不到指定的模块。)。ActivityId: {b7c91a2e-4f63-3d21-9c58-6a2f0e77d3c1}",
	"部署操作内部错误 0x80073D05，注册扩展 windows.stateExtension 失败。ActivityId: {b7c91a2e-4f63-3d21-9c58-6a2f0e77d3c1}",
	"部署失败: 注册包失败，错误 0x80073CF6；内部错误: 0x80073D05。ActivityId: {b7c91a2e-4f63-3d21-9c58-6a2f0e77d3c1}",
}

func TestInstallMsixOrphanDataIncidentRegression(t *testing.T) {
	joined := strings.Join(msixIncidentActivityChain, "\n")

	// 全链入 Detail（真机 Add-AppxPackage 异常文本常带完整活动链）→ 孤儿档，
	// 且经 InstallMsix 全链归因 + WARN 留痕。
	buf, restore := captureMsixLogs(t, slog.LevelWarn)
	defer restore()
	packages := &fakeMsixPackages{installErr: &apppackage.Error{
		Code: apppackage.CodeDeployment, Message: "Windows 包操作失败", Detail: joined, HResult: "0x80073CF6",
	}}
	svc := newMsixTestService(packages, &fakeMsixSource{hasRelease: true, preparePath: `C:\cache\bundle.msixbundle`})
	err := svc.InstallMsix("2026.2")
	if err == nil {
		t.Fatal("事故链必须报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, msixOrphanMarker) || !strings.Contains(msg, "🧹 清理后重试") {
		t.Errorf("现网事故链须归孤儿档并指路清理钮: %s", msg)
	}
	if strings.Contains(msg, "开发者模式") {
		t.Errorf("冤枉路话术不得再对本事故出货: %s", msg)
	}
	if !strings.Contains(buf.String(), "op=安装") {
		t.Errorf("失败留痕缺失: %s", buf.String())
	}

	// 分条单独入 Detail：①②自带删数据指纹 → 孤儿档；③仅基础设施码 → 维持
	// 基础设施档（判据不放宽，宁缺毋滥）。
	for i, sample := range msixIncidentActivityChain {
		tierOrphan := msixOrphanFailure(&apppackage.Error{Detail: sample, HResult: "0x80073CF6"})
		if want := i < 2; tierOrphan != want {
			t.Errorf("链样本 %d 分流判据翻转: orphan=%v want=%v（%s）", i+1, tierOrphan, want, sample)
		}
	}
}

// ---------- 孤儿数据：路径组装 / GetMsixState 只读探测 / 三道闸隔离 ----------

func TestMsixOrphanDataDirLiteral(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
	dir, err := msixOrphanDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(`C:\Users\tester\AppData\Local`, "Packages", MsixPackageFamily); dir != want {
		t.Errorf("路径组装偏离常量口径: %q want %q", dir, want)
	}
	t.Setenv("LOCALAPPDATA", "  ")
	if _, err := msixOrphanDataDir(); err == nil || !strings.Contains(err.Error(), "LOCALAPPDATA") {
		t.Errorf("环境变量缺席须中文报因: %v", err)
	}
}

func TestGetMsixStateOrphanProbe(t *testing.T) {
	orphanDir := `C:\fake\AppData\Local\Packages\` + MsixPackageFamily

	t.Run("目录在场+全用户无册 → orphans 坐实且给出精确路径", func(t *testing.T) {
		packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: nil}}, allUsersRegistered: boolPtr(false)}
		svc := newMsixTestService(packages, &fakeMsixSource{})
		svc.orphan = (&fakeOrphanFS{dir: orphanDir}).hooks()
		state, err := svc.GetMsixState()
		if err != nil {
			t.Fatal(err)
		}
		if !state.OrphanData || state.OrphanPath != orphanDir {
			t.Errorf("孤儿态未坐实: %+v", state)
		}
		if packages.allUsersCalls != 1 {
			t.Errorf("全用户探测应恰一次: %d", packages.allUsersCalls)
		}
	})

	t.Run("目录不在场 → orphans 假且零 PowerShell 探测（成本递增序钉）", func(t *testing.T) {
		packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: nil}}, allUsersRegistered: boolPtr(false)}
		svc := newMsixTestService(packages, &fakeMsixSource{})
		state, err := svc.GetMsixState() // 缺省替身 exists 恒假
		if err != nil {
			t.Fatal(err)
		}
		if state.OrphanData || state.OrphanPath != "" {
			t.Errorf("无目录不得报孤儿: %+v", state)
		}
		if packages.allUsersCalls != 0 {
			t.Errorf("目录不在场不得起全用户探测: %d", packages.allUsersCalls)
		}
	})

	t.Run("当前用户在册 → 数据是活数据，orphan 探测整段跳过", func(t *testing.T) {
		packages := &fakeMsixPackages{
			queryScript:        []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.2.0.0"}}},
			allUsersRegistered: boolPtr(false),
		}
		svc := newMsixTestService(packages, &fakeMsixSource{})
		svc.orphan = (&fakeOrphanFS{dir: orphanDir}).hooks()
		state, err := svc.GetMsixState()
		if err != nil {
			t.Fatal(err)
		}
		if state.OrphanData || packages.allUsersCalls != 0 {
			t.Errorf("在册态串档: orphans=%v allUsersCalls=%d", state.OrphanData, packages.allUsersCalls)
		}
	})

	t.Run("探测无法确证（在册/查询失败/能力缺席）→ 宁缺毋滥判非孤儿", func(t *testing.T) {
		packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: nil}}, allUsersRegistered: boolPtr(true)}
		svc := newMsixTestService(packages, &fakeMsixSource{})
		svc.orphan = (&fakeOrphanFS{dir: orphanDir}).hooks()
		if state, err := svc.GetMsixState(); err != nil || state.OrphanData {
			t.Errorf("他人在册仍报孤儿: %+v %v", state, err)
		}

		packages = &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: nil}}, allUsersErr: errors.New("非管理员下 -AllUsers 受限")}
		svc = newMsixTestService(packages, &fakeMsixSource{})
		svc.orphan = (&fakeOrphanFS{dir: orphanDir}).hooks()
		if state, err := svc.GetMsixState(); err != nil || state.OrphanData {
			t.Errorf("探测失败仍报孤儿: %+v %v", state, err)
		}

		svc = newMsixTestService(noProberPackages{}, &fakeMsixSource{})
		if state, err := svc.GetMsixState(); err != nil || state.OrphanData {
			t.Errorf("能力缺席仍报孤儿: %+v %v", state, err)
		}
	})
}

// TestCleanMsixOrphanGate1RefusesRegistered 闸①：全用户查得在册 → 拒动并
// 点名"包仍在册请直接卸载"，改名绝不发生。
func TestCleanMsixOrphanGate1RefusesRegistered(t *testing.T) {
	fs := &fakeOrphanFS{dir: `C:\fake\AppData\Local\Packages\` + MsixPackageFamily}
	packages := &fakeMsixPackages{allUsersRegistered: boolPtr(true)}
	svc := newMsixTestService(packages, &fakeMsixSource{})
	svc.orphan = fs.hooks()

	err := svc.CleanMsixOrphan()
	if err == nil || !strings.Contains(err.Error(), "仍在册") || !strings.Contains(err.Error(), "卸载") {
		t.Fatalf("在册须拒动并指引卸载: %v", err)
	}
	if len(fs.renamed) != 0 {
		t.Errorf("闸①拒后不得改名: %v", fs.renamed)
	}
}

// TestCleanMsixOrphanGate1RefusesProbeFailure 闸①保守面：探测失败与能力
// 缺席都按"无法证明无注册"拒动；探测通道错误按值并入话术，不再套归因表
// （查询失败不该开出"缺 Store/孤儿数据"等不相干指引）。
func TestCleanMsixOrphanGate1RefusesProbeFailure(t *testing.T) {
	fs := &fakeOrphanFS{dir: `C:\fake\AppData\Local\Packages\` + MsixPackageFamily}
	svc := newMsixTestService(&fakeMsixPackages{allUsersErr: &apppackage.Error{
		Code: apppackage.CodePowerShellAbsent, Message: "系统 Windows PowerShell 不可用",
	}}, &fakeMsixSource{})
	svc.orphan = fs.hooks()
	err := svc.CleanMsixOrphan()
	if err == nil || !strings.Contains(err.Error(), "无法确认") || !strings.Contains(err.Error(), "PowerShell") {
		t.Fatalf("探测失败须如实拒动: %v", err)
	}
	if strings.Contains(err.Error(), msixOrphanMarker) || strings.Contains(err.Error(), "包注册基础设施") {
		t.Errorf("探测失败话术不得再套归因表: %v", err)
	}
	if len(fs.renamed) != 0 {
		t.Errorf("探测失败不得改名: %v", fs.renamed)
	}

	svc = newMsixTestService(noProberPackages{}, &fakeMsixSource{})
	svc.orphan = (&fakeOrphanFS{dir: "C:\\anywhere"}).hooks()
	if err := svc.CleanMsixOrphan(); err == nil || !strings.Contains(err.Error(), "全用户") {
		t.Fatalf("能力缺席须拒动并报明: %v", err)
	}
}

// TestCleanMsixOrphanGate2RefusesAbsentDir 闸②：目录不在场=无事可做，
// 回中文实话且不触碰注册探测之后的任何动作。
func TestCleanMsixOrphanGate2RefusesAbsentDir(t *testing.T) {
	packages := &fakeMsixPackages{allUsersRegistered: boolPtr(false)}
	svc := newMsixTestService(packages, &fakeMsixSource{})
	// 缺省替身 exists 恒假
	err := svc.CleanMsixOrphan()
	if err == nil || !strings.Contains(err.Error(), "未检测到孤儿数据目录") {
		t.Fatalf("无目录须报「未检测到」: %v", err)
	}
	if packages.allUsersCalls != 1 {
		t.Errorf("闸①仍须先走（授权语义序）: %d", packages.allUsersCalls)
	}
}

// TestCleanMsixOrphanGate3RefusesBlindInventory 闸③：指纹清点失败=带盲区
// 动手，拒；改名不发生。
func TestCleanMsixOrphanGate3RefusesBlindInventory(t *testing.T) {
	fs := &fakeOrphanFS{dir: `C:\fake\AppData\Local\Packages\` + MsixPackageFamily, walkErr: errors.New("拒绝访问")}
	svc := newMsixTestService(&fakeMsixPackages{allUsersRegistered: boolPtr(false)}, &fakeMsixSource{})
	svc.orphan = fs.hooks()
	err := svc.CleanMsixOrphan()
	if err == nil || !strings.Contains(err.Error(), "指纹清点失败") {
		t.Fatalf("清点失败须拒动: %v", err)
	}
	if len(fs.renamed) != 0 {
		t.Errorf("清点失败不得改名: %v", fs.renamed)
	}
}

// TestCleanMsixOrphanSuccessChainQuarantinesNotDeletes 成功链（改名隔离非
// 删除）：Info 指纹先落（含 ExplorerTAP.dll——explorer 持柄真凶样本）、
// Rename 走精确常量路径到 .orphan-YYYYMMDD、WARN 审计收口；话术不承诺删除。
func TestCleanMsixOrphanSuccessChainQuarantinesNotDeletes(t *testing.T) {
	dir := `C:\Users\Administrator\AppData\Local\Packages\28017CharlesMilette.TranslucentTB_v826wp6bftszj`
	fs := &fakeOrphanFS{dir: dir, inv: &orphanInventory{
		files: 3, bytes: 4718592,
		entries: []string{`AC\Settings.dat`, `TempState\ExplorerTAP.dll`, `LocalState\settings.json`},
	}}
	buf, restore := captureMsixLogs(t, slog.LevelInfo)
	defer restore()

	svc := newMsixTestService(&fakeMsixPackages{allUsersRegistered: boolPtr(false)}, &fakeMsixSource{})
	svc.orphan = fs.hooks()
	if err := svc.CleanMsixOrphan(); err != nil {
		t.Fatal(err)
	}
	if len(fs.renamed) != 1 || fs.renamed[0][0] != dir || fs.renamed[0][1] != dir+".orphan-20260927" {
		t.Fatalf("改名隔离落点偏离实证形制（可逆、零真删）: %v", fs.renamed)
	}
	logged := buf.String()
	infoAt := strings.Index(logged, "隔离前指纹留档")
	warnAt := strings.Index(logged, "已改名隔离")
	if infoAt < 0 || warnAt < 0 || infoAt > warnAt {
		t.Fatalf("指纹 Info 须先于审计 WARN: %s", logged)
	}
	// entries 样本含 explorer 持柄真凶 ExplorerTAP.dll（TextHandler 引号转义
	// 反斜杠，按文件名 token 断言不锁分隔符形制）。
	for _, want := range []string{"path=" + dir, "files=3", "bytes=4718592", "ExplorerTAP.dll", "TempState", "to=" + dir + ".orphan-20260927"} {
		if !strings.Contains(logged, want) {
			t.Errorf("留痕缺 %q: %s", want, logged)
		}
	}
}

// TestCleanMsixOrphanTargetCollisionAndRenameFailure 撞名退避（同日二次
// 隔离退 HHMMSS 戳、两级都占用拒动，绝不覆盖既有备份）与 Rename 失败
// （explorer 持柄等）如实报因。
func TestCleanMsixOrphanTargetCollisionAndRenameFailure(t *testing.T) {
	dir := `C:\fake\AppData\Local\Packages\` + MsixPackageFamily
	fs := &fakeOrphanFS{dir: dir, extra: map[string]bool{dir + ".orphan-20260927": true}}
	svc := newMsixTestService(&fakeMsixPackages{allUsersRegistered: boolPtr(false)}, &fakeMsixSource{})
	svc.orphan = fs.hooks()
	if err := svc.CleanMsixOrphan(); err != nil {
		t.Fatal(err)
	}
	if len(fs.renamed) != 1 || fs.renamed[0][1] != dir+".orphan-20260927-150405" {
		t.Fatalf("同日占用未退到秒级戳: %v", fs.renamed)
	}

	fs = &fakeOrphanFS{dir: dir, extra: map[string]bool{
		dir + ".orphan-20260927": true, dir + ".orphan-20260927-150405": true,
	}}
	svc.orphan = fs.hooks()
	err := svc.CleanMsixOrphan()
	if err == nil || !strings.Contains(err.Error(), "撞名") {
		t.Fatalf("两级占用须拒撞名: %v", err)
	}
	if len(fs.renamed) != 0 {
		t.Errorf("拒撞名不得改名: %v", fs.renamed)
	}

	fs = &fakeOrphanFS{dir: dir, renameErr: errors.New("另一个程序正在使用此文件")}
	svc.orphan = fs.hooks()
	err = svc.CleanMsixOrphan()
	if err == nil || !strings.Contains(err.Error(), "改名隔离失败") || !strings.Contains(err.Error(), "持柄") {
		t.Fatalf("Rename 失败须如实报因: %v", err)
	}
}

// ---------- InstallMsix 进度事件通道（2026-09-27 机主撞账补线） ----------

// captureMsixProgress 换接 msixProgressEmit 缝捕获事件序列；返回快照与还原。
func captureMsixProgress(t *testing.T) (*[]MsixProgress, func()) {
	t.Helper()
	got := &[]MsixProgress{}
	restore := msixProgressEmit
	msixProgressEmit = func(p MsixProgress) { *got = append(*got, p) }
	return got, func() { msixProgressEmit = restore }
}

func progressStages(got []MsixProgress) []string {
	stages := make([]string, 0, len(got))
	for _, p := range got {
		stages = append(stages, p.Stage)
	}
	return stages
}

func TestInstallMsixProgressSuccessSequence(t *testing.T) {
	got, restore := captureMsixProgress(t)
	defer restore()
	packages := &fakeMsixPackages{queryScript: []fakeMsixQuery{{pkg: &apppackage.Package{Version: "2026.2.0.0"}}}}
	msix := &fakeMsixSource{hasRelease: true, preparePath: `C:\cache\bundle.msixbundle`, progressScript: []fakeMsixProgressEvent{
		{0, "downloading"}, {42, "downloading"}, {100, "verify-sha256"}, {100, "done"},
	}}
	svc := newMsixTestService(packages, msix)
	if err := svc.InstallMsix("2026.2"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"preparing", "downloading", "downloading", "verify-sha256", "deploying", "done"}; !equalStrings(progressStages(*got), want) {
		t.Errorf("事件序列偏离词表: %v want %v", progressStages(*got), want)
	}
	if (*got)[1].Percent != 0 || (*got)[2].Percent != 42 {
		t.Errorf("下载百分比未透传: %+v %+v", (*got)[1], (*got)[2])
	}
	// deploying 是同步部署段的唯一不确定态；末笔 done 恰一条（版本线中间 done 被吞）。
	if (*got)[4].Message == "" || (*got)[5].Stage != "done" || (*got)[5].Percent != 100 {
		t.Errorf("deploying/done 档形制失真: %+v %+v", (*got)[4], (*got)[5])
	}
}

func TestInstallMsixProgressErrorCarriesAttribution(t *testing.T) {
	got, restore := captureMsixProgress(t)
	defer restore()
	packages := &fakeMsixPackages{installErr: &apppackage.Error{
		Code: apppackage.CodeDeployment, Message: "Windows 包操作失败",
		Detail: "删除程序包先前已有的应用程序数据时出错 0x800703FA；内部错误 0x80073D05", HResult: "0x80073CF6",
	}}
	svc := newMsixTestService(packages, &fakeMsixSource{hasRelease: true, preparePath: `C:\cache\bundle.msixbundle`})
	if err := svc.InstallMsix("2026.2"); err == nil {
		t.Fatal("应失败")
	}
	stages := progressStages(*got)
	if stages[len(stages)-1] != "error" {
		t.Fatalf("失败必须以 error 档收口: %v", stages)
	}
	last := (*got)[len(*got)-1]
	if !strings.Contains(last.Message, msixOrphanMarker) || !strings.Contains(last.Message, "🧹 清理后重试") {
		t.Errorf("error 档须自带归因后完整话术（前端孤儿钮同源判据）: %s", last.Message)
	}
}

func TestInstallMsixNoReleaseDoesNotTouchProgressChannel(t *testing.T) {
	got, restore := captureMsixProgress(t)
	defer restore()
	svc := newMsixTestService(&fakeMsixPackages{}, &fakeMsixSource{hasRelease: false})
	if err := svc.InstallMsix("2024.4"); err == nil {
		t.Fatal("应报无打包形态")
	}
	if len(*got) != 0 {
		t.Errorf("无资产预检不进度化（瞬时回执走 toast 即可）: %v", progressStages(*got))
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
