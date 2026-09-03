package quicklook

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

	"hanxi/internal/modules/quicklook/instance"
	"hanxi/internal/modules/quicklook/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（单实例互斥体出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// QuickLookService 向前端暴露 QuickLook 版本管理与托管启停能力。
// 空格预览本身不内嵌：预览窗与插件查看器依赖上游 Manager 进程与全局键盘钩子，
// 样式设置全在 QuickLook 自有设置窗口完成（入口=托盘左键，上游无唤窗契约）。
type QuickLookService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *quicklookStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewQuickLookService(plat platform.Platform) *QuickLookService {
	paths := settings.GetPaths()
	svc := &QuickLookService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newQuicklookStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewQuickLookProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// normalizeVersion QuickLook 上游版本 tag 无 v 前缀（如 4.5.0）；
// 兼容前端/用户误带 v 前缀，统一剥离为裸号后比对与落库。
func normalizeVersion(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "v")
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 quicklook:instance-state。
func (s *QuickLookService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("quicklook instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("quicklook:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("quicklook", "QuickLook 实例异常", snap.Error, "/ext/quicklook")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *QuickLookService) activate() {
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
func (s *QuickLookService) ListReleases() ([]version.QuickLookRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *QuickLookService) ListInstalledVersions() ([]version.QuickLookVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 quicklook:version-download 推送进度。
func (s *QuickLookService) DownloadVersion(targetVersion string) (string, error) {
	targetVersion = normalizeVersion(targetVersion)

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	// 已安装则直接返回，避免重复下载（zip 约 117MB，尤其值得去重）
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(normalizeVersion(v.Version), targetVersion) {
				return "already-installed", nil
			}
		}
	}

	go func() {
		emit := func(p version.DownloadProgress) {
			slog.Debug("quicklook download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("quicklook:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("quicklook", "版本安装成功", fmt.Sprintf("QuickLook %s 已成功安装", p.Version), "/ext/quicklook")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("quicklook", "版本安装失败", fmt.Sprintf("QuickLook %s 安装失败: %v", targetVersion, err), "/ext/quicklook")
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
func (s *QuickLookService) RemoveVersion(targetVersion string) error {
	targetVersion = normalizeVersion(targetVersion)
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(normalizeVersion(snap.Version), targetVersion) {
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
func (s *QuickLookService) SetActiveVersion(targetVersion string) (string, error) {
	targetVersion = normalizeVersion(targetVersion)
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）。
func (s *QuickLookService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已解压的 QuickLook 便携目录（整套迁移）。
// 与 ccswitch 的单 exe 导入不同：QuickLook 便携目录含原生 DLL 与 QuickLook.Plugin 插件树，
// 且配置随 portable.lock 落此目录，故整套搬入版本隔离目录（连用户既有 .config 设置一并保留）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe/DLL 被独占，拷贝必然失败。
func (s *QuickLookService) ImportLocal(srcDir string) (version.QuickLookVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.QuickLookVersionInfo{}, fmt.Errorf("QuickLook 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开安装目录"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *QuickLookService) OpenDir(dir string) error {
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
func (s *QuickLookService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// StartQuickLook 启动编排中枢：
//   - stopped/failed：解析 active 版本无参启动 → 驻托盘 + 全局键盘钩子生效（资源管理器按空格即预览）；
//   - running/starting：自有实例幂等指引（重复拉起只会弹"已在运行"框，无唤窗语义）；
//   - external：不接管外部实例（互斥体探测拿不到 PID，且无信使可唤醒窗口）。
func (s *QuickLookService) StartQuickLook() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "QuickLook 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		return ControlOutcome{Action: "external-running", External: true,
			Message: "检测到外部自行启动的 QuickLook 实例，本控制台不接管；在资源管理器选中文件按空格即可预览，样式设置在其托盘菜单"}, nil

	case instance.StateRunning:
		return ControlOutcome{Action: "already-running",
			Message: fmt.Sprintf("QuickLook %s 已在运行（空格预览生效中），样式设置请左键点击系统托盘图标", snap.Version)}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 QuickLook 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 QuickLook 就绪超时（%d 秒），请确认便携目录完整且未被安全软件拦截", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("QuickLook %s 已启动：在资源管理器选中文件按空格即可预览，样式设置请左键点击系统托盘图标", v)}, nil
	}
}

// Quit 退出引擎托管的 QuickLook（优先命名管道 Quit 优雅退出，宽限后强杀兜底，
// 理由与零残渣论证见 instance 包注释）。
// external 状态不越权强杀（互斥体探测拿不到 PID）：仅返回人性化指引。
func (s *QuickLookService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 QuickLook 托盘图标菜单中退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "QuickLook 已退出"}, nil
}

// Reload 请求运行中的实例重载配置（命名管道 Reload，best-effort）。
func (s *QuickLookService) Reload() (string, error) {
	if err := s.engine.Reload(); err != nil {
		return "", err
	}
	return "已请求 QuickLook 重载配置", nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *QuickLookService) Shutdown() {
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
func (s *QuickLookService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 QuickLook 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// versionCompare 比较裸号版本号 X.Y.Z（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 4.10.0/4.9.0 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	pa := strings.Split(normalizeVersion(a), ".")
	pb := strings.Split(normalizeVersion(b), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		if errA != nil || errB != nil {
			return strings.Compare(a, b) // 非规范段退化为字典序（导入兜底目录等）
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
func (s *QuickLookService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *QuickLookService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *QuickLookService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *QuickLookService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
