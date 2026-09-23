package vscode

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/vscode/instance"
	"hanxi/internal/modules/vscode/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 30 * time.Second // 冷启动就绪上限（Electron 主进程 + 首窗，慢机留足）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	formPortable  = string(version.FormPortable)
	formInstaller = string(version.FormInstaller)
)

// VSCodeService 向前端暴露 VS Code 双形态托管：便携版版本管理 + 安装版
// 感知/静默安装升级 + 两形态独立启停唤窗。
// 编辑器操作在 VS Code 自有窗口内完成（其界面即产品，内嵌无意义）；
// 不设空闲自动退出（关窗即退、窗口开着 = 用户正在编辑，强退反需求）。
//
// 所有业务 RPC 方法（含双形态全部通道）经 holder.Enter() 接入统一调用门
// （Wave 3）：未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type VSCodeService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *vscodeStore
	holder  *extapi.LeaseHolder

	portableEngine  *instance.Engine
	installerEngine *instance.Engine

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载/安装
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewVSCodeService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewVSCodeService(plat platform.Platform, holder *extapi.LeaseHolder) *VSCodeService {
	paths := settings.GetPaths()
	svc := &VSCodeService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newVSCodeStore(paths.StateDir()),
		holder:  holder,
	}
	svc.portableEngine = instance.NewEngine(plat.Job(),
		instance.NewPortableProbe(paths.VersionsDir()),
		instance.Callbacks{OnState: func(snap instance.Snapshot) { svc.emitInstanceState(formPortable, snap) }})
	svc.installerEngine = instance.NewEngine(plat.Job(),
		instance.NewInstallerProbe(),
		instance.Callbacks{OnState: func(snap instance.Snapshot) { svc.emitInstanceState(formInstaller, snap) }})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 vscode:instance-state（Form 由 service 权威标注）。
func (s *VSCodeService) emitInstanceState(form string, snap instance.Snapshot) {
	snap.Form = form
	slog.Debug("vscode instance state", "form", form, "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("vscode:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("vscode", "VS Code 实例异常", labelForm(form)+snap.Error, "/ext/vscode")
	}
}

// activate 启动后台外部实例感知：5s 轮询两引擎探测通道校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *VSCodeService) activate() {
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
				s.portableEngine.RefreshExternal()
				s.installerEngine.RefreshExternal()
			}
		}
	}()
}

// ---------- 版本管理 ----------

// ListRemoteVersions 获取指定形态（portable/installer）的远程可用版本（官方端点，10 分钟缓存）。
func (s *VSCodeService) ListRemoteVersions(form string) ([]version.Release, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote(version.Form(strings.TrimSpace(form)))
}

// ListInstalledVersions 获取本地已安装便携版列表。
func (s *VSCodeService) ListInstalledVersions() ([]version.VersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// GetInstalledApp 探测本机安装版 VS Code（HKCU 注册表；含用户自行安装的）。
func (s *VSCodeService) GetInstalledApp() (version.InstalledInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.InstalledInfo{}, gateErr
	}
	defer release()
	return version.DetectInstalled(), nil
}

// vscodePortableInstallSteps / vscodeInstallerInstallSteps 托管资产事务的 journal
// 步骤词汇（Wave 4）：便携 zip 形态走 download→unpack→place（verify 由内核 Fetch
// 折进 download 步内完成，模块进度词表不单独可见，如实不造幻影步骤）；安装版形态
// Inno 交互安装留模块 bespoke，事务只记 download→install 两步。
var (
	vscodePortableInstallSteps  = []string{"download", "unpack", "place"}
	vscodeInstallerInstallSteps = []string{"download", "install"}
)

// DownloadVersion 下载并安装便携版：立即返回，全程经事件 vscode:version-download 推送进度。
// 安装版（form=installer）通道 = 下载 + Inno 静默安装/升级，复用同一进度事件。
// confirm 是安装版确认闸回执：检测到运行中实例时首次调用返回 confirm-required，
// 前端全局确认框放行后带 confirm=true 重入（不带参数会永远被拦成死锁）。
//
// 同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链追加版本或
// 升级安装版，managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段
// 迁移逐步 Advance，收口经观察面 Handle 自动落账并广播 operation:changed
// （与既有模块事件双通道并行，Wave 4-B 接线，ccswitch 同构）。
func (s *VSCodeService) DownloadVersion(targetVersion string, form string, confirm bool) (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	f := version.Form(strings.TrimSpace(strings.ToLower(form)))
	if f != version.FormInstaller {
		f = version.FormPortable
	}

	// 安装版通道 = 静默升级语义：Inno CloseApplications=force 会强关运行中的
	// 安装版实例（含用户日常使用、可能有未保存内容的），必须显式确认后放行。
	if f == version.FormInstaller && !confirm {
		if snap := s.installerEngine.Snapshot(); snap.State == instance.StateRunning ||
			snap.State == instance.StateStarting || instance.InstallerInstanceRunning() {
			return ControlOutcome{Action: "confirm-required",
				Message: "检测到正在运行的 VS Code（可能包含您日常使用的窗口）：静默升级会强制关闭全部实例，未保存内容有 hot exit 备份兜底。请保存工作后确认继续，或先自行退出再安装。"}, nil
		}
	}

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	opKind := extapi.OpInstall
	steps := vscodePortableInstallSteps
	// 便携版已安装则直接返回，避免重复下载（安装版恒可重装=升级语义，不做此拦截）
	if f == version.FormPortable {
		installed, err := s.manager.ListInstalled()
		if err == nil {
			for _, v := range installed {
				if strings.EqualFold(v.Version, targetVersion) {
					return ControlOutcome{Action: "already-installed", Message: fmt.Sprintf("VS Code %s 便携版已安装", targetVersion)}, nil
				}
			}
		}
		if err == nil && len(installed) > 0 {
			opKind = extapi.OpUpdate // 向已托管工具链追加版本 = update
		}
	} else {
		steps = vscodeInstallerInstallSteps
		if version.DetectInstalled().Installed {
			opKind = extapi.OpUpdate // 静默升级既有安装
		}
	}
	txnID := uuid.NewString()

	go func() {
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("vscode:version-download", version.DownloadProgress{
					Version: targetVersion, Form: string(f), Stage: "error", Message: lerr.Error()})
			}
			notify.Error("vscode", "VS Code 安装失败", fmt.Sprintf("VS Code %s（%s）后台租约开启失败: %v", targetVersion, labelForm(string(f)), lerr), "/ext/vscode")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, steps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("vscode:version-download", version.DownloadProgress{
					Version: targetVersion, Form: string(f), Stage: "error", Message: terr.Error()})
			}
			notify.Error("vscode", "VS Code 安装失败", fmt.Sprintf("VS Code %s（%s）事务开启失败: %v", targetVersion, labelForm(string(f)), terr), "/ext/vscode")
			return
		}
		stepIdx := -1
		var stepErr error
		defer func() {
			if txn.JournalFailed() {
				msg := "journal 事务步进失败"
				if stepErr != nil {
					msg = stepErr.Error()
				}
				txn.Fail("journal-degraded", msg)
			} else if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
			}
			txn.Close()
		}()
		emit := func(p version.DownloadProgress) {
			slog.Debug("vscode download progress", "version", p.Version, "form", p.Form, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("vscode:version-download", p)
			}
			// journal 只在阶段迁移处落盘（§8.2 每步迁移即持久化）；
			// 下载分块进度仅进观察面内存投影，不产生 fsync 风暴；
			// verify 由内核折进 download 步、不单独映射，error/done 收口时统一落账
			switch p.Stage {
			case "downloading":
				if stepIdx < 0 {
					stepIdx = 0
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
				txn.Progress(p.Done, p.Total)
			case "extract", "install":
				if stepIdx < 1 {
					stepIdx = 1
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
			}
			if p.Stage == "done" {
				notify.Success("vscode", "VS Code 安装成功", fmt.Sprintf("VS Code %s（%s）已就绪", p.Version, labelForm(p.Form)), "/ext/vscode")
			}
		}
		if err := s.manager.DownloadContext(txn.Context(), txnID, targetVersion, f, emit); err != nil {
			txn.Fail("asset-install-failed", err.Error())
			emit(version.DownloadProgress{Version: targetVersion, Form: string(f), Stage: "error", Message: err.Error()})
			notify.Error("vscode", "VS Code 安装失败", fmt.Sprintf("VS Code %s（%s）失败: %v", targetVersion, labelForm(string(f)), err), "/ext/vscode")
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Form: string(f), Stage: "error", Message: err.Error()})
			notify.Error("vscode", "VS Code 安装失败", fmt.Sprintf("VS Code %s（%s）事务收口失败: %v", targetVersion, labelForm(string(f)), err), "/ext/vscode")
			return
		}
		// 便携版：未设定使用版本时自动把刚下载完的版本设为使用版本
		if f == version.FormPortable && s.store.GetActive() == "" {
			_ = s.store.SetActive(targetVersion)
		}
	}()

	return ControlOutcome{Action: "started",
		Message: fmt.Sprintf("已开始下载 VS Code %s（%s）", targetVersion, labelForm(string(f)))}, nil
}

// RemoveVersion 卸载指定便携版（正在运行的版本拒绝卸载）。
func (s *VSCodeService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if snap := s.portableEngine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(snap.Version, targetVersion) {
		return fmt.Errorf("便携版 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("") // 卸载的是设定版本则清空，冷启动自动回退最新已装
	}
	return nil
}

// SetActiveVersion 设定便携版使用版本（先校验已安装，再持久化）。
func (s *VSCodeService) SetActiveVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回便携版设定版本（空字符串 = 未指定，冷启动自动用最新已装）。
func (s *VSCodeService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地便携版 VS Code 目录（整套迁移，data\ 除外）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 被独占，且语义易混。
func (s *VSCodeService) ImportLocal(srcDir string) (version.VersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.VersionInfo{}, gateErr
	}
	defer release()
	if p := s.portableEngine.Snapshot(); p.State == instance.StateRunning || p.State == instance.StateExternal {
		return version.VersionInfo{}, fmt.Errorf("便携版实例正在运行，请先退出再导入")
	}
	if i := s.installerEngine.Snapshot(); i.State == instance.StateRunning || i.State == instance.StateExternal {
		return version.VersionInfo{}, fmt.Errorf("安装版实例正在运行，与导入无关但目录可能被占用，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.VersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// engineFor 按形态取引擎（未知形态归一为便携版）。
func (s *VSCodeService) engineFor(form string) *instance.Engine {
	if strings.EqualFold(strings.TrimSpace(form), formInstaller) {
		return s.installerEngine
	}
	return s.portableEngine
}

// OpenWindow 窗口唤起编排中枢（form = portable/installer）：
//   - external：信使二次拉起，单实例协议转发唤/开既有实例窗口；
//   - running：自有实例直接信使唤窗；
//   - stopped/failed：解析当前版本无参启动。
func (s *VSCodeService) OpenWindow(form string) (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	isInstaller := strings.EqualFold(strings.TrimSpace(form), formInstaller)
	engine := s.engineFor(form)
	engine.RefreshExternal()
	snap := engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "VS Code 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.resolveExeAny(isInstaller)
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 VS Code 窗口"}, nil

	case instance.StateRunning:
		if _, err := engine.OpenWindow(engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 VS Code 窗口"}, nil

	default:
		ver, exe, err := s.resolveStartTarget(isInstaller)
		if err != nil {
			return ControlOutcome{}, err
		}
		f := formPortable
		if isInstaller {
			f = formInstaller
		}
		if err := engine.Start(instance.StartOptions{Version: ver, Form: f, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 VS Code 失败: %w", err)
		}
		if !engine.WaitReady(readyTimeout) {
			if cur := engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 VS Code 就绪超时（%d 秒），请检查杀软是否拦截或文件是否完整", int(readyTimeout/time.Second))
		}
		if !isInstaller && s.store.GetActive() == "" {
			_ = s.store.SetActive(ver) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("VS Code %s（%s）已启动", ver, labelForm(f))}, nil
	}
}

// OpenPreferredWindow 托盘快捷入口：优先便携版（托管隔离通道），
// 无已装便携版时回落安装版；两者皆无则引导先下载。
func (s *VSCodeService) OpenPreferredWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	if installed, err := s.manager.ListInstalled(); err == nil && len(installed) > 0 {
		return s.OpenWindow(formPortable)
	}
	if version.DetectInstalled().Installed {
		return s.OpenWindow(formInstaller)
	}
	return ControlOutcome{}, fmt.Errorf("尚未托管任何 VS Code（便携版未下载且本机无安装版），请先到版本管理下载")
}

// Quit 退出引擎托管的 VS Code（form = portable/installer）。
// external 状态不越权强杀（安装版互斥体探测拿不到 PID；便携版可能是用户日常实例）：
// 仅返回人性化指引。
func (s *VSCodeService) Quit(form string) (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	engine := s.engineFor(form)
	if snap := engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 VS Code 窗口内退出（安装版与您的日常使用同实例组，托管不越权终止）"}, nil
	}
	if err := engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "VS Code 已退出"}, nil
}

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *VSCodeService) Shutdown() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.shutdown()
	return nil
}

// shutdown 装配布线：Go 直调路径，不得依赖运行态（见 ADR-0001 Wave 3 注记）。
// 模块停用/应用退出：停后台轮询 + 按开关联动终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *VSCodeService) shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	if s.store.GetFollowOnExit() {
		_ = s.portableEngine.Stop()  // 联动开启才杀；关闭则完全不影响工具（Job 已解除 kill-on-close）
		_ = s.installerEngine.Stop() // 两形态各自独立
	}
}

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe <dir> 直启；
// 刻意不走 explorer.exe <file> 的"执行"语义（markeron「打开安装目录」按钮的事故教训）。
func (s *VSCodeService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// GetStatus 返回两引擎快照 + 安装版注册表探测 + 开关（先各做一次静止态外部校正）。
func (s *VSCodeService) GetStatus() (Status, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Status{}, gateErr
	}
	defer release()
	s.portableEngine.RefreshExternal()
	s.installerEngine.RefreshExternal()
	ps := s.portableEngine.Snapshot()
	ps.Form = formPortable
	is := s.installerEngine.Snapshot()
	is.Form = formInstaller
	return Status{
		Portable:     ps,
		Installer:    is,
		Installed:    version.DetectInstalled(),
		FollowOnExit: s.store.GetFollowOnExit(),
	}, nil
}

// ---------- 版本解析 ----------

// resolveStartTarget 冷启动目标（exe 及其版本标签）：
//   - 安装版：注册表探测，未安装报错引导先安装或改用便携版；
//   - 便携版：activeVersion 优先，未设定/失效回退最新已装（并自愈清空）。
func (s *VSCodeService) resolveStartTarget(isInstaller bool) (string, string, error) {
	if isInstaller {
		info := version.DetectInstalled()
		if !info.Installed {
			return "", "", fmt.Errorf("本机没有安装版 VS Code：可在版本管理「安装」通道静默安装，或使用便携版托管")
		}
		return info.Version, info.ExePath, nil
	}
	if active := s.store.GetActive(); active != "" {
		if exe, err := s.manager.ResolveExe(active); err == nil {
			return active, exe, nil
		}
		_ = s.store.SetActive("") // 已设定的版本被卸载/损坏：清空自愈，回退最新已装
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", err
	}
	if len(installed) == 0 {
		return "", "", fmt.Errorf("尚未安装任何 VS Code 便携版，请先在版本管理下载、导入本地或使用安装通道")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	return installed[0].Version, installed[0].ExePath, nil
}

// resolveExeAny external 态信使 exe（信使归属实例组由 user-data 决定，与具体版本号无关）。
func (s *VSCodeService) resolveExeAny(isInstaller bool) (string, error) {
	if isInstaller {
		info := version.DetectInstalled()
		if !info.Installed {
			return "", fmt.Errorf("本机没有安装版 VS Code，无法代为唤起外部实例窗口")
		}
		return info.ExePath, nil
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 VS Code 便携版，无法代为唤起外部实例窗口")
	}
	return installed[0].ExePath, nil
}

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false，两形态共用）。
func (s *VSCodeService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *VSCodeService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用便携版创建快捷方式（同名覆盖）。
func (s *VSCodeService) CreateDesktopShortcut() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	_, exe, err := s.resolveStartTarget(false)
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("VS Code（便携托管）", exe, filepath.Dir(exe))
}

// RepositoryURL 上游官网地址（页面展示与复制）。
func (s *VSCodeService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游官网。
func (s *VSCodeService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// ---------- 小工具 ----------

// normalizeVersion 归一化版本输入（VS Code 版本无 v 前缀，容错剥除大小写 v）。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	return strings.TrimSpace(v)
}

// labelForm 形态中文标签（错误/通知文案共用）。
func labelForm(form string) string {
	if strings.EqualFold(form, formInstaller) {
		return "安装版"
	}
	return "便携版"
}

// versionCompare 比较 X.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名与字符串序对 1.100.0/1.99.0 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（先经模块自有 normalizeVersion 归一再逐段委托）。
	return versioncmp.Compare(normalizeVersion(a), normalizeVersion(b))
}
