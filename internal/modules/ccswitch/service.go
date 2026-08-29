package ccswitch

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

	"hanxi/internal/modules/ccswitch/instance"
	"hanxi/internal/modules/ccswitch/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（单实例互斥体出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	idleQuitAfter = 3 * time.Minute // 空闲自动退出阈值：无 Hanxi 发起操作且主窗口未开
	idleCheckTick = 30 * time.Second
)

// CCSwitchService 向前端暴露 CC Switch 版本管理与窗口唤起能力。
// 供应商切换本身不内嵌：打开 CC Switch 自有窗口操作（其界面完整，
// 且切换逻辑直写用户 Claude Code/Codex 配置，由原版实现最稳妥）。
type CCSwitchService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *ccswitchStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	idleMu       sync.Mutex
	lastActivity time.Time // 最近一次 Hanxi 发起的使用（打开窗口）；GetStatus 轮询不计
}

func NewCCSwitchService(plat platform.Platform) *CCSwitchService {
	paths := settings.GetPaths()
	svc := &CCSwitchService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newCCSwitchStore(paths.DataDir()),
	}
	svc.lastActivity = time.Now()
	svc.engine = instance.NewEngine(plat.Job(), instance.NewCCProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 ccswitch:instance-state。
func (s *CCSwitchService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("ccswitch instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("ccswitch:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("ccswitch", "CC Switch 实例异常", snap.Error, "/ext/ccswitch")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *CCSwitchService) activate() {
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
func (s *CCSwitchService) touch() {
	s.idleMu.Lock()
	s.lastActivity = time.Now()
	s.idleMu.Unlock()
}

// idleCheck 空闲自动退出巡检：仅退出自己托管的实例。
// 豁免两个场景：主窗口正在显示（用户可能正在用）、实例不是我们托管的（external）。
// 用户侧"关窗驻托盘"（窗口隐藏）不豁免——无人操作 3 分钟即退出是明确需求。
func (s *CCSwitchService) idleCheck() {
	s.idleMu.Lock()
	idle := time.Since(s.lastActivity)
	s.idleMu.Unlock()

	snap := s.engine.Snapshot()
	if !shouldIdleQuit(snap, s.engine.IsMainWindowOpen(), idle) {
		return
	}
	slog.Info("ccswitch idle auto-quit", "idle", idle.Truncate(time.Second))
	if err := s.engine.Quit(); err != nil {
		slog.Warn("ccswitch idle auto-quit failed", "err", err)
		return
	}
	notify.Info("ccswitch", "已自动退出", "CC Switch 已空闲 3 分钟，自动退出以释放内存", "/ext/ccswitch")
}

// shouldIdleQuit 空闲退出判定（纯函数，便于单测穷举）。
func shouldIdleQuit(snap instance.Snapshot, windowOpen bool, idle time.Duration) bool {
	if snap.State != instance.StateRunning || snap.External {
		return false
	}
	if windowOpen {
		return false // 主窗口开着 = 用户可能正在用
	}
	return idle >= idleQuitAfter
}

// ---------- 版本管理（委托 manager） ----------

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存）。
func (s *CCSwitchService) ListReleases() ([]version.CCRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *CCSwitchService) ListInstalledVersions() ([]version.CCVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 ccswitch:version-download 推送进度。
func (s *CCSwitchService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("ccswitch download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("ccswitch:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("ccswitch", "版本下载成功", fmt.Sprintf("CC Switch %s 已成功安装", p.Version), "/ext/ccswitch")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("ccswitch", "版本下载失败", fmt.Sprintf("CC Switch %s 下载失败: %v", targetVersion, err), "/ext/ccswitch")
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
func (s *CCSwitchService) RemoveVersion(targetVersion string) error {
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
func (s *CCSwitchService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *CCSwitchService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已安装的 CC Switch（安装版/绿色版均可）。
// 配置恒在 ~/.cc-switch 不受导入影响；仅迁移 exe 与便携标记。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *CCSwitchService) ImportLocal(srcDir string) (version.CCVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.CCVersionInfo{}, fmt.Errorf("CC Switch 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *CCSwitchService) OpenDir(dir string) error {
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

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *CCSwitchService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：任一已装 exe 充当单实例信使，插件回调 show+focus 外部主窗口；
//   - running：自有实例直接信使唤窗；
//   - stopped/failed：解析 active 版本直接无参启动（CC Switch 唯一启动语义即开窗）。
func (s *CCSwitchService) OpenWindow() (ControlOutcome, error) {
	s.touch() // 用户主动打开 = 使用记录，重置空闲倒计时
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "CC Switch 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 CC Switch 窗口"}, nil

	case instance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 CC Switch 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 CC Switch 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 CC Switch 就绪超时（%d 秒），请确认已安装 WebView2 Runtime", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("CC Switch %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 CC Switch。
// external 状态不越权强杀（互斥体探测拿不到 PID）：仅返回人性化指引。
func (s *CCSwitchService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 CC Switch 托盘或窗口内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "CC Switch 已退出"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *CCSwitchService) Shutdown() {
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

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，未设定/已失效回退最新已装。
func (s *CCSwitchService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 CC Switch 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（信使用途，与版本号无关）。
func (s *CCSwitchService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 CC Switch 版本，无法代为唤起外部实例窗口")
	}
	return installed[0].ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 3.10.0/3.9.0 这类多位数段有误，必须数值分段比较。
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

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *CCSwitchService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *CCSwitchService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *CCSwitchService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("CC Switch", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *CCSwitchService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *CCSwitchService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
