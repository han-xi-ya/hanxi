package subnetdesk

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

	"hanxi/internal/modules/subnetdesk/instance"
	"hanxi/internal/modules/subnetdesk/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	// readyTimeout 冷启动等待内层进程树出现的 RPC 上限：
	// 首启需解压 ~25MB 负载（引擎侧 startGrace=60s），超时不代表失败，
	// 页面状态会经事件自动纠正为 running。
	readyTimeout = 45 * time.Second
	// watchInterval 外部便携实例感知轮询间隔
	watchInterval = 5 * time.Second
)

// SubnetDeskService 向前端暴露 SubnetDesk 版本管理与启停/唤窗能力。
// 远程桌面画面本身不内嵌（上游 GUI 完整，内嵌不可行）：
// 连接、局域网设置（用户名/密码/CIDR 白名单/端口 21118）均在 SubnetDesk 自有窗口操作。
type SubnetDeskService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *subnetdeskStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewSubnetDeskService(plat platform.Platform) *SubnetDeskService {
	paths := settings.GetPaths()
	svc := &SubnetDeskService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newSubnetDeskStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewSubnetDeskProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 subnetdesk:instance-state。
func (s *SubnetDeskService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("subnetdesk instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("subnetdesk:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("subnetdesk", "SubnetDesk 实例异常", snap.Error, "/ext/subnetdesk")
	}
}

// activate 启动后台外部实例感知：5s 轮询提取目录校正 external/stopped。
// （自有实例由引擎监督协程轮询进程树感知，不依赖本 watcher。）
//
// 与 ccswitch 的偏差：无空闲自动退出巡检——被控端长期驻留（藏托盘继续监听
// TCP 21118）正是远程桌面的常态形态，按空闲杀进程等于掐断托管。
func (s *SubnetDeskService) activate() {
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
func (s *SubnetDeskService) ListReleases() ([]version.SDRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *SubnetDeskService) ListInstalledVersions() ([]version.SDVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 subnetdesk:version-download 推送进度。
func (s *SubnetDeskService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("subnetdesk download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("subnetdesk:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("subnetdesk", "版本下载成功", fmt.Sprintf("SubnetDesk %s 已成功安装", p.Version), "/ext/subnetdesk")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("subnetdesk", "版本下载失败", fmt.Sprintf("SubnetDesk %s 下载失败: %v", targetVersion, err), "/ext/subnetdesk")
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
func (s *SubnetDeskService) RemoveVersion(targetVersion string) error {
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
func (s *SubnetDeskService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *SubnetDeskService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本机已有的 SubnetDesk 便携 exe（文件路径或所在目录均可）。
// 配置恒在 %APPDATA%\SubnetDesk 不受导入影响；仅迁移 exe。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 被独占，拷贝必然失败。
func (s *SubnetDeskService) ImportLocal(srcPath string) (version.SDVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal || snap.State == instance.StateStarting {
		return version.SDVersionInfo{}, fmt.Errorf("SubnetDesk 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcPath))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron 事故教训）；这里入参恒为目录，语义安全。
func (s *SubnetDeskService) OpenDir(dir string) error {
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
func (s *SubnetDeskService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢（SubnetDesk 无唤窗信使，二次拉起 = 新窗口实例）：
//   - external：EnumWindows 唤起外部便携实例窗口；驻留托盘无窗可唤时只给指引，不越权拉起；
//   - running：优先唤起自有窗口；窗口已销毁（藏托盘）则派生新实例开窗（同 Job 托管）；
//   - stopped/failed：解析 active 版本冷启动。
func (s *SubnetDeskService) OpenWindow() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting",
			Message: "SubnetDesk 正在启动（首次运行需解压内置负载，最长约 1 分钟），请稍候"}, nil

	case instance.StateExternal:
		if n := s.engine.RestoreExternalWindow(); n > 0 {
			return ControlOutcome{Action: "external-opened", External: true,
				Message: fmt.Sprintf("已唤起外部便携实例的 %d 个窗口", n)}, nil
		}
		return ControlOutcome{Action: "external", External: true,
			Message: "检测到外部便携实例驻留托盘（无窗口可唤）：请点击其托盘图标打开，或先退出该实例再由 Hanxi 托管"}, nil

	case instance.StateRunning:
		if n := s.engine.RestoreWindow(); n > 0 {
			return ControlOutcome{Action: "opened",
				Message: fmt.Sprintf("已唤起 SubnetDesk 窗口（%d 个）", n)}, nil
		}
		// 主窗已销毁、进程驻留托盘：上游无唤回通道，派生第二个托管实例开新窗
		if err := s.engine.SpawnWindow(); err != nil {
			return ControlOutcome{}, fmt.Errorf("重新打开窗口失败: %w", err)
		}
		return ControlOutcome{Action: "spawned-new",
			Message: "实例驻留托盘且窗口已销毁，已派生新窗口实例（同一 Job 托管）；旧实例仍在托盘中"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 SubnetDesk 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{Action: "starting",
				Message: fmt.Sprintf("SubnetDesk %s 仍在解压/启动中（首次启动较慢），页面稍后会自动就绪；若 1 分钟后仍未出现，请检查杀软是否拦截", v)}, nil
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("SubnetDesk %s 已启动（局域网被控：在其窗口 LAN 设置中启用并配置密码）", v)}, nil
	}
}

// Quit 退出引擎托管的 SubnetDesk（终止整个进程树，含被控监听端）。
// external 状态不越权强杀：仅返回人性化指引。
func (s *SubnetDeskService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的便携实例，不在 Hanxi 托管范围：请在其托盘菜单退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true,
		Message: "SubnetDesk 已退出（便携版无优雅退出通道，进程树整体终止——进行中的远程会话会断开）"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *SubnetDeskService) Shutdown() {
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
func (s *SubnetDeskService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 SubnetDesk 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
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
func (s *SubnetDeskService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *SubnetDeskService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *SubnetDeskService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("SubnetDesk", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *SubnetDeskService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *SubnetDeskService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
