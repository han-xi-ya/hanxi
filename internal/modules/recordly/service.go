package recordly

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/recordly/instance"
	"hanxi/internal/modules/recordly/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	// ChannelStable / ChannelBeta 更新通道常量。beta 上游自注"需手动安装，可能
	// 不稳定"且缺 stable 自动更新元数据——默认 stable，beta 必须用户主动切换。
	ChannelStable = "stable"
	ChannelBeta   = "beta"

	readyTimeout  = 30 * time.Second // 冷启动就绪上限（Electron 主窗口出现；214MB 安装体首启偏慢，比 tauri 模块放宽）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	idleQuitAfter = 5 * time.Minute // 空闲自动退出阈值：无 Hanxi 发起操作且无可见窗口
	idleCheckTick = 30 * time.Second

	// disableAutoUpdateEnv 上游官方开关（updater.ts 实证）：阻止 electron-updater
	// 的 quitAndInstall 按注册表覆写 Hanxi 托管安装目录，版本升级统一走版本管理。
	disableAutoUpdateEnv = "RECORDLY_DISABLE_AUTO_UPDATES=1"
)

// RecordlyService 向前端暴露 Recordly 版本管理与窗口唤起能力。
// 录屏/剪辑操作不内嵌：打开 Recordly 自有窗口完成（其界面完整，内嵌重做
// 性价比低，且录屏依赖其原生 helper 进程树——决策记录见 module.go 包注释）。
type RecordlyService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *recordlyStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	idleMu       sync.Mutex
	lastActivity time.Time // 最近一次 Hanxi 发起的使用（打开窗口）；GetStatus 轮询不计
}

func NewRecordlyService(plat platform.Platform) *RecordlyService {
	paths := settings.GetPaths()
	svc := &RecordlyService{
		plat: plat,
		manager: version.NewManagerWithDesktop(paths.VersionsDir(), func() string {
			dir, err := plat.DesktopDir()
			if err != nil {
				return ""
			}
			return dir
		}),
		store: newRecordlyStore(paths.DataDir()),
	}
	svc.lastActivity = time.Now()
	svc.engine = instance.NewEngine(plat.Job(), instance.NewRecordlyProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 recordly:instance-state。
func (s *RecordlyService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("recordly instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("recordly:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("recordly", "Recordly 实例异常", snap.Error, "/ext/recordly")
	}
}

// activate 启动后台外部实例感知：5s 轮询进程名校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *RecordlyService) activate() {
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
	// 空闲退出巡检与外部感知共用同一个 watchStop 生命周期
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

// touch 记录一次 Hanxi 发起的实例使用（打开窗口算；状态轮询不算）。
func (s *RecordlyService) touch() {
	s.idleMu.Lock()
	s.lastActivity = time.Now()
	s.idleMu.Unlock()
}

// idleCheck 空闲自动退出巡检：仅退出自己托管的实例。
// 豁免两个场景：存在可见窗口（录制 HUD 浮层在场同样命中——录制中绝不退出）、
// 实例不是我们托管的（external）。Recordly Windows 版无托盘，
// "有进程无窗口"仅出现在启动早期/关闭收尾期，5 分钟阈值远大于该窗口。
func (s *RecordlyService) idleCheck() {
	s.idleMu.Lock()
	idle := time.Since(s.lastActivity)
	s.idleMu.Unlock()

	snap := s.engine.Snapshot()
	if !shouldIdleQuit(snap, s.engine.IsMainWindowOpen(), idle) {
		return
	}
	slog.Info("recordly idle auto-quit", "idle", idle.Truncate(time.Second))
	if err := s.engine.Quit(); err != nil {
		slog.Warn("recordly idle auto-quit failed", "err", err)
		return
	}
	notify.Info("recordly", "已自动退出", "Recordly 已空闲 5 分钟，自动退出以释放内存", "/ext/recordly")
}

// shouldIdleQuit 空闲退出判定（纯函数，便于单测穷举）。
func shouldIdleQuit(snap instance.Snapshot, windowOpen bool, idle time.Duration) bool {
	if snap.State != instance.StateRunning || snap.External {
		return false
	}
	if windowOpen {
		return false // 有可见窗口 = 用户可能正在用/正在录
	}
	return idle >= idleQuitAfter
}

// ---------- 版本管理（委托 manager） ----------

// GetReleaseChannel 返回当前更新通道（stable/beta）。
func (s *RecordlyService) GetReleaseChannel() (string, error) {
	return s.store.GetReleaseChannel(), nil
}

// SetReleaseChannel 切换更新通道（beta 含上游标注"可能不稳定"的预发布）。
func (s *RecordlyService) SetReleaseChannel(channel string) (string, error) {
	if err := s.store.SetReleaseChannel(channel); err != nil {
		return "", err
	}
	return channel, nil
}

// ListReleases 获取当前通道可用版本（多镜像回退，10 分钟缓存；
// 数据源为 GitHub releases——上游存在"空壳 tag"发布事故，tags 不可作准）。
func (s *RecordlyService) ListReleases() ([]version.RecordlyRelease, error) {
	return s.manager.ListRemote(s.store.GetReleaseChannel() == ChannelBeta)
}

// ListInstalledVersions 获取本地托管安装信息（至多一条，见 Manager 单目录说明）。
func (s *RecordlyService) ListInstalledVersions() ([]version.RecordlyVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载并静默安装指定版本：立即返回，
// 全程经事件 recordly:version-download 推送进度。
func (s *RecordlyService) DownloadVersion(targetVersion string) (string, error) {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	// 已安装同版本（含 beta 后缀与 PE 版本互认的数值核心一致场）直接返回
	installed, err := s.manager.ListInstalled()
	if err == nil && len(installed) > 0 && version.CompareCore(installed[0].Version, targetVersion) == 0 {
		return "already-installed", nil
	}
	// 覆盖安装 = NSIS 静默替换：运行中的实例会锁死目标文件，拒绝
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateStarting {
		return "", fmt.Errorf("Recordly 正在运行，请先退出再安装新版本")
	}

	go func() {
		emit := func(p version.DownloadProgress) {
			slog.Debug("recordly download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("recordly:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("recordly", "安装成功", fmt.Sprintf("Recordly %s 已安装完成", p.Version), "/ext/recordly")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("recordly", "安装失败", fmt.Sprintf("Recordly %s 安装失败: %v", targetVersion, err), "/ext/recordly")
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载托管版本（正在运行则拒绝）。%APPDATA%\Recordly 配置与录像保留。
func (s *RecordlyService) RemoveVersion(targetVersion string) error {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		version.CompareCore(snap.Version, targetVersion) == 0 {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	return s.manager.Remove(targetVersion)
}

// ImportLocal 导入本地已安装的 Recordly（整套 Electron 目录搬迁进托管目录）。
// 配置恒在 %APPDATA%\Recordly 不受导入影响；托管目录已有版本时需先卸载。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *RecordlyService) ImportLocal(srcDir string) (version.RecordlyVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.RecordlyVersionInfo{}, fmt.Errorf("Recordly 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开托管安装目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *RecordlyService) OpenDir(dir string) error {
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

// OpenConfigDir 打开 Recordly 的用户数据目录（配置与录像库所在）——纯托管下用户想看"数据在哪"的直达入口。只读导航，不改写。
func (s *RecordlyService) OpenConfigDir() error {
	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("Recordly 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// userConfigDir Electron userData 默认目录（模块注释与 THIRD_PARTY_NOTICES 实证：配置与录像恒在 %APPDATA%\Recordly）。
func userConfigDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("无法定位 APPDATA 目录")
	}
	return filepath.Join(appData, "Recordly"), nil
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *RecordlyService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：任一已装 exe 充当单实例信使，Electron second-instance 回调唤起外部主窗口；
//   - running：自有实例直接信使唤窗；
//   - stopped/failed：解析托管安装直接无参启动（Recordly 唯一启动语义即开窗）。
func (s *RecordlyService) OpenWindow() (ControlOutcome, error) {
	s.touch() // 用户主动打开 = 使用记录，重置空闲倒计时
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "Recordly 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 Recordly 窗口"}, nil

	case instance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 Recordly 窗口"}, nil

	default:
		v, exe, err := s.resolveInstalled()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{
			Version:  v,
			Exe:      exe,
			Env:      []string{disableAutoUpdateEnv},
			Detached: !s.store.GetFollowOnExit(),
		}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 Recordly 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 Recordly 主窗口就绪超时（%d 秒），请检查杀毒软件是否拦截未签名程序后重试", int(readyTimeout/time.Second))
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("Recordly %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 Recordly。
// external 状态不越权强杀（进程名探测拿不到主进程 PID）：仅返回人性化指引。
func (s *RecordlyService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 Recordly 窗口内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "Recordly 已退出"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *RecordlyService) Shutdown() {
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

// ---------- 版本解析 ----------

// resolveInstalled 解析当前托管安装（唯一目录，无 activeVersion 概念）。
func (s *RecordlyService) resolveInstalled() (string, string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", err
	}
	if len(installed) == 0 {
		return "", "", fmt.Errorf("尚未安装 Recordly，请先在版本管理在线安装或导入本地副本")
	}
	return installed[0].Version, installed[0].ExePath, nil
}

// resolveInstalledExeAny 返回托管 exe 路径（信使用途，与版本号无关）。
func (s *RecordlyService) resolveInstalledExeAny() (string, error) {
	_, exe, err := s.resolveInstalled()
	return exe, err
}

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *RecordlyService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *RecordlyService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为托管安装创建快捷方式（同名覆盖）。
// 上游安装器自动建的快捷方式会被装后清理（防绕过托管），此按钮是显式替代。
func (s *RecordlyService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveInstalled()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("Recordly", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *RecordlyService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *RecordlyService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
