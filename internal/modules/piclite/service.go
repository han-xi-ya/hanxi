package piclite

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

	"hanxi/internal/modules/piclite/instance"
	"hanxi/internal/modules/piclite/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（单实例互斥体出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	idleQuitAfter = 3 * time.Minute // 空闲自动退出阈值：无 Hanxi 发起操作且无可见用户窗口
	idleCheckTick = 30 * time.Second
)

// PicLiteService 向前端暴露 PicLite 版本管理与窗口唤起能力。
// 压缩工作台本身不内嵌：打开 PicLite 自有窗口操作（其工作台/悬浮窗/图床上传
// 界面完整，内嵌重做性价比低，且悬浮结果流依赖上游全局快捷键与剪贴板监听）。
type PicLiteService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *picliteStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	idleMu       sync.Mutex
	lastActivity time.Time // 最近一次 Hanxi 发起的使用（打开窗口）；GetStatus 轮询不计
}

func NewPicLiteService(plat platform.Platform) *PicLiteService {
	paths := settings.GetPaths()
	svc := &PicLiteService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newPicliteStore(paths.DataDir()),
	}
	svc.lastActivity = time.Now()
	svc.engine = instance.NewEngine(plat.Job(), instance.NewPicProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 piclite:instance-state。
func (s *PicLiteService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("piclite instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("piclite:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("piclite", "PicLite 实例异常", snap.Error, "/ext/piclite")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *PicLiteService) activate() {
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
func (s *PicLiteService) touch() {
	s.idleMu.Lock()
	s.lastActivity = time.Now()
	s.idleMu.Unlock()
}

// idleCheck 空闲自动退出巡检：仅退出自己托管的实例。
// 豁免两个场景：有可见用户窗口（主窗/悬浮结果/拖放区任一开着 = 用户可能在用）、
// 实例不是我们托管的（external）。
// 上游"关窗驻托盘"（窗口全部隐藏）不豁免——无人操作 3 分钟即退出是明确需求。
func (s *PicLiteService) idleCheck() {
	s.idleMu.Lock()
	idle := time.Since(s.lastActivity)
	s.idleMu.Unlock()

	snap := s.engine.Snapshot()
	if !shouldIdleQuit(snap, s.engine.IsUserWindowOpen(), idle) {
		return
	}
	slog.Info("piclite idle auto-quit", "idle", idle.Truncate(time.Second))
	if err := s.engine.Quit(); err != nil {
		slog.Warn("piclite idle auto-quit failed", "err", err)
		return
	}
	notify.Info("piclite", "已自动退出", "PicLite 已空闲 3 分钟，自动退出以释放内存", "/ext/piclite")
}

// shouldIdleQuit 空闲退出判定（纯函数，便于单测穷举）。
func shouldIdleQuit(snap instance.Snapshot, windowOpen bool, idle time.Duration) bool {
	if snap.State != instance.StateRunning || snap.External {
		return false
	}
	if windowOpen {
		return false // 有可见窗口 = 用户可能正在用
	}
	return idle >= idleQuitAfter
}

// ---------- 版本管理（委托 manager） ----------

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存）。
func (s *PicLiteService) ListReleases() ([]version.PicRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *PicLiteService) ListInstalledVersions() ([]version.PicVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 piclite:version-download 推送进度。
func (s *PicLiteService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("piclite download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("piclite:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("piclite", "版本安装成功", fmt.Sprintf("PicLite %s 已成功安装", p.Version), "/ext/piclite")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("piclite", "版本安装失败", fmt.Sprintf("PicLite %s 安装失败: %v", targetVersion, err), "/ext/piclite")
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
func (s *PicLiteService) RemoveVersion(targetVersion string) error {
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
func (s *PicLiteService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *PicLiteService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已安装的 PicLite（官方安装版目录即可：Program Files\PicLite）。
// 配置恒在 %APPDATA%\com.piclite.desktop 不受导入影响；仅迁移 exe。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *PicLiteService) ImportLocal(srcDir string) (version.PicVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.PicVersionInfo{}, fmt.Errorf("PicLite 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *PicLiteService) OpenDir(dir string) error {
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
func (s *PicLiteService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：任一已装 exe 充当单实例信使，插件回调 show+focus 外部主窗口；
//   - running：自有实例直接信使唤窗；
//   - stopped/failed：解析 active 版本直接无参启动（PicLite 唯一启动语义即开窗）。
func (s *PicLiteService) OpenWindow() (ControlOutcome, error) {
	s.touch() // 用户主动打开 = 使用记录，重置空闲倒计时
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "PicLite 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 PicLite 窗口"}, nil

	case instance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 PicLite 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 PicLite 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 PicLite 就绪超时（%d 秒），请确认已安装 WebView2 Runtime", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("PicLite %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 PicLite（上游无优雅退出通道，托管侧经 JobObject 直接终止，
// 理由与数据安全性论证见 instance 包注释）。
// external 状态不越权强杀（互斥体探测拿不到 PID）：仅返回人性化指引。
func (s *PicLiteService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 PicLite 托盘图标菜单中退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "PicLite 已退出"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *PicLiteService) Shutdown() {
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
func (s *PicLiteService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 PicLite 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（信使用途，与版本号无关）。
func (s *PicLiteService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 PicLite 版本，无法代为唤起外部实例窗口")
	}
	return installed[0].ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 1.10.0/1.9.0 这类多位数段有误，必须数值分段比较。
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
func (s *PicLiteService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *PicLiteService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *PicLiteService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("PicLite", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *PicLiteService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *PicLiteService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
