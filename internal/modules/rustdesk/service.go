package rustdesk

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

	"hanxi/internal/modules/rustdesk/instance"
	"hanxi/internal/modules/rustdesk/version"
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

// RustDeskService 向前端暴露 RustDesk 版本管理与启停/唤窗能力，双形态纳管：
//   - 便携版（隔离目录 packer exe）：全托管，JobObject 启停联动；
//   - 安装版（MSI 系统安装）：Hanxi 负责取包校验、发起上游安装向导、注册表
//     探测识别与客户端拉起/唤窗；Windows 服务不归 Hanxi 管辖（无人值守
//     的被控常驻正是它的价值），卸载引导至系统设置、不代执行。
//
// 远程桌面画面本身不内嵌（上游 GUI 完整，内嵌不可行）：
// 连接发起（输入对端 ID）、永久密码设置与自建服务器（rendezvous/relay）配置
// 均在 RustDesk 自有窗口操作。
type RustDeskService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *rustdeskStore
	engine  *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewRustDeskService(plat platform.Platform) *RustDeskService {
	paths := settings.GetPaths()
	svc := &RustDeskService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newRustDeskStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewRustDeskProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 rustdesk:instance-state。
func (s *RustDeskService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("rustdesk instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("rustdesk:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("rustdesk", "RustDesk 实例异常", snap.Error, "/ext/rustdesk")
	}
}

// activate 启动后台外部实例感知：5s 轮询提取目录校正 external/stopped。
// （自有实例由引擎监督协程轮询进程树感知，不依赖本 watcher。）
//
// 与 ccswitch 的偏差：无空闲自动退出巡检——被控端长期驻留（藏托盘继续经
// 21116/21117 收发）正是远程桌面的常态形态，按空闲杀进程等于掐断托管。
func (s *RustDeskService) activate() {
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
func (s *RustDeskService) ListReleases() ([]version.RDRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地可用版本列表（两形态合流）：隔离目录便携安装
// 按版本序在前，系统安装版（如探测命中）追加于尾部——安装版全局至一条
// （MSI MajorUpgrade 就地替换，真机实证），且不可按删目录方式卸载。
func (s *RustDeskService) ListInstalledVersions() ([]version.RDVersionInfo, error) {
	list, err := s.manager.ListInstalled()
	if err != nil {
		return nil, err
	}
	if si, ok := s.manager.SystemVersion(); ok {
		list = append(list, si)
	}
	return list, nil
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 rustdesk:version-download 推送进度。
func (s *RustDeskService) DownloadVersion(targetVersion string) (string, error) {
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
			slog.Debug("rustdesk download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("rustdesk:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("rustdesk", "版本下载成功", fmt.Sprintf("RustDesk %s 已成功安装", p.Version), "/ext/rustdesk")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("rustdesk", "版本下载失败", fmt.Sprintf("RustDesk %s 下载失败: %v", targetVersion, err), "/ext/rustdesk")
			return
		}
		// 未设使用版本时自动把刚下载完的版本设为使用版本：
		// 首个版本下载完成后无需再手动点一下设置（与 snipaste 既有行为对齐）。
		if v, _ := s.store.GetActive(); v == "" {
			_ = s.store.SetActive(targetVersion, version.FormPortable)
		}
	}()

	return "started", nil
}

// InstallVersion 下载并安装指定版本的安装版（MSI，后台编排）：进度经事件
// rustdesk:version-download（kind=installer）推送；安装本体是 Windows Installer
// 前台向导（含 UAC），全程由用户操作，Hanxi 只等待结果并以注册表探测做事实核验。
// 与 DownloadVersion 共用下载锁：Windows Installer 同一时刻只允许一个装机任务。
func (s *RustDeskService) InstallVersion(targetVersion string) (string, error) {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	if si, ok := s.manager.SystemVersion(); ok && si.Version == targetVersion {
		return "already-installed", nil
	}

	go func() {
		emit := func(stage, msg string) {
			p := version.DownloadProgress{Version: targetVersion, Kind: "installer", Stage: stage, Message: msg}
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("rustdesk:version-download", p)
			}
		}
		if err := s.manager.DownloadInstaller(targetVersion, func(p version.DownloadProgress) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("rustdesk:version-download", p)
			}
		}); err != nil {
			emit("error", err.Error())
			notify.Error("rustdesk", "安装版下载失败", fmt.Sprintf("RustDesk %s 安装包下载失败: %v", targetVersion, err), "/ext/rustdesk")
			return
		}
		emit("install", "安装向导已弹出：请在 Windows Installer 界面（含 UAC 授权）中完成安装")
		if err := s.manager.Install(targetVersion); err != nil {
			emit("error", err.Error())
			notify.Error("rustdesk", "安装版安装失败", fmt.Sprintf("RustDesk %s 安装未完成: %v", targetVersion, err), "/ext/rustdesk")
			return
		}
		si, _ := s.manager.SystemVersion()
		emit("done", fmt.Sprintf("RustDesk 安装版 %s 已就绪", si.Version))
		notify.Success("rustdesk", "安装版安装成功", fmt.Sprintf("RustDesk 安装版 %s 已就绪（含系统服务，可无人值守被控）", si.Version), "/ext/rustdesk")
	}()

	return "started", nil
}

// RemoveVersion 卸载指定便携版本（删除隔离目录；正在运行的版本拒绝卸载）。
// 仅作用于隔离目录：系统安装版不走本入口（卸载引导至系统设置，见 OpenUninstallSettings）。
func (s *RustDeskService) RemoveVersion(targetVersion string) error {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(strings.TrimPrefix(snap.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	// 卸载的是当前设定的便携版本则清空，下次冷启动自动回退最新已装
	if v, f := s.store.GetActive(); v == targetVersion && f == version.FormPortable {
		_ = s.store.SetActive("", "")
	}
	return nil
}

// SetActiveVersion 设定使用版本与形态（先校验可用，再持久化）。
// form：portable = 隔离目录版本；installed = 系统安装版（版本须与探测一致）。
func (s *RustDeskService) SetActiveVersion(targetVersion string, form string) (string, error) {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	form = strings.TrimSpace(form)
	if form == "" {
		form = version.FormPortable // 兼容旧调用：缺省便携
	}
	switch form {
	case version.FormPortable:
		if _, err := s.manager.ResolveExe(targetVersion); err != nil {
			return "", err
		}
	case version.FormInstalled:
		if si, ok := s.manager.SystemVersion(); !ok || si.Version != targetVersion {
			return "", fmt.Errorf("系统安装版当前不是 %s（可能已被升级或卸载），请刷新后重选", targetVersion)
		}
	default:
		return "", fmt.Errorf("未知版本形态: %s", form)
	}
	if err := s.store.SetActive(targetVersion, form); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动回退）。
func (s *RustDeskService) GetActiveVersion() (string, error) {
	v, _ := s.store.GetActive()
	return v, nil
}

// GetActiveForm 返回当前设定形态（portable / installed；未指定时为空）。
func (s *RustDeskService) GetActiveForm() (string, error) {
	_, f := s.store.GetActive()
	return f, nil
}

// ImportLocal 导入本机已有的 RustDesk 便携 exe（文件路径或所在目录均可）。
// 配置恒在 %APPDATA%\RustDesk 不受导入影响；仅迁移 exe。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 被独占，拷贝必然失败。
func (s *RustDeskService) ImportLocal(srcPath string) (version.RDVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal || snap.State == instance.StateStarting {
		return version.RDVersionInfo{}, fmt.Errorf("RustDesk 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcPath))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron 事故教训）；这里入参恒为目录，语义安全。
func (s *RustDeskService) OpenDir(dir string) error {
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

// OpenConfigDir 打开 RustDesk 的用户数据目录（身份密钥、地址簿与设置所在）——纯托管下用户想看"数据在哪"的直达入口。只读导航，不改写。
func (s *RustDeskService) OpenConfigDir() error {
	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("RustDesk 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// userConfigDir portable 形态默认写 %APPDATA%\RustDesk（module.go 包注释实证；本机实存印证）。
func userConfigDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("无法定位 APPDATA 目录")
	}
	return filepath.Join(appData, "RustDesk"), nil
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *RustDeskService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢（RustDesk 无唤窗信使，二次拉起 = 新窗口实例）：
//   - external：EnumWindows 唤起外部便携实例窗口；驻留托盘无窗可唤时只给指引，不越权拉起；
//   - running：优先唤起自有窗口；窗口已销毁（藏托盘）则派生新实例开窗（同 Job 托管）；
//   - stopped/failed：解析 active 版本冷启动。
func (s *RustDeskService) OpenWindow() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting",
			Message: "RustDesk 正在启动（首次运行需解压内置负载，最长约 1 分钟），请稍候"}, nil

	case instance.StateExternal:
		if n := s.engine.RestoreExternalWindow(); n > 0 {
			return ControlOutcome{Action: "external-opened", External: true,
				Message: fmt.Sprintf("已唤起外部实例的 %d 个窗口", n)}, nil
		}
		return ControlOutcome{Action: "external", External: true,
			Message: "检测到外部自行启动的实例驻留托盘（无窗口可唤）：请点击其托盘图标打开，或先退出该实例再由 Hanxi 托管"}, nil

	case instance.StateRunning:
		if n := s.engine.RestoreWindow(); n > 0 {
			return ControlOutcome{Action: "opened",
				Message: fmt.Sprintf("已唤起 RustDesk 窗口（%d 个）", n)}, nil
		}
		// 主窗已销毁、进程驻留托盘：上游无唤回通道，派生第二个托管实例开新窗
		if err := s.engine.SpawnWindow(); err != nil {
			return ControlOutcome{}, fmt.Errorf("重新打开窗口失败: %w", err)
		}
		return ControlOutcome{Action: "spawned-new",
			Message: "实例驻留托盘且窗口已销毁，已派生新窗口实例（同一 Job 托管）；旧实例仍在托盘中"}, nil

	default:
		v, exe, form, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Form: form, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 RustDesk 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			if form == version.FormInstalled {
				return ControlOutcome{Action: "starting",
					Message: fmt.Sprintf("RustDesk 安装版 %s 启动中，页面稍后会自动就绪；若长时间未出现请检查杀软是否拦截", v)}, nil
			}
			return ControlOutcome{Action: "starting",
				Message: fmt.Sprintf("RustDesk %s 仍在解压/启动中（首次启动较慢），页面稍后会自动就绪；若 1 分钟后仍未出现，请检查杀软是否拦截", v)}, nil
		}
		if active, _ := s.store.GetActive(); active == "" {
			_ = s.store.SetActive(v, form) // 首次冷启动将实际采用的版本与形态回写
		}
		if form == version.FormInstalled {
			return ControlOutcome{Action: "started",
				Message: fmt.Sprintf("RustDesk 安装版 %s 已启动（系统服务在位时，客户端退出后仍可持续无人值守被控）", v)}, nil
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("RustDesk %s 已启动（被控：保持本窗口或托盘驻留即可经设备 ID 被接入；默认走官方公共信令，可在设置中改用自建服务器）", v)}, nil
	}
}

// Quit 退出引擎托管的 RustDesk（终止整个进程树，含被控监听端）。
// external 状态不越权强杀：仅返回人性化指引。
func (s *RustDeskService) Quit() (QuitOutcome, error) {
	snap := s.engine.Snapshot()
	if snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，不在 Hanxi 托管范围：请在其托盘菜单退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	if snap.Form == version.FormInstalled {
		return QuitOutcome{Stopped: true,
			Message: "安装版客户端已退出（系统服务保持运行——无人值守被控不受影响；如需彻底停用服务请到系统服务管理）"}, nil
	}
	return QuitOutcome{Stopped: true,
		Message: "RustDesk 已退出（便携版无优雅退出通道，进程树整体终止——进行中的远程会话会断开）"}, nil
}

// Shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *RustDeskService) Shutdown() {
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

// resolveActiveVersion 解析当前应使用的版本（版本号/可执行路径/形态）：
// activeVersion+activeForm 优先，失效（便携目录被删 / 安装版被卸载或升级换版）
// 清空自愈；未设定时回退最新已装便携版，无任何便携安装但存在系统安装版时也
// 直接采用安装版（冷启动诚实使用现状）。
func (s *RustDeskService) resolveActiveVersion() (string, string, string, error) {
	active, form := s.store.GetActive()
	if active != "" {
		switch form {
		case version.FormInstalled:
			if si, ok := s.manager.SystemVersion(); ok && si.Version == active {
				return active, si.ExePath, version.FormInstalled, nil
			}
		default: // portable（含 form 缺失的旧配置）
			if exe, err := s.manager.ResolveExe(active); err == nil {
				return active, exe, version.FormPortable, nil
			}
		}
		_ = s.store.SetActive("", "") // 已设定的版本被卸载/损坏：清空自愈，回退最新已装
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", "", err
	}
	if len(installed) > 0 {
		sort.Slice(installed, func(i, j int) bool {
			return versionCompare(installed[i].Version, installed[j].Version) > 0
		})
		latest := installed[0]
		return latest.Version, latest.ExePath, version.FormPortable, nil
	}
	if si, ok := s.manager.SystemVersion(); ok {
		return si.Version, si.ExePath, version.FormInstalled, nil
	}
	return "", "", "", fmt.Errorf("尚未安装任何 RustDesk 版本，请先在版本管理下载便携版或安装系统版")
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
func (s *RustDeskService) GetFollowOnExit() (bool, error) {
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *RustDeskService) SetFollowOnExit(b bool) error {
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *RustDeskService) CreateDesktopShortcut() error {
	_, exe, _, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("RustDesk", exe, filepath.Dir(exe))
}

// OpenUninstallSettings 打开系统「安装的应用」设置页，引导用户卸载系统安装版。
// 刻意不代执行 msiexec /x：卸载系统级软件（连带移除服务）属用户决策的高危
// 操作，交还 Windows 原生入口（遵循项目敏感操作确认守则）。
func (s *RustDeskService) OpenUninstallSettings() error {
	return s.plat.OpenURL("ms-settings:appsfeatures")
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *RustDeskService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *RustDeskService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}
