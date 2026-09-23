package ddnsgo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hanxi/internal/extapi"
	"hanxi/internal/jsonstore"
	"hanxi/internal/modules/ddnsgo/instance"
	"hanxi/internal/modules/ddnsgo/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// ddnsInstallSteps 资产安装事务的 journal 步骤账目,与进度词表
// downloading/verify/extract 一一对应(download 覆盖流式下载段)。
var ddnsInstallSteps = []string{"download", "verify", "unpack"}

const (
	watchInterval = 5 * time.Second // 外部实例感知轮询间隔

	consoleWindowName = "ddnsgo-console" // 固定窗口名（Wails 窗口管理器内唯一键）

	// 控制台子窗口尺寸：默认展开 / 最小缩放两档（面板为上游 web 页，尺寸只影响首屏视野）。
	consoleWindowWidth  = 1120
	consoleWindowHeight = 820
	consoleMinWidth     = 760
	consoleMinHeight    = 520

	// configFileName 上游约定的单文件配置名：恒存 %USERPROFILE%\.ddns_go_config.yaml
	// （-c 可覆盖，本托管不改传参恒用默认，与用户自行运行的实例共享同一份配置）。
	configFileName = ".ddns_go_config.yaml"
)

// DdnsGoService 向前端暴露 ddns-go 版本管理、托管启停与内嵌 Web 控制台能力。
// DNS 解析配置操作在子 Webview 窗口内的上游原生页面完成（决策记录见包注释），
// 本服务只做托管：拉起/退出/状态/日志/端口设置，不重复实现上游功能面。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
// 独立控制台子窗入口（Start/OpenConsole）一并接门：停用模块点窗被拒符合预期。
type DdnsGoService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *ddnsgoStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	consoleMu  sync.Mutex // 子 Webview 窗口生命周期（创建于 RPC 线程，主循环内执行）
	consoleWin *application.WebviewWindow
	consoleURL string
}

// NewDdnsGoService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewDdnsGoService(plat platform.Platform, holder *extapi.LeaseHolder) *DdnsGoService {
	paths := settings.GetPaths()
	svc := &DdnsGoService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newDdnsgoStore(paths.StateDir()),
		holder:  holder,
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *DdnsGoService) ListInstalledVersions() ([]version.DdnsVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 ddnsgo:version-download 推送进度。
func (s *DdnsGoService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
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

	// Wave 4-B 资产安装事务：journal 先落盘再执行副作用，崩溃残件由
	// .tmp-<txnID> staging 经启动恢复背书法认领（照 ccswitch 定稿形态）。
	opKind := extapi.OpInstall
	if err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate
	}
	txnID := uuid.NewString()

	go func() {
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("ddnsgo:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error("ddnsgo", "版本下载失败", fmt.Sprintf("ddns-go %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/ddnsgo")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, ddnsInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("ddnsgo:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("ddnsgo", "版本下载失败", fmt.Sprintf("ddns-go %s 事务开启失败: %v", targetVersion, terr), "/ext/ddnsgo")
			return
		}
		stepIdx := -1
		var stepErr error
		defer func() {
			if txn.JournalFailed() {
				msg := "journal 事务步进失败"
				if stepErr != nil {
					msg = stepErr.Error()
				}
				txn.Fail("journal-degraded", msg)
			} else if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
			}
			txn.Close()
		}()
		emit := func(p version.DownloadProgress) {
			slog.Debug("ddnsgo download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("ddnsgo:version-download", p)
			}
			// journal 只在阶段迁移处落盘（§8.2）；下载分块进度只进观察面内存
			switch p.Stage {
			case "downloading":
				if stepIdx < 0 {
					stepIdx = 0
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
				txn.Progress(p.Done, p.Total)
			case "verify":
				if stepIdx < 1 {
					stepIdx = 1
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
			case "extract":
				if stepIdx < 2 {
					stepIdx = 2
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
			}
			if p.Stage == "done" {
				notify.Success("ddnsgo", "版本下载成功", fmt.Sprintf("ddns-go %s 已成功安装", p.Version), "/ext/ddnsgo")
			}
		}
		if err := s.manager.DownloadContext(txn.Context(), txnID, targetVersion, emit); err != nil {
			// N26 用户主动取消：按 2b 纪律如实收口，票面话术不露 ctx 原始错误；
			// 主动动作不发"失败"系统通知（前端票面已呈现「已取消」）。
			if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: "托管下载已取消"})
			} else {
				txn.Fail("asset-install-failed", err.Error())
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
				notify.Error("ddnsgo", "版本下载失败", fmt.Sprintf("ddns-go %s 下载失败: %v", targetVersion, err), "/ext/ddnsgo")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("ddnsgo", "版本下载失败", fmt.Sprintf("ddns-go %s 事务收口失败: %v", targetVersion, err), "/ext/ddnsgo")
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的 ddns-go.exe（任意目录下载的官方原版）。
// 配置恒在 ~/.ddns_go_config.yaml 不受导入影响；仅迁移单 exe。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *DdnsGoService) ImportLocal(srcDir string) (version.DdnsVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.DdnsVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.DdnsVersionInfo{}, fmt.Errorf("ddns-go 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.DdnsVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// Start 托管启动自有 ddns-go 实例（不弹面板）：
// external 状态不越权接管、running 幂等直返、stopped/failed 冷启动。
func (s *DdnsGoService) Start() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
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

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *DdnsGoService) Shutdown() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.shutdown()
	return nil
}

// shutdown 装配布线：Go 直调路径，不得依赖运行态（见 ADR-0001 Wave 3 注记）。
// 模块停用/应用退出：停后台轮询 + 终止自有实例（强杀通道，不等待
// 配置写静默期——OnShutdown 阻塞返回）。外部实例不受影响；自有实例另受
// JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *DdnsGoService) shutdown() {
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
// 关闭按钮语义为隐藏复用（RegisterHook Cancel+Hide），动机只有一条：
// 隐藏复用保留 WebView2 会话 Cookie，下次打开免重复登录。
// （旧注释另载的"误关子窗牵连最后窗口退出"顾虑已被证伪——退出判据数的是
// windowMap 存活总数、含隐藏窗且主窗恒在，子窗真销毁带不崩应用，
// 最后窗口判据见 TROUBLESHOOTING #56。）
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
		Width:            consoleWindowWidth,
		Height:           consoleWindowHeight,
		MinWidth:         consoleMinWidth,
		MinHeight:        consoleMinHeight,
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// Logs 返回实例最近 n 行进程输出（引擎重启会清空，前端另以事件流累积）。
func (s *DdnsGoService) Logs(n int) ([]string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	if n <= 0 || n > instance.LogCapacityHint {
		n = instance.LogCapacityHint
	}
	return s.engine.Logs(n), nil
}

// ---------- 端口与联动设置 ----------

// GetListenPort 返回 web 监听端口。
func (s *DdnsGoService) GetListenPort() (int, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return 0, gateErr
	}
	defer release()
	return s.store.GetListenPort(), nil
}

// SetListenPort 设定端口（1024~65535，下次启动生效；运行中实例不变）。
func (s *DdnsGoService) SetListenPort(port int) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	if err := jsonstore.ValidateListenPort(port); err != nil {
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

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *DdnsGoService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *DdnsGoService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *DdnsGoService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *DdnsGoService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir（存在性/类型校验与中文报错内置；入参恒为目录，
// 刻意不走 explorer.exe <file> 的"执行"语义——markeron「打开安装目录」按钮的事故教训）。
func (s *DdnsGoService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// OpenConfigDir 在资源管理器中定位 ddns-go 的配置文件（%USERPROFILE%\.ddns_go_config.yaml）——
// 上游配置为单文件而非目录，直接打开整个用户主目录噪音过大，采用 /select 高亮定位。只读导航，不改写。
func (s *DdnsGoService) OpenConfigDir() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	file, err := userConfigFile()
	if err != nil {
		return err
	}
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("ddns-go 配置文件尚未创建（程序还未保存过配置）: %s", file)
	}
	return windows.RevealFile(file)
}

// userConfigFile 上游约定路径：%USERPROFILE%\.ddns_go_config.yaml（-c 可覆盖，本托管不改传参恒用默认，
// 与用户自行运行的实例共享同一份配置——module.go 包注释实证）。
func userConfigFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %v", err)
	}
	return filepath.Join(home, configFileName), nil
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
	// 数值分段比较实现收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）。
	return versioncmp.Compare(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}
