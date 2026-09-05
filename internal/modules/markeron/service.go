package markeron

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/markeron/instance"
	"hanxi/internal/modules/markeron/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second       // 冷启动就绪上限（互斥体出现）
	settleDelay   = 800 * time.Millisecond // 互斥体就绪 → WM_COPYDATA 消息窗口注册完毕的缓冲，之后信使才可靠
	watchInterval = 5 * time.Second        // 外部实例感知轮询间隔
)

// MarkerOnService 向前端暴露 MarkerOn 版本管理与标注开关能力。
type MarkerOnService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *markeronStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	toggleMu   sync.Mutex // ToggleAnnotate/StopAnnotate 编排串行化
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewMarkerOnService(plat platform.Platform) *MarkerOnService {
	paths := settings.GetPaths()
	svc := &MarkerOnService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newMarkeronStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewMarkerProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 markeron:instance-state。
func (s *MarkerOnService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("markeron instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("markeron:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("markeron", "标注实例异常", snap.Error, "/ext/markeron")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *MarkerOnService) activate() {
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

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存）
func (s *MarkerOnService) ListReleases() ([]version.MarkerRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表
func (s *MarkerOnService) ListInstalledVersions() ([]version.MarkerVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 markeron:version-download 推送进度。
func (s *MarkerOnService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("markeron download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("markeron:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("markeron", "版本下载成功", fmt.Sprintf("MarkerOn %s 已成功安装", p.Version), "/ext/markeron")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("markeron", "版本下载失败", fmt.Sprintf("MarkerOn %s 下载失败: %v", targetVersion, err), "/ext/markeron")
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

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）
func (s *MarkerOnService) RemoveVersion(targetVersion string) error {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(strings.TrimPrefix(snap.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
		return fmt.Errorf("版本 %s 正在运行，请先停止", targetVersion)
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
func (s *MarkerOnService) SetActiveVersion(targetVersion string) (string, error) {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）
func (s *MarkerOnService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ---------- 标注开关 ----------

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）
func (s *MarkerOnService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// ToggleAnnotate 标注开关编排中枢：
//   - external：任一已装 exe 充当单实例信使，触发外部主实例 toggle_drawing；
//   - running：自有实例直接翻转（启动后极短时间内先补足消息窗口注册缓冲）；
//   - stopped/failed：仅启动到后台运行（不自动进入标注）——进入/退出标注交给下一次点击或 Ctrl+Shift+D。
func (s *MarkerOnService) ToggleAnnotate() (ToggleOutcome, error) {
	s.toggleMu.Lock()
	defer s.toggleMu.Unlock()

	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ToggleOutcome{}, err
		}
		if err := s.engine.Toggle(exe); err != nil {
			return ToggleOutcome{}, fmt.Errorf("切换标注失败: %w", err)
		}
		return ToggleOutcome{Outcome: "external-toggled", External: true,
			Message: "已向外部运行中的 MarkerOn 发出切换指令"}, nil

	case instance.StateRunning:
		// 刚启动就点开标时，补足 WM_COPYDATA 消息窗口注册缓冲，保证信使转发生效
		if d := s.engine.RunningDuration(); d < settleDelay {
			time.Sleep(settleDelay - d)
		}
		if err := s.engine.Toggle(s.engine.Exe()); err != nil {
			return ToggleOutcome{}, fmt.Errorf("切换标注失败: %w", err)
		}
		return ToggleOutcome{Outcome: "toggled", Drawing: !snap.Drawing,
			Message: "已切换桌面标注状态"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ToggleOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ToggleOutcome{}, fmt.Errorf("启动 MarkerOn 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ToggleOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ToggleOutcome{}, fmt.Errorf("等待 MarkerOn 就绪超时（%d 秒），请确认已安装 WebView2 Runtime", int(readyTimeout/time.Second))
		}
		// 仅启动到后台运行，不自动进入标注：标注态由再次点击开关或快捷键 Ctrl+Shift+D 决定
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ToggleOutcome{Outcome: "started", Drawing: false,
			Message: fmt.Sprintf("MarkerOn %s 已启动（后台运行，未进入标注）", v)}, nil
	}
}

// StopAnnotate 停止引擎托管的 MarkerOn。
// external 状态不越权强杀（互斥体探测拿不到 PID）：仅返回人性化指引。
func (s *MarkerOnService) StopAnnotate() (StopOutcome, error) {
	s.toggleMu.Lock()
	defer s.toggleMu.Unlock()

	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return StopOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 MarkerOn 系统托盘图标上退出"}, nil
	}
	if err := s.engine.Stop(); err != nil {
		return StopOutcome{}, err
	}
	return StopOutcome{Stopped: true, Message: "MarkerOn 已停止"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *MarkerOnService) Shutdown() {
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
func (s *MarkerOnService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 MarkerOn 版本，请先在版本管理下载")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（信使用途，与版本号无关）
func (s *MarkerOnService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 MarkerOn 版本，无法代为切换外部实例——请使用快捷键 Ctrl+Shift+D")
	}
	return installed[0].ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 2.10.0/2.9.4 这类多位数段有误，必须数值分段比较。
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
func (s *MarkerOnService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *MarkerOnService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *MarkerOnService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("MarkerOn 标注", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *MarkerOnService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *MarkerOnService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
