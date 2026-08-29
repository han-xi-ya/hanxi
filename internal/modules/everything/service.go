package everything

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	evinstance "hanxi/internal/modules/everything/instance"
	evsearch "hanxi/internal/modules/everything/search"
	evversion "hanxi/internal/modules/everything/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/settings"
)

const (
	readyTimeout      = 20 * time.Second // 冷启动就绪上限（托盘通知窗口出现）
	watchInterval     = 5 * time.Second  // 外部实例感知轮询间隔
	searchResultLimit = 300              // 单次内嵌搜索上限（与 evsearch.maxResults 对齐）

	idleQuitAfter = 3 * time.Minute // 空闲自动退出阈值：无 Hanxi 发起操作且搜索窗口未开
	idleCheckTick = 30 * time.Second
)

// EverythingService 向前端暴露 Everything 版本管理、托管控制与内嵌搜索能力。
type EverythingService struct {
	plat    platform.Platform
	manager *evversion.Manager
	store   *everythingStore
	engine  *evinstance.Engine
	esDir   string // ES 搜索组件目录（dataDir/everything/es，版本无关）

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	controlMu  sync.Mutex // Start/OpenWindow/Quit/Search 编排串行化
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	idleMu       sync.Mutex
	lastActivity time.Time // 最近一次 Hanxi 发起的使用（搜索/开窗/启动）；GetStatus 轮询不计
}

func NewEverythingService(plat platform.Platform) *EverythingService {
	paths := settings.GetPaths()
	svc := &EverythingService{
		plat:    plat,
		manager: evversion.NewManager(paths.VersionsDir()),
		store:   newEverythingStore(paths.DataDir()),
		esDir:   filepath.Join(paths.DataDir(), "everything", "es"),
	}
	svc.lastActivity = time.Now()
	svc.engine = evinstance.NewEngine(plat.Job(), evinstance.NewEverythingProbe(), evinstance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 everything:instance-state。
func (s *EverythingService) emitInstanceState(snap evinstance.Snapshot) {
	slog.Debug("everything instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("everything:instance-state", snap)
	}
	if snap.State == evinstance.StateFailed && snap.Error != "" {
		notify.Error("everything", "Everything 实例异常", snap.Error, "/ext/everything")
	}
}

// emitDownload 统一下载进度事件（app 版本包 / es 搜索组件共用）。
func (s *EverythingService) emitDownload(t DownloadTicket) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("everything:download", t)
	}
}

// activate 启动两个后台任务：外部实例感知轮询（5s）+ 空闲自动退出巡检（30s）。
// 两个任务共用同一 stop 通道，Shutdown 一并终止。
func (s *EverythingService) activate() {
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
	go func() {
		t := time.NewTicker(idleCheckTick)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.idleCheck()
			}
		}
	}()
}

// touch 记录一次 Hanxi 发起的实例使用（搜索/开窗/启动都算；状态轮询不算）。
func (s *EverythingService) touch() {
	s.idleMu.Lock()
	s.lastActivity = time.Now()
	s.idleMu.Unlock()
}

// idleCheck 空闲自动退出巡检：仅退出自己托管的纯后台实例。
// 严格豁免两个场景：搜索窗口正在显示（用户可能在用）、实例不是我们托管的（external）。
func (s *EverythingService) idleCheck() {
	s.idleMu.Lock()
	idle := time.Since(s.lastActivity)
	s.idleMu.Unlock()

	snap := s.engine.Snapshot()
	if !shouldIdleQuit(snap, s.engine.IsSearchWindowOpen(), idle) {
		return
	}
	slog.Info("everything idle auto-quit", "idle", idle.Truncate(time.Second))
	if err := s.engine.Quit(); err != nil {
		slog.Warn("everything idle auto-quit failed", "err", err)
		return
	}
	notify.Info("everything", "已自动退出", "Everything 已空闲 3 分钟，自动退出以释放内存；再次搜索会自动重启", "/ext/everything")
}

// shouldIdleQuit 空闲退出判定（纯函数，便于单测穷举）。
func shouldIdleQuit(snap evinstance.Snapshot, windowOpen bool, idle time.Duration) bool {
	if snap.State != evinstance.StateRunning || snap.External {
		return false
	}
	if windowOpen {
		return false // 搜索窗口开着 = 用户可能正在用
	}
	return idle >= idleQuitAfter
}

// ---------- 托管控制 ----------

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）
func (s *EverythingService) GetStatus() (evinstance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// StartBackground 启动后台实例（-startup：后台驻留建索引，不弹搜索窗）。
func (s *EverythingService) StartBackground() (ControlOutcome, error) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.touch()
	return s.startBackgroundLocked()
}

// startBackgroundLocked StartBackground 的无锁版：供已持 controlMu 的编排方复用
// （Search 懒启动路径）。controlMu 不是可重入锁，嵌套调用会死锁——新增编排方法时
// 凡已持锁者一律调用本方法。
func (s *EverythingService) startBackgroundLocked() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	switch s.engine.Snapshot().State {
	case evinstance.StateExternal:
		return ControlOutcome{Action: "external-running", External: true,
			Message: "已有外部 Everything 实例在运行，无需重复启动"}, nil
	case evinstance.StateRunning, evinstance.StateStarting:
		return ControlOutcome{Action: "already-running", Message: "Everything 已在运行"}, nil
	}

	v, exe, err := s.resolveActiveVersion()
	if err != nil {
		return ControlOutcome{}, err
	}
	if err := s.engine.Start(evinstance.StartOptions{Version: v, Exe: exe, Mode: evinstance.ModeBackground}); err != nil {
		return ControlOutcome{}, fmt.Errorf("启动 Everything 失败: %w", err)
	}
	if !s.engine.WaitReady(readyTimeout) {
		if cur := s.engine.Snapshot(); cur.State == evinstance.StateFailed && cur.Error != "" {
			return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
		}
		return ControlOutcome{}, fmt.Errorf("等待 Everything 就绪超时（%d 秒），请检查索引库是否损坏", int(readyTimeout/time.Second))
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
	}
	return ControlOutcome{Action: "started-background",
		Message: fmt.Sprintf("Everything %s 已在后台启动（后台驻留建索引）", v)}, nil
}

// OpenWindow 打开/唤起搜索窗口：
//   - 运行中（自有/外部）→ 单实例协议信使唤窗；
//   - 未运行 → 窗口模式冷启动。
func (s *EverythingService) OpenWindow() (ControlOutcome, error) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.touch()

	s.engine.RefreshExternal()
	switch snap := s.engine.Snapshot(); snap.State {
	case evinstance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "opened", Message: "已唤起搜索窗口"}, nil
	case evinstance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			// 未装任何版本时无法派信使：提示用户直接点外部实例的托盘/热键
			return ControlOutcome{Action: "external-opened", External: true, Message: err.Error()}, nil
		}
		if _, err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已向外部 Everything 实例发出窗口唤起"}, nil
	case evinstance.StateStarting:
		return ControlOutcome{Action: "busy", Message: "实例启动中，请稍候"}, nil
	}

	v, exe, err := s.resolveActiveVersion()
	if err != nil {
		return ControlOutcome{}, err
	}
	if err := s.engine.Start(evinstance.StartOptions{Version: v, Exe: exe, Mode: evinstance.ModeWindow}); err != nil {
		return ControlOutcome{}, fmt.Errorf("启动 Everything 失败: %w", err)
	}
	if !s.engine.WaitReady(readyTimeout) {
		if cur := s.engine.Snapshot(); cur.State == evinstance.StateFailed && cur.Error != "" {
			return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
		}
		return ControlOutcome{}, fmt.Errorf("等待 Everything 就绪超时（%d 秒），请检查索引库是否损坏", int(readyTimeout/time.Second))
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(v)
	}
	return ControlOutcome{Action: "started-window", Message: fmt.Sprintf("Everything %s 已启动，搜索窗口已打开", v)}, nil
}

// Quit 退出 Everything。
// 外部实例不越权强杀（实例探测拿不到 PID）：仅返回人性化指引。
// 自有实例走 -quit 优雅退出（先落盘索引库），超时由引擎强杀兜底。
func (s *EverythingService) Quit() (QuitOutcome, error) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()

	s.engine.RefreshExternal()
	if snap := s.engine.Snapshot(); snap.State == evinstance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 Everything 托盘图标上退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "Everything 已退出"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 优雅退出自有实例（5s 兜底强杀）。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *EverythingService) Shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	_ = s.engine.Quit()
}

// ---------- 内嵌搜索 ----------

// EnsureSearchTool 确保 ES 搜索组件已安装（幂等；下载进度经 everything:download 事件推送）。
func (s *EverythingService) EnsureSearchTool() (string, error) {
	if err := evsearch.EnsureESExe(s.esDir, func(stage string) {
		s.emitDownload(DownloadTicket{Component: "es", Version: evsearch.ESVersion(), Stage: stage})
	}); err != nil {
		return "", err
	}
	return "ready", nil
}

// Search 内嵌搜索：查询运行中实例的索引，返回路径列表。
// 无实例时先懒启动后台实例（等待就绪后查询），用户无感衔接。
func (s *EverythingService) Search(query string, limit int) ([]evsearch.Result, error) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	s.touch()

	// 1. 确保实例在场（外部实例亦可用于查询——ES 只认默认实例，外部若为 -instance 定制则查不到）
	s.engine.RefreshExternal()
	if snap := s.engine.Snapshot(); snap.State == evinstance.StateStopped || snap.State == evinstance.StateFailed {
		if _, err := s.startBackgroundLocked(); err != nil {
			return nil, err
		}
	}

	// 2. 确保搜索组件在场（缺失则同步补齐，失败时建议用户重试）
	if err := evsearch.EnsureESExe(s.esDir, func(stage string) {
		s.emitDownload(DownloadTicket{Component: "es", Version: evsearch.ESVersion(), Stage: stage})
	}); err != nil {
		return nil, fmt.Errorf("搜索组件就绪失败: %w", err)
	}

	// 3. 经 ES.exe 查询运行中的实例（10s 超时）
	results, err := evsearch.Search(evsearch.ESExePath(s.esDir), query, limit)
	if err != nil {
		if snap := s.engine.Snapshot(); snap.State == evinstance.StateExternal {
			return nil, fmt.Errorf("%w（若外部实例使用了 -instance 定制名，请用 Everything 自带窗口搜索）", err)
		}
		return nil, err
	}
	return results, nil
}

// OpenTarget 以系统默认方式打开搜索结果目标：
//   - 目录 → 资源管理器打开该目录；
//   - 文件 → 默认关联程序打开（rundll32 FileProtocolHandler，不限于 exe 等可执行文件）。
func (s *EverythingService) OpenTarget(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("目标路径不能为空")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("目标不存在或不可访问: %s", path)
	}
	var cmd *exec.Cmd
	if fi.IsDir() {
		cmd = exec.Command("explorer.exe", path)
	} else {
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	}
	return cmd.Start()
}

// RevealTarget 在资源管理器中定位并选中目标（文件/目录通用）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"而非"定位"
// （markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
func (s *EverythingService) RevealTarget(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("目标路径不能为空")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("目标不存在或不可访问: %s", path)
	}
	// /select, 与路径必须为同一参数，中间逗号是语法一部分
	return exec.Command("explorer.exe", "/select,"+path).Start()
}

// ---------- 版本管理（委托 manager） ----------

// ListReleases 获取远程可用版本槽位（稳定版 + 1.5 测试版，stale 标记降级状态）
func (s *EverythingService) ListReleases() ([]evversion.EverythingRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表
func (s *EverythingService) ListInstalledVersions() ([]evversion.EverythingVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 everything:download 推送进度。
func (s *EverythingService) DownloadVersion(targetVersion string) (string, error) {
	targetVersion = strings.TrimSpace(targetVersion)

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	// 已安装则直接返回，避免重复下载
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(v.Version, targetVersion) {
				return "already-installed", nil
			}
		}
	}

	go func() {
		emit := func(p evversion.DownloadProgress) {
			s.emitDownload(DownloadTicket{Component: "app", Version: p.Version, Stage: p.Stage, Done: p.Done, Total: p.Total, Message: p.Message})
			if p.Stage == "done" {
				notify.Success("everything", "版本下载成功", fmt.Sprintf("Everything %s 已成功安装", p.Version), "/ext/everything")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(evversion.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("everything", "版本下载失败", fmt.Sprintf("Everything %s 下载失败: %v", targetVersion, err), "/ext/everything")
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）
func (s *EverythingService) RemoveVersion(targetVersion string) error {
	targetVersion = strings.TrimSpace(targetVersion)
	if snap := s.engine.Snapshot(); snap.State == evinstance.StateRunning &&
		strings.EqualFold(snap.Version, targetVersion) {
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

// SetActiveVersion 设定使用版本（先校验已安装，再持久化）
func (s *EverythingService) SetActiveVersion(targetVersion string) (string, error) {
	targetVersion = strings.TrimSpace(targetVersion)
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）
func (s *EverythingService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地便携安装整套（exe+配置+语言包+索引库）。
// 源目录实例运行中（自有/外部）时拒绝：索引库被写锁，拷贝结果不可信。
func (s *EverythingService) ImportLocal(srcDir string) (evversion.EverythingVersionInfo, error) {
	srcDir = strings.TrimSpace(srcDir)
	if srcDir == "" {
		return evversion.EverythingVersionInfo{}, fmt.Errorf("请填写 Everything 安装目录路径")
	}
	if snap := s.engine.Snapshot(); snap.State == evinstance.StateRunning || snap.State == evinstance.StateExternal {
		return evversion.EverythingVersionInfo{},
			fmt.Errorf("检测到 Everything 实例正在运行（索引库可能被占用），请先在 Everything 托盘退出再导入")
	}
	info, err := s.manager.ImportLocal(srcDir)
	if err != nil {
		return evversion.EverythingVersionInfo{}, err
	}
	notify.Success("everything", "导入成功", fmt.Sprintf("Everything %s 已导入（含配置与索引库）", info.Version), "/ext/everything")
	return info, nil
}

// ---------- 版本解析 ----------

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，未设定/已失效回退最新已装。
func (s *EverythingService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 Everything 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versioncmp.Compare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（信使用途，与版本号无关）
func (s *EverythingService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 Everything 版本，无法代为唤起外部实例——请在 Everything 托盘图标上打开")
	}
	return installed[0].ExePath, nil
}
