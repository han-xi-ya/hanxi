package eartrumpet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"
	"hanxi/internal/platform/versioncmp"
)

// URLOpener 抽象"用系统默认方式打开 URL/协议"的能力（平台层为
// Platform.OpenURL，支持 https 等已注册协议）。
type URLOpener interface {
	OpenURL(url string) error
}

const (
	listTimeout    = 15 * time.Second
	downloadBudget = 5 * time.Minute
	installBudget  = 3 * time.Minute
)

// EarTrumpetService 是暴露给前端的 Wails Service。
//
// 职责边界：只管官方直装渠道（install.eartrumpet.app）——状态检测（含
// 运行态与商店版并存标记）、启动、退出、卸载、安装/更新。商店渠道不纳管
// （用户拍板"商店版不要了"），检测到并存时由 UI 黄条警告。EarTrumpet 是
// 带开机自启的常驻托盘应用：不绑定 JobObject，退出为按 PID 直接终止
// （上游无优雅退出通道，见 Exit 注释），关闭 Hanxi 不影响 EarTrumpet。
type EarTrumpetService struct {
	packages  apppackage.API
	openURL   URLOpener
	fetch     func(ctx context.Context, url string) ([]byte, error)
	download  func(ctx context.Context, url, dst string) error
	procs     platform.ProcessAPI
	findProcs func(installLocation string) []platform.ProcInfo
	remote    remoteCache
}

// NewEarTrumpetService 从平台聚合接口取包管理、URL 打开与进程能力。
func NewEarTrumpetService(plat platform.Platform) *EarTrumpetService {
	s := newEarTrumpetService(plat.AppPackage(), plat, httpGet, httpSave)
	s.procs = plat.Process()
	s.findProcs = func(installLocation string) []platform.ProcInfo {
		return findProcessesUnder(installLocation, s.procs)
	}
	return s
}

func newEarTrumpetService(packages apppackage.API, opener URLOpener, fetch func(context.Context, string) ([]byte, error), download func(context.Context, string, string) error) *EarTrumpetService {
	return &EarTrumpetService{packages: packages, openURL: opener, fetch: fetch, download: download}
}

// GetStatus 查询直装渠道注册状态，并顺带检测商店版并存。
//
// 每个 Query 都要冷启动一次 PowerShell 子进程（实测约 1.8s/次），两个
// 查询并发执行把耗时压回单次延迟。
func (s *EarTrumpetService) GetStatus() (PackageSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var managed, store *apppackage.Package
	var managedErr, storeErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); managed, managedErr = s.packages.Query(ctx, managedIdentity) }()
	go func() { defer wg.Done(); store, storeErr = s.packages.Query(ctx, storeIdentity) }()
	wg.Wait()
	if managedErr != nil {
		return PackageSnapshot{}, managedErr
	}
	if storeErr != nil {
		return PackageSnapshot{}, storeErr
	}

	snap := PackageSnapshot{ObservedAt: time.Now().Format(time.RFC3339)}
	if managed != nil {
		snap.Installed = true
		snap.Running = s.isRunning(managed)
		snap.Version = managed.Version
		snap.PackageFullName = managed.PackageFullName
		snap.Architecture = managed.Architecture
		snap.InstallLocation = managed.InstallLocation
		snap.PackageStatus = managed.Status
	}
	if store != nil {
		snap.StoreCoexist = true
		snap.StoreVersion = store.Version
	}
	return snap, nil
}

// isRunning 按直装包安装目录探测进程；探测能力未接线时保守报告未运行。
func (s *EarTrumpetService) isRunning(pkg *apppackage.Package) bool {
	if pkg == nil || s.findProcs == nil {
		return false
	}
	return len(s.findProcs(pkg.InstallLocation)) > 0
}

// GetRemoteVersion 返回官方直装渠道当前最新版本号（10 分钟缓存，
// 网络失败回退上次核验成功的清单）。
func (s *EarTrumpetService) GetRemoteVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	rel, err := s.remote.fetch(ctx, s.fetch)
	if err != nil {
		return "", err
	}
	return rel.Version, nil
}

// Launch 激活已安装的直装版。
//
// 不做前置 Query：脚本 activate 分支在同一 PowerShell 进程内已查包，未安装
// 会返回 CodeNotInstalled——省掉一次 ~1.8s 的子进程冷启动。
//
// 注意上游单实例语义：若已有实例在运行（含并存时的商店版实例），第二个
// 实例会因单实例互斥静默退出且不会唤起 UI——上游没有唤窗通道。
func (s *EarTrumpetService) Launch() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := s.packages.Activate(ctx, managedIdentity)
	var pkgErr *apppackage.Error
	if errors.As(err, &pkgErr) && pkgErr.Code == apppackage.CodeNotInstalled {
		return fmt.Errorf("EarTrumpet 直装版尚未安装")
	}
	return err
}

// Exit 终止正在运行的直装版进程，返回终止的进程数。
//
// 上游没有任何优雅退出通道（无 CLI/IPC/唤窗协议，WM_CLOSE 只会关掉其隐藏的
// flyout 窗口），因此退出 = 按 PID 直接终止（经 KillVerified 指纹复核防 PID
// 复用，且只杀安装目录匹配直装渠道的进程）。这是安全的：设置保存在事务性
// LocalSettings 容器，音量状态由系统音频栈持有。只影响当前会话——它注册了
// 登录自启，下次登录仍会出现；真要常驻移除请卸载，Hanxi 不改它的自启注册。
func (s *EarTrumpetService) Exit() (int, error) {
	if s.procs == nil || s.findProcs == nil {
		return 0, fmt.Errorf("进程探测能力不可用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pkg, err := s.packages.Query(ctx, managedIdentity)
	if err != nil {
		return 0, err
	}
	if pkg == nil {
		return 0, fmt.Errorf("EarTrumpet 直装版尚未安装")
	}
	procs := s.findProcs(pkg.InstallLocation)
	if len(procs) == 0 {
		return 0, fmt.Errorf("当前没有运行中的进程")
	}
	killed, firstErr := 0, error(nil)
	for _, p := range procs {
		if err := s.procs.KillVerified(ctx, platform.VerifyToken{
			PID: p.PID, ExePath: p.ExePath, StartedAt: p.StartedAt,
		}, false); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		killed++
	}
	if killed == 0 {
		return 0, firstErr
	}
	return killed, nil
}

// Uninstall 卸载直装版的当前用户包。
//
// 风险提示：设置（热键、音量覆盖、Actions 规则等）保存在包的 LocalSettings
// 容器内，随包卸载一并删除。
func (s *EarTrumpetService) Uninstall() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pkg, err := s.packages.Query(ctx, managedIdentity)
	if err != nil {
		return err
	}
	if pkg == nil {
		return fmt.Errorf("EarTrumpet 直装版尚未安装，无需卸载")
	}
	return s.packages.Uninstall(ctx, managedIdentity, pkg.PackageFullName)
}

// Install 安装/更新官方直装渠道的最新版本。
//
// 完整性链条：appinstaller 清单钉死包名+发布者+官方主机（TLS）→ 下载
// appxbundle → 若 winget-pkgs 官方仓库收录同版本则交叉比对 SHA-256 →
// Add-AppxPackage 时 Windows 校验 ACS 包签名与发布者身份。
//
// 签名链条（2026-09-03 实测）：上游经 Azure Code Signing 用短时效证书
// （约 3 天）签名并附 RFC3161 时间戳，Authenticode 视角 Valid、本地 X509
// 链按当前时钟构建报 NotTimeValid，但实机已成功部署 2.3.0.20——Windows
// 部署栈接受"过期证书+有效期时间戳"组合。若其他环境仍失败会映射为
// SignatureInvalid（含 0x800B0101 CERT_E_EXPIRED）。
//
// 与商店版并存会争抢单实例互斥且配置分裂，检测到 Store 版时直接拒绝。
func (s *EarTrumpetService) Install() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadBudget+installBudget)
	defer cancel()

	rel, err := s.remote.fetch(ctx, s.fetch)
	if err != nil {
		return "", err
	}
	store, err := s.packages.Query(ctx, storeIdentity)
	if err != nil {
		return "", err
	}
	if store != nil {
		return "", fmt.Errorf("检测到已安装商店版 EarTrumpet（版本 %s）。两渠道并存会争抢单实例互斥、配置互相独立，请先经 Windows 设置/商店卸载商店版", store.Version)
	}
	managed, err := s.packages.Query(ctx, managedIdentity)
	if err != nil {
		return "", err
	}
	if managed != nil && versioncmp.Compare(rel.Version, managed.Version) <= 0 {
		return managed.Version, nil // 已是最新，幂等返回
	}

	dir, err := os.MkdirTemp("", "hanxi-eartrumpet-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	bundle := dir + string(os.PathSeparator) + "EarTrumpet.Package.appxbundle"
	if err := s.download(ctx, rel.BundleURL, bundle); err != nil {
		return "", fmt.Errorf("下载官方安装包失败: %w", err)
	}
	if err := s.crossCheckSHA(ctx, rel.Version, bundle); err != nil {
		return "", err
	}

	pkg, err := s.packages.Install(ctx, apppackage.InstallOptions{
		PackagePath:     bundle,
		Expected:        managedIdentity,
		ExpectedVersion: rel.Version,
		Dependencies:    rel.Dependencies,
	})
	if err != nil {
		return "", err
	}
	return pkg.Version, nil
}

// crossCheckSHA 尽力从 winget-pkgs 官方清单交叉比对 SHA-256。
// 版本超前于 winget 仓库（清单 404）时跳过——appinstaller 钉死校验与
// Windows 安装期的包签名校验仍然生效。
func (s *EarTrumpetService) crossCheckSHA(ctx context.Context, version, bundlePath string) error {
	manifestURL := "https://raw.githubusercontent.com/microsoft/winget-pkgs/master/manifests/f/File-New-Project/EarTrumpet/" + version + "/File-New-Project.EarTrumpet.installer.yaml"
	body, err := s.fetch(ctx, manifestURL)
	if err != nil || len(body) == 0 {
		return nil // 未收录，跳过交叉核验
	}
	expected := ""
	for _, line := range strings.Split(string(body), "\n") {
		if idx := strings.Index(line, "InstallerSha256:"); idx >= 0 {
			expected = strings.ToLower(strings.TrimSpace(line[idx+len("InstallerSha256:"):]))
			break
		}
	}
	if expected == "" {
		return nil
	}
	actual, err := fileSHA256(bundlePath)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("安装包 SHA-256 与 winget 官方清单不符: %s != %s", actual, expected)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// OpenRepo 打开 EarTrumpet 的 GitHub 项目主页。
func (s *EarTrumpetService) OpenRepo() error {
	return s.openURL.OpenURL(repoURL)
}
