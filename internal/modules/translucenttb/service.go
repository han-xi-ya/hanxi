package translucenttb

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/translucenttb/instance"
	"hanxi/internal/modules/translucenttb/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（单实例互斥体出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// TranslucentTBService 向前端暴露 TranslucentTB 版本管理与托管启停能力。
// 任务栏透明样式设置不内嵌：上游 UI 就是系统托盘 XAML 飞控（无主窗口可唤），
// Hanxi 侧提供版本管理、启停、状态重设与 settings.json 所在目录直达。
// 本模块禁用空闲自动退出（常驻特效工具：退出 = 任务栏特效消失，与托管诉求正相反）。
type TranslucentTBService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *translucenttbStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewTranslucentTBService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewTranslucentTBService(plat platform.Platform) *TranslucentTBService {
	paths := settings.GetPaths()
	svc := &TranslucentTBService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newTranslucentTBStore(paths.StateDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewTBProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 translucenttb:instance-state。
func (s *TranslucentTBService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("translucenttb instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("translucenttb:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("translucenttb", "TranslucentTB 实例异常", snap.Error, "/ext/translucenttb")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *TranslucentTBService) activate() {
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
func (s *TranslucentTBService) ListReleases() ([]version.TBRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *TranslucentTBService) ListInstalledVersions() ([]version.TBVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 translucenttb:version-download 推送进度。
func (s *TranslucentTBService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("translucenttb download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("translucenttb:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("translucenttb", "版本下载成功", fmt.Sprintf("TranslucentTB %s 已成功安装", p.Version), "/ext/translucenttb")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("translucenttb", "版本下载失败", fmt.Sprintf("TranslucentTB %s 下载失败: %v", targetVersion, err), "/ext/translucenttb")
			return
		}
		// 未设使用版本时自动把刚下载完的版本设为使用版本：
		// 首个版本下载完成后无需再手动点一下设置（与 ccswitch 既有行为对齐）。
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(targetVersion)
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
// 注意：TranslucentTB 的 settings.json 就在版本目录内，卸载连同用户配置一起删除
// （前端卸载确认框如实预告）。
func (s *TranslucentTBService) RemoveVersion(targetVersion string) error {
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
func (s *TranslucentTBService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *TranslucentTBService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已安装的 TranslucentTB 便携版（settings.json 在 exe 同目录，
// 整套随目录迁移，与 ccswitch 的单 exe 导入不同）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *TranslucentTBService) ImportLocal(srcDir string) (version.TBVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.TBVersionInfo{}, fmt.Errorf("TranslucentTB 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// Start 冷启动编排中枢：
//   - external：不越权拉起第二个实例（上游单实例协议会让它信使化自退并弹
//     "已在运行"气泡，且托管归属混乱），仅如实告知；
//   - running：幂等告知；
//   - stopped/failed：解析 active 版本无参启动。首启会弹上游欢迎授权窗口，
//     互斥体先于该 UI 创建，WaitReady 不受阻塞。
func (s *TranslucentTBService) Start() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "TranslucentTB 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		return ControlOutcome{Action: "external-detected", External: true,
			Message: "检测到外部自行启动的 TranslucentTB 实例，已在运行，无需重复启动"}, nil

	case instance.StateRunning:
		return ControlOutcome{Action: "already-running",
			Message: fmt.Sprintf("TranslucentTB %s 已在运行（Hanxi 托管）", snap.Version)}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 TranslucentTB 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 TranslucentTB 就绪超时（%d 秒），请确认系统为 Windows 11 且已安装 WinUI 2.8 / VCLibs 框架包", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("TranslucentTB %s 已启动，任务栏透明样式在系统托盘图标菜单中设置", v)}, nil
	}
}

// ResetState 重设任务栏动态状态（等价上游托盘菜单 "Reset dynamic state"）：
// 经单实例互斥体协议拉起状态信使，运行实例收到 TTB_NewInstanceStarted 后
// 重放当前配置到任务栏。任务栏外观被 explorer 重启/其它工具改动弄花时的修复通道。
//   - running/external：可发信使；
//   - starting：前序启动未完，提示稍候；
//   - stopped/failed：无实例可通知——拉起只会冷启动新实例，语义不是"重设"，明确报错。
func (s *TranslucentTBService) ResetState() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "TranslucentTB 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.ResetState(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("重设状态失败: %w", err)
		}
		return ControlOutcome{Action: "external-reset", External: true,
			Message: "已向外部实例发送任务栏状态重设信号"}, nil

	case instance.StateRunning:
		if err := s.engine.ResetState(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("重设状态失败: %w", err)
		}
		return ControlOutcome{Action: "reset-sent",
			Message: "已向 TranslucentTB 发送任务栏状态重设信号"}, nil

	default:
		return ControlOutcome{}, fmt.Errorf("TranslucentTB 未在运行，请先启动再重设状态")
	}
}

// Quit 退出引擎托管的 TranslucentTB（WM_CLOSE 优雅：保存 settings.json 后退出）。
// external 状态不越权强杀（互斥体探测拿不到 PID）：仅返回人性化指引。
func (s *TranslucentTBService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 TranslucentTB 托盘菜单中退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "TranslucentTB 已退出，任务栏已还原默认外观"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例（仅在联动开启时）。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject 联动口径约束。
func (s *TranslucentTBService) Shutdown() {
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

// ---------- 目录与仓库辅助 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe <dir> 直启；
// 刻意不走 explorer.exe <file> 的"执行"语义（markeron「打开安装目录」按钮的事故教训）。
func (s *TranslucentTBService) OpenDir(dir string) error {
	return windows.RevealDir(dir)
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *TranslucentTBService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// ---------- 版本解析 ----------

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，未设定/已失效回退最新已装。
func (s *TranslucentTBService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 TranslucentTB 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（信使用途，与版本号无关）。
func (s *TranslucentTBService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 TranslucentTB 版本，无法代为发送状态信号")
	}
	return installed[0].ExePath, nil
}

// versionCompare 比较 YYYY.N 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 2026.10/2026.2 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（裸 YYYY.N 版本号，无 v 前缀可剥）。
	return versioncmp.Compare(a, b)
}

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *TranslucentTBService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *TranslucentTBService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *TranslucentTBService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *TranslucentTBService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
