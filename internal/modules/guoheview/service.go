package guoheview

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/guoheview/instance"
	"hanxi/internal/modules/guoheview/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（可见窗口出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// GuoheViewService 向前端暴露果核看图版本管理与窗口唤起能力。
// 看图界面本身不内嵌：上游是原生极速渲染（自研解码内核 + 分块加载 + ICC 色彩
// 管理），内嵌重做毫无性价比，所有浏览操作在 GuoheView 自有窗口完成。
type GuoheViewService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *guoheviewStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewGuoheViewService(plat platform.Platform) *GuoheViewService {
	paths := settings.GetPaths()
	svc := &GuoheViewService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newGuoheviewStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewViewProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 guoheview:instance-state。
func (s *GuoheViewService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("guoheview instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("guoheview:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("guoheview", "果核看图实例异常", snap.Error, "/ext/guoheview")
	}
}

// activate 启动后台外部实例感知：5s 轮询进程名校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。
// 刻意无空闲自动退出：多实例看图器"进程活着 = 窗口开着 = 用户在看图"，
// 空闲退出不但无收益还会打断浏览——理由详见 instance 包注释。）
func (s *GuoheViewService) activate() {
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

// ListReleases 获取远程可用版本（上游只发布当前版本，至多 stable+beta 两条）。
func (s *GuoheViewService) ListReleases() ([]version.ViewRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *GuoheViewService) ListInstalledVersions() ([]version.ViewVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 guoheview:version-download 推送进度。
func (s *GuoheViewService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("guoheview download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("guoheview:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("guoheview", "版本安装成功", fmt.Sprintf("果核看图 %s 已成功安装", p.Version), "/ext/guoheview")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("guoheview", "版本安装失败", fmt.Sprintf("果核看图 %s 安装失败: %v", targetVersion, err), "/ext/guoheview")
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
func (s *GuoheViewService) RemoveVersion(targetVersion string) error {
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
func (s *GuoheViewService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *GuoheViewService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的果核看图便携目录（整套搬运，含 config.ini 设置）。
// 运行中的实例拒绝导入：Windows 下运行中的文件被独占，拷贝必然失败。
func (s *GuoheViewService) ImportLocal(srcDir string) (version.ViewVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning {
		return version.ViewVersionInfo{}, fmt.Errorf("托管实例正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *GuoheViewService) OpenDir(dir string) error {
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
func (s *GuoheViewService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢（多实例上游的正确契约，见 instance 包注释）：
//   - running：聚焦自有托管实例主窗口；窗口尚未出现的极窄竞态给出"启动中"提示；
//   - external：优先唤回用户已打开的看图窗口；无可聚焦窗口则另开独立窗口
//     （不进 Job、不随 Hanxi 退出——与用户双击图片的实例同等地位）；
//   - stopped/failed：解析 active 版本无参启动托管实例。
func (s *GuoheViewService) OpenWindow() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "果核看图正在启动中，请稍候"}, nil

	case instance.StateExternal:
		if s.engine.FocusExternal() {
			return ControlOutcome{Action: "external-focused", External: true,
				Message: "已唤回正在运行的果核看图窗口"}, nil
		}
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.LaunchDetachedWindow(exe); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "external-window", External: true,
			Message: "已另开独立看图窗口（不受 Hanxi 生命周期管理）"}, nil

	case instance.StateRunning:
		if !s.engine.Focus() {
			return ControlOutcome{Action: "starting", Message: "托管实例窗口尚未出现，请稍候片刻"}, nil
		}
		return ControlOutcome{Action: "focused", Message: "已唤起果核看图窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动果核看图失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待果核看图就绪超时（%d 秒），请在版本管理重新安装后重试", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("果核看图 %s 已启动", v)}, nil
	}
}

// Quit 优雅退出托管实例（WM_CLOSE 关窗即退 + 宽限 + Job 兜底，实证见 instance 包注释）。
// external 状态不越权强杀（用户自行打开的窗口归用户）：仅返回人性化指引。
func (s *GuoheViewService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是用户自行打开的看图窗口，请在其窗口内关闭（Hanxi 不代关外部实例）"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "果核看图已退出"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *GuoheViewService) Shutdown() {
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
func (s *GuoheViewService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何果核看图版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（另开独立窗口的信使载体，
// 与版本号无关）。
func (s *GuoheViewService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何果核看图版本，无法代为打开窗口")
	}
	return installed[0].ExePath, nil
}

// versionCompare 数值分段比较 vX.Y.Z.W 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 3.10.x/3.9.x 这类多位数段有误，必须逐段转数值。
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

// ---------- 联动开关与官网入口 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *GuoheViewService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *GuoheViewService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 官网地址（上游闭源无仓库页；前端展示与复制）。
func (s *GuoheViewService) RepositoryURL() (string, error) {
	return version.SiteURL(), nil
}

// OpenRepository 用默认浏览器打开官网页面。
func (s *GuoheViewService) OpenRepository() error {
	return s.plat.OpenURL(version.SiteURL())
}
