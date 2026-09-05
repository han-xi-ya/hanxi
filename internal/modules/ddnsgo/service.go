package ddnsgo

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hanxi/internal/modules/ddnsgo/instance"
	"hanxi/internal/modules/ddnsgo/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	watchInterval = 5 * time.Second // 外部实例感知轮询间隔

	consoleWindowName = "ddnsgo-console" // 固定窗口名（Wails 窗口管理器内唯一键）
)

// DdnsGoService 向前端暴露 ddns-go 版本管理、托管启停与内嵌 Web 控制台能力。
// DNS 解析配置操作在子 Webview 窗口内的上游原生页面完成（决策记录见包注释），
// 本服务只做托管：拉起/退出/状态/日志/端口设置，不重复实现上游功能面。
type DdnsGoService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *ddnsgoStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	consoleMu  sync.Mutex // 子 Webview 窗口生命周期（创建于 RPC 线程，主循环内执行）
	consoleWin *application.WebviewWindow
	consoleURL string
}

func NewDdnsGoService(plat platform.Platform) *DdnsGoService {
	paths := settings.GetPaths()
	svc := &DdnsGoService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newDdnsgoStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
		OnLog:   svc.emitLog,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 ddnsgo:instance-state。
func (s *DdnsGoService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("ddnsgo instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("ddnsgo:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("ddnsgo", "ddns-go 实例异常", snap.Error, "/ext/ddnsgo")
	}
}

// emitLog 进程输出行 → 事件 ddnsgo:instance-log（DDNS 更新周期即 5 分钟级，
// 行频极低无需节流）。
func (s *DdnsGoService) emitLog(entry instance.LogEntry) {
	slog.Debug("ddnsgo instance log", "line", entry.Line)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("ddnsgo:instance-log", entry)
	}
}

// activate 启动后台外部实例感知：5s 轮询进程名扫描校正 external/stopped。
// ddns-go 是长驻后台服务形态，不设空闲自动退出。
func (s *DdnsGoService) activate() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watching {
		return
	}
	s.watching = true
	stop := make(chan struct{})
	s.watchStop = stop
	go func() {
		t := time.NewTicker(watchInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.engine.RefreshExternal()
			}
		}
	}()
}

// ---------- 版本管理（委托 manager） ----------

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存）。
func (s *DdnsGoService) ListReleases() ([]version.DdnsRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *DdnsGoService) ListInstalledVersions() ([]version.DdnsVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 ddnsgo:version-download 推送进度。
func (s *DdnsGoService) DownloadVersion(targetVersion string) (string, error) {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	// 已安装则直接返回，避免重复下载
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(strings.TrimPrefix(v.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
				return "already-installed", nil
			}
		}
	}

	go func() {
		emit := func(p version.DownloadProgress) {
			slog.Debug("ddnsgo download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("ddnsgo:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("ddnsgo", "版本下载成功", fmt.Sprintf("ddns-go %s 已成功安装", p.Version), "/ext/ddnsgo")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("ddnsgo", "版本下载失败", fmt.Sprintf("ddns-go %s 下载失败: %v", targetVersion, err), "/ext/ddnsgo")
			return
		}
		// 未设使用版本时自动把刚下载完的版本设为使用版本：
		// 首个版本下载完成后无需再手动点一下设置（与 snipaste 既有行为对齐）。
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(targetVersion)
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
func (s *DdnsGoService) RemoveVersion(targetVersion string) error {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(strings.TrimPrefix(snap.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	// 卸载的是当前设定版本则清空，下次冷启动自动回退最新已装
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("")
	}
	return nil
}

// SetActiveVersion 设定使用版本（先校验已安装，再持久化）。
func (s *DdnsGoService) SetActiveVersion(targetVersion string) (string, error) {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）。
func (s *DdnsGoService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的 ddns-go.exe（任意目录下载的官方原版）。
// 配置恒在 ~/.ddns_go_config.yaml 不受导入影响；仅迁移单 exe。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *DdnsGoService) ImportLocal(srcDir string) (version.DdnsVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.DdnsVersionInfo{}, fmt.Errorf("ddns-go 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// Start 托管启动自有 ddns-go 实例（不弹面板）：
// external 状态不越权接管、running 幂等直返、stopped/failed 冷启动。
func (s *DdnsGoService) Start() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "ddns-go 正在启动中，请稍候"}, nil
	case instance.StateExternal:
		return ControlOutcome{Action: "external", External: true,
			Message: "检测到外部 ddns-go 实例（自行启动或 Windows 服务），托管启动未执行"}, nil
	case instance.StateRunning:
		return ControlOutcome{Action: "already-running",
			Message: fmt.Sprintf("ddns-go %s 已在运行（%s）", snap.Version, snap.ListenAddr)}, nil
	default:
		addr, err := s.startOwned()
		if err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("ddns-go 已启动，Web 面板 %s", consoleURLOf(addr))}, nil
	}
}

// OpenConsole 打开（或复用）内嵌 Web 控制台子窗口：
//   - running：直接指向自有实例监听地址；
//   - external：按候选端口（设定端口 + 上游默认 9876）探测外部 web 服务；
//   - stopped/failed：冷启动后打开。
func (s *DdnsGoService) OpenConsole() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "ddns-go 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		for _, cand := range externalConsoleCandidates(s.store.GetListenPort()) {
			if s.engine.PortOpen(cand) {
				if err := s.ensureConsoleWindow(consoleURLOf(cand)); err != nil {
					return ControlOutcome{}, err
				}
				return ControlOutcome{Action: "external-opened", External: true,
					Message: fmt.Sprintf("已打开外部 ddns-go 面板（%s，非 Hanxi 托管）", cand)}, nil
			}
		}
		return ControlOutcome{}, fmt.Errorf("检测到外部 ddns-go 实例，但其 web 端口未开放（可能用了自定义 -l 参数，或运行于其他机器/服务账号下），无法定位面板")

	case instance.StateRunning:
		if snap.ListenAddr == "" {
			return ControlOutcome{}, fmt.Errorf("实例状态异常：缺少监听地址，请退出后重新启动")
		}
		if err := s.ensureConsoleWindow(consoleURLOf(snap.ListenAddr)); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "opened", Message: "已打开 ddns-go 控制台"}, nil

	default:
		addr, err := s.startOwned()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.ensureConsoleWindow(consoleURLOf(addr)); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "started",
			Message: "ddns-go 已启动并打开控制台（首次使用请在页面设置用户名/密码与 DNS 服务商）"}, nil
	}
}

// Quit 退出引擎托管的 ddns-go（经配置写静默期防护后终止）。
// external 状态不越权强杀（进程归属不在本引擎）：仅返回人性化指引。
func (s *DdnsGoService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 ddns-go 面板或其 Windows 服务中退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	s.hideConsole()
	return QuitOutcome{Stopped: true, Message: "ddns-go 已退出"}, nil
}

// startOwned 冷启动编排：解析版本 → 引擎托管拉起（内含端口预检与就绪等待）。
// 返回实际监听地址。首次冷启动把实际采用的版本回写为 activeVersion。
func (s *DdnsGoService) startOwned() (string, error) {
	v, exe, err := s.resolveActiveVersion()
	if err != nil {
		return "", err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", s.store.GetListenPort())
	if err := s.engine.Start(instance.StartOptions{
		Version: v, Exe: exe, ListenAddr: addr, Detached: !s.store.GetFollowOnExit(),
	}); err != nil {
		return "", fmt.Errorf("启动 ddns-go 失败: %w", err)
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(v)
	}
	return addr, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例（强杀通道，不等待
// 配置写静默期——OnShutdown 阻塞返回）。外部实例不受影响；自有实例另受
// JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *DdnsGoService) Shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop() // 联动开启才杀；关闭则完全不影响工具（Job 已解除 kill-on-close）
	}
}

// ---------- 内嵌 Web 控制台子窗口 ----------

// consoleURLOf 监听地址 → 面板 URL。
func consoleURLOf(listenAddr string) string {
	return "http://" + listenAddr + "/"
}

// externalConsoleCandidates 外部实例面板候选地址：设定端口优先，
// 上游默认端口 9876 兜底（外部自行启动/服务形态绝大多数用默认值）。
func externalConsoleCandidates(listenPort int) []string {
	addrs := make([]string, 0, 2)
	seen := make(map[int]bool, 2)
	for _, p := range []int{listenPort, defaultListenPort} {
		if p > 0 && !seen[p] {
			seen[p] = true
			addrs = append(addrs, fmt.Sprintf("127.0.0.1:%d", p))
		}
	}
	return addrs
}

// ensureConsoleWindow 创建或复用 ddns-go 控制台子 Webview 窗口并置前。
// 关闭按钮语义为隐藏复用（RegisterHook Cancel+Hide）：
//   - 避免误关子窗口在"最后窗口"平台分支上牵连应用退出策略；
//   - 隐藏复用保留 WebView2 会话 Cookie，下次打开免重复登录。
//
// 监听地址变化（改端口/重启用）经 SetURL 导航到新面板。
func (s *DdnsGoService) ensureConsoleWindow(url string) error {
	app := application.Get()
	if app == nil || app.Window == nil {
		return fmt.Errorf("控制台需要在应用内打开，请重试")
	}

	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()

	if s.consoleWin != nil {
		if s.consoleURL != url {
			s.consoleWin.SetURL(url)
			s.consoleURL = url
		}
		s.consoleWin.Show()
		s.consoleWin.Focus()
		return nil
	}

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             consoleWindowName,
		Title:            "ddns-go 控制台",
		Width:            1120,
		Height:           820,
		MinWidth:         760,
		MinHeight:        520,
		URL:              url,
		BackgroundColour: application.NewRGB(245, 246, 248),
	})
	// 关窗即隐藏：会话驻留 + 永不触发窗口销毁（外部 URL 页面无 Wails 运行时，
	// 与主窗口的托盘隐藏策略同构，语义对前端透明）。
	win.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
		ev.Cancel()
		win.Hide()
	})
	s.consoleWin = win
	s.consoleURL = url
	win.Show()
	win.Focus()
	return nil
}

// hideConsole 实例退出时收起面板（隐藏而非销毁，保留会话）。
func (s *DdnsGoService) hideConsole() {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if s.consoleWin != nil {
		s.consoleWin.Hide()
	}
}

// ---------- 状态与日志 ----------

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补轮询间隙的即时性）。
func (s *DdnsGoService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// Logs 返回实例最近 n 行进程输出（引擎重启会清空，前端另以事件流累积）。
func (s *DdnsGoService) Logs(n int) ([]string, error) {
	if n <= 0 || n > instance.LogCapacityHint {
		n = instance.LogCapacityHint
	}
	return s.engine.Logs(n), nil
}

// ---------- 端口与联动设置 ----------

// GetListenPort 返回 web 监听端口。
func (s *DdnsGoService) GetListenPort() (int, error) {
	return s.store.GetListenPort(), nil
}

// SetListenPort 设定端口（1024~65535，下次启动生效；运行中实例不变）。
func (s *DdnsGoService) SetListenPort(port int) (string, error) {
	if err := validateListenPort(port); err != nil {
		return "", err
	}
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateStarting {
		return "pending", s.store.SetListenPort(port)
	}
	if err := s.store.SetListenPort(port); err != nil {
		return "", err
	}
	return "applied", nil
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *DdnsGoService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *DdnsGoService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *DdnsGoService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *DdnsGoService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *DdnsGoService) OpenDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("目录路径不能为空")
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("目标不是目录: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// OpenConfigDir 在资源管理器中定位 ddns-go 的配置文件（%USERPROFILE%\.ddns_go_config.yaml）——
// 上游配置为单文件而非目录，直接打开整个用户主目录噪音过大，采用 /select 高亮定位。只读导航，不改写。
func (s *DdnsGoService) OpenConfigDir() error {
	file, err := userConfigFile()
	if err != nil {
		return err
	}
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("ddns-go 配置文件尚未创建（程序还未保存过配置）: %s", file)
	}
	return exec.Command("explorer.exe", "/select,"+file).Start()
}

// userConfigFile 上游约定路径：%USERPROFILE%\.ddns_go_config.yaml（-c 可覆盖，本托管不改传参恒用默认，
// 与用户自行运行的实例共享同一份配置——module.go 包注释实证）。
func userConfigFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %v", err)
	}
	return filepath.Join(home, ".ddns_go_config.yaml"), nil
}

// ---------- 版本解析 ----------

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，未设定/已失效回退最新已装。
func (s *DdnsGoService) resolveActiveVersion() (string, string, error) {
	if active := s.store.GetActive(); active != "" {
		if exe, err := s.manager.ResolveExe(active); err == nil {
			return active, exe, nil
		}
		// 已设定的版本被卸载/损坏：清空自愈，回退最新已装
		_ = s.store.SetActive("")
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", err
	}
	if len(installed) == 0 {
		return "", "", fmt.Errorf("尚未安装任何 ddns-go 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 6.9.0/6.10.0 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		if errA != nil || errB != nil {
			return strings.Compare(a, b) // 非规范段退化为字典序（正常数据不可达）
		}
		if na != nb {
			if na > nb {
				return 1
			}
			return -1
		}
	}
	return 0
}
