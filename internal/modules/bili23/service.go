package bili23

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

	"hanxi/internal/modules/bili23/instance"
	"hanxi/internal/modules/bili23/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 30 * time.Second // 冷启动就绪上限（单实例互斥体出现；Qt 首帧 + 网络栈预热）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// Service 向前端暴露 Bili23 Downloader 版本管理与托管启停能力。
// 下载操作本身不内嵌：打开 Bili23 自有窗口完成（其 GUI 已高度完整，纯托管决策
// 详见包注释）。本服务只做三件事：装对版本、起停受控、状态如实。
//
// 与 ccswitch service 的显著差异：**没有空闲自动退出**——下载器无法从外部感知
// 任务活跃度（无 CLI 状态通道），静默退出打断在途下载的代价远大于省内存的收益。
type Service struct {
	plat    platform.Platform
	manager *version.Manager
	store   *bili23Store
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewService(plat platform.Platform) *Service {
	paths := settings.GetPaths()
	svc := &Service{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newBili23Store(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewBili23Probe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 bili23:instance-state。
func (s *Service) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("bili23 instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("bili23:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("bili23", "Bili23 实例异常", snap.Error, "/ext/bili23")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *Service) activate() {
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
func (s *Service) ListReleases() ([]version.Bili23Release, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *Service) ListInstalledVersions() ([]version.Bili23VersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 bili23:version-download 推送进度。
func (s *Service) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("bili23 download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("bili23:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("bili23", "版本下载成功", fmt.Sprintf("Bili23 Downloader %s 已成功安装", p.Version), "/ext/bili23")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("bili23", "版本下载失败", fmt.Sprintf("Bili23 Downloader %s 下载失败: %v", targetVersion, err), "/ext/bili23")
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
func (s *Service) RemoveVersion(targetVersion string) error {
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
func (s *Service) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *Service) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的 Bili23 Downloader（Inno 安装版目录 / 手动解压的便携目录均可）。
// 整目录复制（~108MB）；用户配置恒在 %APPDATA%\Bili23 Downloader\ 不受导入影响。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *Service) ImportLocal(srcDir string) (version.Bili23VersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.Bili23VersionInfo{}, fmt.Errorf("Bili23 Downloader 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训）。这里入参恒为目录，语义安全。
func (s *Service) OpenDir(dir string) error {
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

// OpenConfigDir 打开 Bili23 的用户数据目录（%APPDATA%\Bili23 Downloader，
// 配置/任务库/日志所在）——纯托管下用户想看"数据在哪"的直达入口。只读导航，不改写。
func (s *Service) OpenConfigDir() error {
	dir, err := bili23UserDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("Bili23 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// bili23UserDir 上游数据目录约定（实证自 src/main.py：QStandardPaths.AppDataLocation
// 导入期取值 + 硬编码 "Bili23 Downloader" 子目录拼接）。
func bili23UserDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("无法定位 APPDATA 目录")
	}
	return filepath.Join(appData, "Bili23 Downloader"), nil
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *Service) GetStatus() (Status, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()
	return statusFrom(snap, s.engine.IsWindowVisible()), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：任一已装 exe 充当单实例信使，上游 QLocalServer 回调激活外部主窗口；
//   - running：自有实例直接信使唤窗（窗口收入托盘后同样经此唤回）；
//   - stopped/failed：解析 active 版本直接无参启动（Bili23 唯一启动语义即开窗）。
func (s *Service) OpenWindow() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "Bili23 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 Bili23 窗口"}, nil

	case instance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 Bili23 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 Bili23 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 Bili23 就绪超时（%d 秒），请确认系统为 Windows 10 1809 及以上", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("Bili23 %s 已启动", v)}, nil
	}
}

// Quit 尽力优雅退出引擎托管的 Bili23。结果如实分叉（引擎不代做决定、不强杀）：
//   - exited：进程已终结（"关闭窗口=退出"设置，或用户在询问对话框中选择了退出）；
//   - hidden：进程收入 Bili23 自身托盘（"关闭窗口=最小化"设置），仍受 Job 托管，
//     可唤窗，也可用「强制结束」终结；
//   - asked：Bili23 弹出询问对话框等待用户选择（默认设置），请到其窗口内完成决定。
//
// external 状态不越权强杀（互斥体探测拿不到 PID）：仅返回人性化指引。
func (s *Service) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 Bili23 窗口或其托盘菜单中退出"}, nil
	}

	res, err := s.engine.Quit()
	if err != nil {
		return QuitOutcome{}, err
	}
	switch res {
	case instance.QuitExited:
		return QuitOutcome{Stopped: true, Message: "Bili23 已退出"}, nil
	case instance.QuitHidden:
		return QuitOutcome{Stopped: false, Hidden: true,
			Message: "Bili23 按「关闭窗口=最小化到托盘」设置收入自身托盘，进程仍在本机运行。" +
				"可用「打开窗口」唤回、「强制结束」终结，或在其托盘菜单退出"}, nil
	default: // QuitWindowUp
		return QuitOutcome{Stopped: false, Asked: true,
			Message: "Bili23 已弹出退出询问对话框（「关闭窗口=总是询问」设置），请在其中选择退出或隐藏"}, nil
	}
}

// ForceStop 立即强杀自有实例（「强制结束」按钮）：跳过优雅收尾，在途下载中断——
// 上游断点续传 + SQLite WAL 保证下次启动可恢复。external 实例不受管辖。
func (s *Service) ForceStop() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，Hanxi 不对其强制执行"}, nil
	}
	if err := s.engine.Stop(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "Bili23 已被强制结束（在途下载已中断，下次启动可续传）"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
// 刻意走 Stop（强杀）而非 Quit（优雅）：应用退出通道不能阻塞等待上游下载线程收敛，
// 且用户若不希望退出被中断，可关闭"随 Hanxi 退出"开关（解除 Job 联动）。
func (s *Service) Shutdown() {
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
func (s *Service) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 Bili23 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（信使用途，与版本号无关——
// 单实例协议跨版本通用）。
func (s *Service) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 Bili23 版本，无法代为唤起外部实例窗口")
	}
	return installed[0].ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 2.15.0/2.9.0 这类多位数段有误，必须数值分段比较；
// 上游存在 2.00.7 前导零段，Atoi 天然兼容。
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
func (s *Service) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *Service) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *Service) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("Bili23 Downloader", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *Service) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *Service) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
