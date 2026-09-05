package litemonitor

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

	"hanxi/internal/modules/litemonitor/instance"
	"hanxi/internal/modules/litemonitor/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（进程出现；首启含 PawnIO 驱动检查对话框交互时由用户自行推进）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// LiteMonitorService 向前端暴露 LiteMonitor 版本管理与实例控制能力。
// 监控条/主题/插件等全部配置操作不内嵌：打开 LiteMonitor 自有界面完成
// （纯托管决策——其桌面常驻形态即产品价值，内嵌重做性价比为零）。
//
// 与 ccswitch 引擎的适配差异（上游契约侦查结论，详见 instance 包注释）：
//   - 单实例互斥体名随安装路径派生 → 探测/唤窗全部走进程枚举 + Win32 直操作；
//   - manifest requireAdministrator → 未提权拉起特判文案指引；
//   - 常驻监控定位 → 不做空闲自动退出（与 FlClash 同产品约束：后台监控
//     正是其用途，空闲即退语义不成立）。
type LiteMonitorService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *litemonitorStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewLiteMonitorService(plat platform.Platform) *LiteMonitorService {
	paths := settings.GetPaths()
	svc := &LiteMonitorService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newLiteMonitorStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewLiteMonitorProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 litemonitor:instance-state。
func (s *LiteMonitorService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("litemonitor instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("litemonitor:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("litemonitor", "LiteMonitor 实例异常", snap.Error, "/ext/litemonitor")
	}
}

// activate 启动后台外部实例感知：5s 轮询进程存在性校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *LiteMonitorService) activate() {
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
func (s *LiteMonitorService) ListReleases() ([]version.LMRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *LiteMonitorService) ListInstalledVersions() ([]version.LMVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 litemonitor:version-download 推送进度。
func (s *LiteMonitorService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("litemonitor download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("litemonitor:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("litemonitor", "版本下载成功", fmt.Sprintf("LiteMonitor %s 已成功安装", p.Version), "/ext/litemonitor")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("litemonitor", "版本下载失败", fmt.Sprintf("LiteMonitor %s 下载失败: %v", targetVersion, err), "/ext/litemonitor")
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
func (s *LiteMonitorService) RemoveVersion(targetVersion string) error {
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
func (s *LiteMonitorService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *LiteMonitorService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已解压的 LiteMonitor 便携套件（settings/themes/plugins 整套迁移）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *LiteMonitorService) ImportLocal(srcDir string) (version.LMVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.LMVersionInfo{}, fmt.Errorf("LiteMonitor 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *LiteMonitorService) OpenDir(dir string) error {
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
func (s *LiteMonitorService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：进程枚举拿外部 PID → EnumWindows SW_RESTORE+SetForegroundWindow
//     直接唤窗（LiteMonitor 二次启动静默退出不唤窗，不能走信使）；
//   - running：自有实例同样直操作窗口；
//   - stopped/failed：解析 active 版本直接无参启动（启动即显示监控条）。
func (s *LiteMonitorService) OpenWindow() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会触发上游单实例竞速）
		return ControlOutcome{Action: "starting", Message: "LiteMonitor 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		s.engine.RestoreExternalWindow(s.engine.ExternalPIDs())
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 LiteMonitor 监控条"}, nil

	case instance.StateRunning:
		s.engine.RestoreWindow()
		return ControlOutcome{Action: "opened", Message: "已唤起 LiteMonitor 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 LiteMonitor 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 LiteMonitor 就绪超时（%d 秒），请确认已安装 .NET 8 桌面运行时", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("LiteMonitor %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 LiteMonitor。
// external 状态不越权强杀（进程枚举不持句柄）：仅返回人性化指引。
func (s *LiteMonitorService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 LiteMonitor 托盘或菜单内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "LiteMonitor 已退出"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *LiteMonitorService) Shutdown() {
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
func (s *LiteMonitorService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 LiteMonitor 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 1.10.0/1.9.0 这类多位数段有误，必须数值分段比较。
// imported- 兜底目录恒视为最小（不参与数值比较）。
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

// ---------- 环境探测与桌面辅助 ----------

// GetRuntimeStatus 探测系统 .NET 桌面运行时（框架依赖版 LiteMonitor 的可运行性判断，
// 前端据此展示"需安装 .NET 8 桌面运行时"条件提示条）。
func (s *LiteMonitorService) GetRuntimeStatus() RuntimeStatus {
	vers := version.DesktopRuntimeVersions()
	return RuntimeStatus{
		DesktopRuntimes: vers,
		HasDesktop8:     version.HasDesktopRuntimeMajor(vers, version.RequiresDesktopMajor),
	}
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *LiteMonitorService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *LiteMonitorService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *LiteMonitorService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("LiteMonitor", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *LiteMonitorService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *LiteMonitorService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
