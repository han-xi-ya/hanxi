package wsl

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/wsl/readiness"
	"hanxi/internal/modules/wsl/releases"
)

// EventReadiness 流式体检事件名（app.go 中注册载荷类型）。
const EventReadiness = "wsl:readiness"

// urlOpener 打开固定官方地址所需的最小平台能力（与 envcheck 同款解耦）。
type urlOpener interface {
	OpenURL(url string) error
}

// msiGuidRe MSI ProductCode 键名形态：{8-4-4-4-12}。
var msiGuidRe = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}\}$`)

// 卸载与残留巡查的固定脚本（只读查询 + 正规 Remove-AppxPackage；绝不写删注册表）。
const (
	msiCodeScript = `$u = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*','HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*' -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -match 'Windows Subsystem for Linux' } | Select-Object -First 1
if ($u) { [string]$u.PSChildName }`
	uninstallMSIXScript = `$ProgressPreference = 'SilentlyContinue'
$p = Get-AppxPackage *WindowsSubsystemForLinux* -ErrorAction SilentlyContinue
if ($p) { try { $p | Remove-AppxPackage -ErrorAction Stop; $r = 'removed' } catch { $r = 'failed' } } else { $r = 'none' }
$n = @(Get-ChildItem 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Lxss' -ErrorAction SilentlyContinue).Count
"$r|$n"`
)

// WslService Wails 绑定服务：WSL 就绪体检（同步 + 流式）、官方版本管理、
// 白名单提权操作与正规卸载。探测/外呼函数一律以字段注入，单测替换后即可离线断言。
type WslService struct {
	opener        urlOpener
	probe         func(context.Context) (readiness.ProbeResult, error)
	netProbe      func(context.Context) (api string, gh string)
	wslVersion    func(context.Context) string
	wslDistros    func(context.Context) []readiness.Distro
	onlineDistros func(context.Context) ([]readiness.DistroOption, error)
	overview      func(localVersion string) (releases.Overview, error)
	elevProc      func(context.Context, string, ...string) (OperationOutcome, error)
	localPS       func(context.Context, string) (string, error)
	emit          func(name string, payload any)

	// lastProbe 缓存最近一次成功探针：卸载取 MSI ProductCode 免重复查询。
	mu        sync.Mutex
	lastProbe *readiness.ProbeResult
	// readinessPublish 串行化轮次切换与事件发布，消除“旧轮已检查、尚未 Emit”窗口。
	readinessPublish sync.Mutex
	// readinessCancel 终止上一次仍在跑的流式体检（重新体检防串扰）。
	readinessCancel context.CancelFunc
	readinessGen    uint64
	// 下载单飞状态与本会话落盘登记（RevealDownload 只认这里的路径）。
	dlBusy  bool
	dlPaths map[string]string
}

func NewWslService(opener urlOpener) *WslService {
	return &WslService{
		opener:        opener,
		probe:         readiness.Probe,
		netProbe:      readiness.ProbeNetwork,
		wslVersion:    readiness.Version,
		wslDistros:    readiness.Distros,
		onlineDistros: readiness.OnlineDistros,
		overview:      releases.OverviewFor,
		elevProc:      runElevatedProcess,
		localPS:       runLocalPS,
		emit:          emitEvent,
		dlPaths:       map[string]string{},
	}
}

// emitEvent 经 Wails 事件总线推送；无 app 实例（单测/装配前）静默跳过。
func emitEvent(name string, payload any) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit(name, payload)
	}
}

// ---- 体检：同步整体 + 流式分相 ----

// GetReadiness 并发采集探针/网络通道/WSL CLI 现状，返回完整体检报告。
// 供操作后的同步刷新；首屏渲染走 StartReadiness 流式通道。
func (s *WslService) GetReadiness() (readiness.Report, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var (
		p        readiness.ProbeResult
		probeErr error
		api, gh  string
		version  string
		distros  []readiness.Distro
		wg       sync.WaitGroup
	)
	wg.Go(func() { p, probeErr = s.probe(ctx) })
	wg.Go(func() { api, gh = s.netProbe(ctx) })
	wg.Go(func() {
		version = s.wslVersion(ctx)
		distros = s.wslDistros(ctx)
	})
	wg.Wait()
	if probeErr != nil {
		return readiness.Report{}, fmt.Errorf("WSL 就绪探针执行失败（PowerShell 不可用或被安全策略拦截？）: %w", probeErr)
	}
	p.APIGitHub, p.GitHub = api, gh
	s.setLastProbe(p)
	return readiness.Evaluate(p, version, distros, time.Now().Format("2006-01-02 15:04:05")), nil
}

// StartReadiness 异步启动流式体检并立即返回：
// 三源并发、先到先推（system / wsl / net 阶段逐项落位），全齐后 done 阶段整体收口。
// 重复调用会静默终止上一轮，永不双跑串扰。
func (s *WslService) StartReadiness() error {
	s.readinessPublish.Lock()
	defer s.readinessPublish.Unlock()
	s.mu.Lock()
	if s.readinessCancel != nil {
		s.readinessCancel()
	}
	s.readinessGen++
	gen := s.readinessGen
	ctx, cancel := context.WithCancel(context.Background())
	s.readinessCancel = cancel
	s.mu.Unlock()

	go func() {
		defer cancel()
		s.streamReadiness(ctx, gen)
	}()
	return nil
}

func (s *WslService) streamReadiness(ctx context.Context, gen uint64) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	var (
		p        readiness.ProbeResult
		probeErr error
		api, gh  string
		version  string
		distros  []readiness.Distro
		wg       sync.WaitGroup
	)
	wg.Go(func() {
		p, probeErr = s.probe(ctx)
		if probeErr == nil && s.isCurrentReadiness(gen, ctx) {
			s.setLastProbeIfCurrent(gen, p)
			s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "system", Items: readiness.SystemItems(p)})
		}
	})
	wg.Go(func() {
		version = s.wslVersion(ctx)
		s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "wsl", Items: []readiness.CheckItem{readiness.RuntimeItem(version)}})
		distros = s.wslDistros(ctx)
	})
	wg.Go(func() {
		api, gh = s.netProbe(ctx)
		s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "net", Items: []readiness.CheckItem{readiness.NetworkItem(api, gh)}})
	})
	wg.Wait()

	if !s.isCurrentReadiness(gen, ctx) {
		return
	}
	if probeErr != nil {
		s.emitIfCurrent(gen, ctx, ReadinessUpdate{
			Stage: "error",
			Error: fmt.Sprintf("WSL 就绪探针执行失败（PowerShell 不可用或被安全策略拦截？）: %v", probeErr),
		})
		return
	}
	p.APIGitHub, p.GitHub = api, gh
	report := readiness.Evaluate(p, version, distros, time.Now().Format("2006-01-02 15:04:05"))
	s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "done", Report: &report})
}

func (s *WslService) isCurrentReadiness(gen uint64, ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readinessGen == gen
}

func (s *WslService) emitIfCurrent(gen uint64, ctx context.Context, update ReadinessUpdate) {
	s.readinessPublish.Lock()
	defer s.readinessPublish.Unlock()
	if !s.isCurrentReadiness(gen, ctx) {
		return
	}
	s.emit(EventReadiness, update)
}

func (s *WslService) setLastProbeIfCurrent(gen uint64, p readiness.ProbeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readinessGen == gen {
		s.lastProbe = &p
	}
}

func (s *WslService) setLastProbe(p readiness.ProbeResult) {
	s.mu.Lock()
	s.lastProbe = &p
	s.mu.Unlock()
}

// ListOnlineDistros 返回官方在线可安装发行版清单（需 wsl.exe 已具备 --list --online 能力）。
func (s *WslService) ListOnlineDistros() ([]readiness.DistroOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.onlineDistros(ctx)
}

// GetReleases 拉取 microsoft/WSL 官方发布列表并判定与本机版本的关系。
// API 被拦（如 api.github.com 对部分云出口 IP 区域封锁 403）时自动降级
// Atom 订阅源（github.com 域通常畅通），载荷 fallback 标记供前端如实标注。
func (s *WslService) GetReleases() (releases.Overview, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	local := ""
	if v := s.wslVersion(ctx); v != "" {
		local = v
	}
	return s.overview(local)
}

// ---- 白名单提权操作：参数全部后端固定，前端零拼接面 ----

// InstallWsl 一键开启：只装 WSL 本体与虚拟机平台，绝不自动装发行版
// （--install 捆绑模式会把默认 Ubuntu 一起塞进来，与"用户手动挑系统"的
// 产品语义冲突，故本模块操作面固定走 --no-distribution）。
func (s *WslService) InstallWsl() (OperationOutcome, error) {
	return s.elevateWsl("--install", "--no-distribution")
}

// UpdateWsl 经默认通道更新 WSL 本体。
func (s *WslService) UpdateWsl() (OperationOutcome, error) {
	return s.elevateWsl("--update")
}

// UpdateWslWebDownload 强制 GitHub 直连更新（商店通道不通时）。
func (s *WslService) UpdateWslWebDownload() (OperationOutcome, error) {
	return s.elevateWsl("--update", "--web-download")
}

// SetDefaultVersion2 将新装发行版默认设为 WSL2。
func (s *WslService) SetDefaultVersion2() (OperationOutcome, error) {
	return s.elevateWsl("--set-default-version", "2")
}

func (s *WslService) elevateWsl(args ...string) (OperationOutcome, error) {
	return s.elevProc(context.Background(), "wsl.exe", args...)
}

// InstallDistro 安装指定在线发行版。ID 不接受裸字符串直传命令——
// 先实时重取在线清单做白名单校验，杜绝本模块被当作任意参数执行面。
// 白名单之后、提权之前还有一道虚拟化装载预检：默认版本为 2 的前提下，
// 虚拟机平台未启用或"已启用但欠重启"时 WSL2 根本起不了虚拟机（实机
// wsl --status 证词："WSL2 无法启动，因为此计算机上未启用虚拟化"），
// 此时点击安装只是白白弹出 UAC 再失败——提前拦下并指路，探针自身不可得时放行（失败由退出码通道如实上报）。
func (s *WslService) InstallDistro(id string) (OperationOutcome, error) {
	id = strings.TrimSpace(id)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if gate := s.virtualizationGate(ctx); gate != "" {
		return OperationOutcome{Message: gate}, nil
	}
	list, err := s.onlineDistros(ctx)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("无法校验发行版清单，安装中止: %w", err)
	}
	allowed := false
	for _, o := range list {
		if o.ID == id {
			allowed = true
			break
		}
	}
	if !allowed {
		return OperationOutcome{}, fmt.Errorf("发行版 %q 不在官方在线清单中，已拒绝执行", id)
	}
	return s.elevateWsl("--install", "-d", id)
}

// virtualizationGate 发行版安装的硬前提预检；返回空串表示放行。
func (s *WslService) virtualizationGate(ctx context.Context) string {
	p, err := s.probe(ctx)
	if err != nil {
		return ""
	}
	switch {
	case !p.FeatureVM:
		return "「虚拟机平台」尚未启用——WSL2 无法承载发行版。请先执行「🚀 一键开启」（或「▶️ 开启虚拟机平台」）并重启，再回来挑选发行版。"
	case p.RebootPending:
		return "「虚拟机平台」已启用但还没重启生效——WSL2 此刻起不了虚拟机（wsl --status 原话：未启用虚拟化），现在装发行版注定失败。重启一次、回来重新体检即可安装。"
	}
	return ""
}

// ---- 正规卸载与组件还原 ----

// UninstallWsl 双形态正规卸载：MSI 系统版走 msiexec /X ProductCode（提权、官方卸载器
// 自收编其注册表登记），MSIX 用户包走 Remove-AppxPackage（当前用户、无需提权）。
// 边界如实申明：不强删注册表、不触碰可选功能（另见 DisableWslFeatures）；
// Lxss 历史发行版注册键属正规卸载器不管辖的残留，巡查后如实报告而非暗中删除。
func (s *WslService) UninstallWsl() (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var steps []string

	// 1) MSI 系统版：优先复用探针缓存的 ProductCode，缺失时只读补查一次。
	if code := s.msiProductCode(ctx); code != "" {
		out, err := s.elevProc(ctx, "MsiExec.exe", "/X"+code, "/qb")
		if err != nil {
			return OperationOutcome{}, fmt.Errorf("MSI 系统版卸载失败: %w", err)
		}
		if !out.Success {
			return out, nil // UAC 取消：整体中止，如实回执
		}
		steps = append(steps, "MSI 系统版已通过官方卸载器移除")
	} else {
		steps = append(steps, "未检出 MSI 系统版（跳过）")
	}

	// 2) MSIX 用户包 + Lxss 残留巡查（同一脚本一次执行）。
	outStr, err := s.localPS(ctx, uninstallMSIXScript)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("MSIX 用户包清理执行失败: %w", err)
	}
	verdict, lxssCount := parseUninstallReport(outStr)
	msixOK := true
	switch verdict {
	case "removed":
		steps = append(steps, "MSIX 用户包已移除")
	case "none":
		steps = append(steps, "未检出 MSIX 用户包（跳过）")
		msixOK = true
	default:
		steps = append(steps, "MSIX 包移除失败（可在「设置→应用」重试）")
		msixOK = false
	}
	if lxssCount > 0 {
		steps = append(steps, fmt.Sprintf("检测到 %d 个历史发行版注册残留（HKCU\\…\\Lxss 键，正规卸载器不负责回收，无害；介意可手动删除）", lxssCount))
	} else {
		steps = append(steps, "Lxss 发行版注册无残留")
	}
	steps = append(steps, "可选功能（虚拟机平台/WSL）未触碰——如需彻底还原系统，可执行「关闭虚拟机平台组件」")

	return OperationOutcome{
		Success: msixOK,
		Message: strings.Join(steps, " · "),
	}, nil
}

// EnableWslFeatures 提权经 DISM 启用虚拟机平台与旧版 WSL 两个可选功能
// （与「关闭」对称的状态还原通道；「🚀 一键开启」会在装 WSL 时顺带完成同样动作）。
// 两条 dism 之间显式传播 $LASTEXITCODE：`;` 链的退出码只反映最后一条命令，
// 曾因"第一条失败、第二条成功"被整体误报为成功（实机 UAC 窗口一闪而过的漏报事故）。
func (s *WslService) EnableWslFeatures() (OperationOutcome, error) {
	inner := "dism /online /enable-feature /featurename:VirtualMachinePlatform /norestart; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; " +
		"dism /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /norestart; exit $LASTEXITCODE"
	out, err := s.elevProc(context.Background(), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inner)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("启用虚拟机平台组件失败: %w", err)
	}
	if out.Success {
		out.Message = "可选功能启用指令已执行完毕，重启后生效（重启前发行版无法拉起）"
	}
	return out, nil
}

// DisableWslFeatures 提权经 DISM 关闭两个可选功能（虚拟机平台 + WSL 旧功能开关），
// 这是"把 Windows 自身被 WSL 改变的开关还原回去"的正道；/norestart 交由用户择机重启。
func (s *WslService) DisableWslFeatures() (OperationOutcome, error) {
	inner := "dism /online /disable-feature /featurename:VirtualMachinePlatform /norestart; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; " +
		"dism /online /disable-feature /featurename:Microsoft-Windows-Subsystem-Linux /norestart; exit $LASTEXITCODE"
	out, err := s.elevProc(context.Background(), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inner)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("关闭虚拟机平台组件失败: %w", err)
	}
	if out.Success {
		out.Message = "可选功能关闭指令已执行完毕，重启后生效（点「🔁 稍后重启」直达系统电源设置）"
	}
	return out, nil
}

// msiProductCode 优先取探针缓存（体检顺带采集），缺失时只读补查注册表。
func (s *WslService) msiProductCode(ctx context.Context) string {
	s.mu.Lock()
	cached := ""
	if s.lastProbe != nil {
		cached = s.lastProbe.WslMSICode
	}
	s.mu.Unlock()
	if msiGuidRe.MatchString(cached) {
		return cached
	}
	out, err := s.localPS(ctx, msiCodeScript)
	code := strings.TrimSpace(out)
	if err != nil || !msiGuidRe.MatchString(code) {
		return ""
	}
	return code
}

// parseUninstallReport 解析 "removed|3" 形态脚本输出；畸形输出按 failed/-1 保守处理。
func parseUninstallReport(s string) (verdict string, lxssCount int) {
	verdict, countPart, _ := strings.Cut(strings.TrimSpace(s), "|")
	if _, err := fmt.Sscanf(strings.TrimSpace(countPart), "%d", &lxssCount); err != nil {
		lxssCount = -1
	}
	switch verdict {
	case "removed", "none", "failed":
		return verdict, lxssCount
	default:
		return "failed", lxssCount
	}
}

// ---- 固定官方地址导航 ----

var wslTagRe = regexp.MustCompile(`^\d+\.\d+(\.\d+){1,2}$`)

// wslAssetNameRe 形如 wsl.2.9.10.0.x64.msi / wsl.2.7.13.0.arm64.msi。
var wslAssetNameRe = regexp.MustCompile(`^wsl\.\d+\.\d+\.\d+\.\d+\.(x64|arm64)\.msi$`)

// OpenReleasesPage 打开 microsoft/WSL 官方发布页。
func (s *WslService) OpenReleasesPage() error {
	return s.openURL("WSL 官方发布页", releases.ReleasesPageURL())
}

// OpenReleaseTag 打开指定版本 tag 的发行说明页（tag 必须为纯数字点分版本）。
func (s *WslService) OpenReleaseTag(tag string) error {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	if !wslTagRe.MatchString(tag) {
		return fmt.Errorf("版本号 %q 格式不合法", tag)
	}
	return s.openURL("版本 "+tag, "https://github.com/microsoft/WSL/releases/tag/"+tag)
}

// assetDownloadURL 拼装并校验官方 MSI 直链（tag 与文件名双重白名单）。
func assetDownloadURL(tag, name string) (string, error) {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	name = strings.TrimSpace(name)
	if !wslTagRe.MatchString(tag) || !wslAssetNameRe.MatchString(name) {
		return "", fmt.Errorf("下载参数不合法: %s / %s", tag, name)
	}
	return "https://github.com/microsoft/WSL/releases/download/" + tag + "/" + name, nil
}

// OpenOfficialDocs 打开微软官方 WSL 安装文档。
func (s *WslService) OpenOfficialDocs() error {
	return s.openURL("WSL 官方文档", "https://learn.microsoft.com/windows/wsl/install")
}

// OpenPowerSettings 打开系统「电源」设置页：功能启用/关闭后引导用户自行重启
// （不代点重启——重启是打断用户工作的高危动作）。
func (s *WslService) OpenPowerSettings() error {
	return s.openURL("系统电源设置", "ms-settings:power")
}

func (s *WslService) openURL(name, rawURL string) error {
	if s.opener == nil {
		return fmt.Errorf("打开 %s 失败: 平台能力不可用", name)
	}
	if err := s.opener.OpenURL(rawURL); err != nil {
		return fmt.Errorf("打开 %s 失败: %w", name, err)
	}
	return nil
}
