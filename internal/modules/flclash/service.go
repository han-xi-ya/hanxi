package flclash

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

	"hanxi/internal/modules/flclash/instance"
	"hanxi/internal/modules/flclash/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（Flutter 首拉起约 1~3s，放宽覆盖弱机）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// FlClashService 向前端暴露 FlClash 版本管理与窗口唤起能力。
// 代理订阅/节点配置不内嵌：打开 FlClash 自有窗口操作（界面完整，
// 配置数据在 %APPDATA% 用户目录，各版本共享）。
type FlClashService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *flclashStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewFlClashService(plat platform.Platform) *FlClashService {
	paths := settings.GetPaths()
	svc := &FlClashService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newFlClashStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewFlClashProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 flclash:instance-state。
func (s *FlClashService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("flclash instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("flclash:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("flclash", "FlClash 实例异常", snap.Error, "/ext/flclash")
	}
}

// activate 启动后台外部实例感知。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *FlClashService) activate() {
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

// shouldIdleQuit 固化代理不因空闲自动退出的产品约束；生产代码不启动空闲巡检。
func shouldIdleQuit(instance.Snapshot, bool, time.Duration) bool {
	return false
}

// ---------- 版本管理（委托 manager） ----------

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存）。
func (s *FlClashService) ListReleases() ([]version.FlClashRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *FlClashService) ListInstalledVersions() ([]version.FlClashVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 flclash:version-download 推送进度。
func (s *FlClashService) DownloadVersion(targetVersion string) (string, error) {
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
		emit := func(p version.DownloadProgress) {
			slog.Debug("flclash download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("flclash:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("flclash", "版本下载成功", fmt.Sprintf("FlClash %s 已成功安装", p.Version), "/ext/flclash")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("flclash", "版本下载失败", fmt.Sprintf("FlClash %s 下载失败: %v", targetVersion, err), "/ext/flclash")
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
func (s *FlClashService) RemoveVersion(targetVersion string) error {
	targetVersion = strings.TrimSpace(targetVersion)
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
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

// SetActiveVersion 设定使用版本（先校验已安装，再持久化）。
func (s *FlClashService) SetActiveVersion(targetVersion string) (string, error) {
	targetVersion = strings.TrimSpace(targetVersion)
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）。
func (s *FlClashService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的 FlClash 便携目录（黑名单整搬：exe+dll+data+全套）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *FlClashService) ImportLocal(srcDir string) (version.FlClashVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.FlClashVersionInfo{}, fmt.Errorf("FlClash 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
func (s *FlClashService) OpenDir(dir string) error {
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

// OpenConfigDir 打开 FlClash 的用户数据目录（订阅配置 config.yaml、profiles 与数据库所在）——纯托管下用户想看"数据在哪"的直达入口。只读导航，不改写。
func (s *FlClashService) OpenConfigDir() error {
	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("FlClash 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// userConfigDir path_provider getApplicationSupportDirectory 按 PE 版本信息拼 %APPDATA%\<CompanyName>\<ProductName>，上游 windows/runner/Runner.rc 实证 CompanyName=com.follow、ProductName=clash。
func userConfigDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("无法定位 APPDATA 目录")
	}
	return filepath.Join(appData, "com.follow", "clash"), nil
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *FlClashService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：进程枚举拿外部 PID → EnumWindows SW_RESTORE+SetForegroundWindow
//     直接唤窗（FlClash 二次启动只退出不唤窗，不能走信使）；
//   - running：自有实例同样直操作窗口；
//   - stopped/failed：解析 active 版本直接无参启动（FlClash 启动即开窗）。
func (s *FlClashService) OpenWindow() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起
		return ControlOutcome{Action: "starting", Message: "FlClash 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		s.engine.RestoreExternalWindow(s.engine.ExternalPIDs())
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 FlClash 窗口"}, nil

	case instance.StateRunning:
		s.engine.RestoreWindow()
		return ControlOutcome{Action: "opened", Message: "已唤起 FlClash 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 FlClash 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 FlClash 就绪超时（%d 秒）", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("FlClash %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 FlClash。
// external 状态不越权强杀（进程枚举拿到的 PID 非我方托管）：仅返回人性化指引。
func (s *FlClashService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 FlClash 窗口/托盘内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "FlClash 已退出"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *FlClashService) Shutdown() {
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
func (s *FlClashService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 FlClash 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versioncmp.Compare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *FlClashService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *FlClashService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *FlClashService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("FlClash", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *FlClashService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *FlClashService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
