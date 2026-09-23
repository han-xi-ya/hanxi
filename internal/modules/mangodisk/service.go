package mangodisk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/mangodisk/instance"
	"hanxi/internal/modules/mangodisk/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second
	watchInterval = 5 * time.Second
)

// MangoDiskService 只托管原版 GUI；磁盘扫描、清理和系统设置仍在上游窗口内完成。
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type MangoDiskService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *mangoDiskStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	downloadMu sync.Mutex
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewMangoDiskService 装配版本管理器、持久化 store 与实例引擎。
// 构造不触发任何 IO/网络；外部实例嗅探协程由 activate 在模块 OnInit 时启动。
func NewMangoDiskService(plat platform.Platform, holder *extapi.LeaseHolder) *MangoDiskService {
	paths := settings.GetPaths()
	svc := &MangoDiskService{
		plat: plat, manager: version.NewManager(paths.VersionsDir()), store: newMangoDiskStore(paths.StateDir()),
		holder: holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewMangoDiskProbe(), instance.Callbacks{OnState: svc.emitInstanceState})
	return svc
}

// emitInstanceState 实例引擎状态回调：广播前端事件驱动页面刷新，Failed 时另发系统通知。
// 由 engine 内部 goroutine 调用，勿在此做阻塞操作。
func (s *MangoDiskService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("mangodisk instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("mangodisk:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("mangodisk", "MangoDisk 实例异常", snap.Error, "/ext/mangodisk")
	}
}

// activate 启动外部实例嗅探 goroutine（每 watchInterval 轮询 RefreshExternal）。
// 幂等：已监视则直接返回。goroutine 由 shutdown 关闭 watchStop 通道终止，模块停用必须成对调用。
func (s *MangoDiskService) activate() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watching {
		return
	}
	s.watching = true
	stop := make(chan struct{})
	s.watchStop = stop
	go func() {
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				s.engine.RefreshExternal()
			}
		}
	}()
}

// ListReleases 拉取远端官方版本列表（GitHub Releases，经缓存与镜像加速），网络失败返回错误。
func (s *MangoDiskService) ListReleases() ([]version.MangoDiskRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 扫描本地 versions 目录并做完整性校验（哈希基线比对）。
func (s *MangoDiskService) ListInstalledVersions() ([]version.MangoDiskVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// mangodiskInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：mangodisk 是
// 单文件便携 exe 形态，unpack 步不存在（不造幻影步骤）；verify 对应模块的
// 装机字节数与 PE 身份断言（官方摘要双核由内核 Fetch 折进 download 步内完成）。
var mangodiskInstallSteps = []string{"download", "verify", "place"}

// DownloadVersion 异步下载指定版本：立即返回 "started"（已在本地则返回 "already-installed"），
// 进度与结果经 "mangodisk:version-download" 事件与通知推送。同一时刻仅允许一个下载
// （TryLock 失败直接报"正在下载"），未设置使用版本时下载完成后自动设为当前版本。
// 同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链追加版本，
// managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段迁移逐步
// Advance，收口经观察面 Handle 自动落账并广播 operation:changed（与既有模块
// 事件双通道并行，Wave 4-B 接线，ccswitch/rufus 同构）。
func (s *MangoDiskService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if !s.downloadMu.TryLock() {
		return "", fmt.Errorf("已有 MangoDisk 版本正在下载，请等待完成后再试")
	}
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, item := range installed {
			if item.Version == targetVersion {
				s.downloadMu.Unlock()
				return "already-installed", nil
			}
		}
	}
	opKind := extapi.OpInstall
	if err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate
	}
	txnID := uuid.NewString()
	go func() {
		defer s.downloadMu.Unlock()
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("mangodisk:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error("mangodisk", "版本下载失败", fmt.Sprintf("MangoDisk %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/mangodisk")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, mangodiskInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("mangodisk:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("mangodisk", "版本下载失败", fmt.Sprintf("MangoDisk %s 事务开启失败: %v", targetVersion, terr), "/ext/mangodisk")
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
		emit := func(progress version.DownloadProgress) {
			slog.Debug("mangodisk download progress", "version", progress.Version, "stage", progress.Stage, "done", progress.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("mangodisk:version-download", progress)
			}
			// journal 只在阶段迁移处落盘（§8.2 每步迁移即持久化）；
			// 下载分块进度仅进观察面内存投影，不产生 fsync 风暴
			switch progress.Stage {
			case "downloading":
				if stepIdx < 0 {
					stepIdx = 0
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
				txn.Progress(progress.Done, progress.Total)
			case "verify":
				if stepIdx < 1 {
					stepIdx = 1
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
			case "install":
				if stepIdx < 2 {
					stepIdx = 2
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
			}
			if progress.Stage == "done" {
				notify.Success("mangodisk", "版本下载成功", fmt.Sprintf("MangoDisk %s 已成功安装", progress.Version), "/ext/mangodisk")
			}
		}
		if err := s.manager.DownloadContext(txn.Context(), txnID, targetVersion, emit); err != nil {
			// N26 用户主动取消：按 2b 纪律如实收口，票面话术不露 ctx 原始错误；
			// 主动动作不发"失败"系统通知（前端票面已呈现「已取消」）。
			if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: "托管下载已取消"})
			} else {
				txn.Fail("asset-install-failed", err.Error())
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
				notify.Error("mangodisk", "版本下载失败", fmt.Sprintf("MangoDisk %s 下载失败: %v", targetVersion, err), "/ext/mangodisk")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("mangodisk", "版本下载失败", fmt.Sprintf("MangoDisk %s 事务收口失败: %v", targetVersion, err), "/ext/mangodisk")
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

// RemoveVersion 删除本地版本目录；该版本正在托管运行时拒绝，删除后若其为使用版本则清空 active。
func (s *MangoDiskService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning && snap.Version == targetVersion {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("")
	}
	return nil
}

// SetActiveVersion 将指定版本设为"当前使用版本"。落盘前先 Inspect，
// 完整性校验失败（IntegrityInvalid）的版本拒绝激活，返回错误提示重新下载/导入。
func (s *MangoDiskService) SetActiveVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	info, err := s.manager.Inspect(targetVersion)
	if err != nil {
		return "", err
	}
	if info.Integrity == version.IntegrityInvalid {
		return "", fmt.Errorf("版本 %s 安装无效：%s", targetVersion, info.IntegrityNote)
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前使用版本号；未设置时返回空串（error 恒为 nil，为前端统一签名保留）。
func (s *MangoDiskService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入用户自备的 MangoDisk 可执行文件为托管版本（读取 PE 版本信息建基线）。
// 实例正在运行（托管或外部嗅探到）时拒绝，避免导入后新旧文件混用。
func (s *MangoDiskService) ImportLocal(srcExe string) (version.MangoDiskVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.MangoDiskVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.MangoDiskVersionInfo{}, fmt.Errorf("MangoDisk 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcExe))
	if err != nil {
		return version.MangoDiskVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// OpenDir 用资源管理器打开目录；路径为空或不存在时返回错误（explorer.Start 本身不报错，故先行校验）。
func (s *MangoDiskService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("目录路径不能为空")
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// GetStatus 先强制刷新外部实例嗅探再返回快照，保证前端轮询看到的是实时状态（error 恒 nil 为签名统一）。
func (s *MangoDiskService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 是页面/托盘"启动"的统一入口，按实例状态机分支：
// Starting→提示等待；External（外部自启实例）→借用其 exe 唤起窗口；
// Running→唤起自家窗口；Stopped/Failed→校验完整性后冷启动并等待就绪（readyTimeout）。
// 冷启动时按 GetFollowOnExit 决定是否挂入 JobObject（Detached 则不随 Hanxi 退出）。
func (s *MangoDiskService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()
	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "MangoDisk 正在启动中，请稍候"}, nil
	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true, Message: "已唤起外部运行中的 MangoDisk 窗口"}, nil
	case instance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 MangoDisk 窗口"}, nil
	default:
		selected, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.manager.VerifyBeforeLaunch(selected); err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: selected, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 MangoDisk 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			if current := s.engine.Snapshot(); current.State == instance.StateFailed && current.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", current.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 MangoDisk 就绪超时（%d 秒），请确认程序完整且已安装 WebView2 Runtime", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(selected)
		}
		return ControlOutcome{Action: "started", Message: fmt.Sprintf("MangoDisk %s 已启动", selected)}, nil
	}
}

// Quit 优雅退出托管实例；外部自启实例（External）不归 Hanxi 管，只回提示不动它。
func (s *MangoDiskService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{External: true, Message: "当前是外部自行启动的实例，请在 MangoDisk 窗口内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "MangoDisk 已退出"}, nil
}

// Shutdown（RPC 导出版，纯 void）：取得调用门租约后转发内部 shutdown()，
// 拒绝即早退——Wave 3 口径：void 方法不改签名。前端当前不调用本方法，
// 但它属绑定面，必须经门收口。
func (s *MangoDiskService) Shutdown() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.shutdown()
}

// shutdown 在模块 OnDestroy 时调用：终止嗅探 goroutine，并按"随 Hanxi 退出"开关决定是否停止托管实例。
// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)。
func (s *MangoDiskService) shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop()
	}
}

// resolveActiveVersion 解析"当前应使用的版本 + exe 路径"：active 有效则直用（失效自动清空），
// 否则回退到完整性可用的最高版本（versioncmp 语义化比较），一个都没有才报错。
func (s *MangoDiskService) resolveActiveVersion() (string, string, error) {
	if active := s.store.GetActive(); active != "" {
		if info, err := s.manager.Inspect(active); err == nil && info.Integrity != version.IntegrityInvalid {
			return active, info.ExePath, nil
		}
		_ = s.store.SetActive("")
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", err
	}
	valid := installed[:0]
	for _, item := range installed {
		if item.Integrity != version.IntegrityInvalid {
			valid = append(valid, item)
		}
	}
	if len(valid) == 0 {
		return "", "", fmt.Errorf("尚未安装可用的 MangoDisk 版本，请先在版本管理下载或导入")
	}
	sort.Slice(valid, func(i, j int) bool {
		return versioncmp.Compare(strings.TrimPrefix(valid[i].Version, "v"), strings.TrimPrefix(valid[j].Version, "v")) > 0
	})
	return valid[0].Version, valid[0].ExePath, nil
}

// resolveInstalledExeAny 为 External 实例唤起窗口挑选一个可用的本地 exe（仅借用其单实例唤窗行为）。
// 按可信度排序：官方校验通过 > 本地导入基线 > 漂移（被上游更新器改过但可运行）。
func (s *MangoDiskService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	for _, state := range []version.IntegrityState{version.IntegrityVerified, version.IntegrityLocalBaseline, version.IntegrityDrifted} {
		for _, item := range installed {
			if item.Integrity == state && item.ExePath != "" {
				return item.ExePath, nil
			}
		}
	}
	return "", fmt.Errorf("尚未安装可用的 MangoDisk 版本，无法代为唤起外部实例窗口")
}

// GetFollowOnExit / SetFollowOnExit 读写"随 Hanxi 一起退出"开关：
// 开启时实例挂入 JobObject（Hanxi 退出/崩溃内核连带终止），关闭时 Detached 独立存活。
// 变更只影响之后的启动/关停时机，不热切换已运行实例。
func (s *MangoDiskService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}
func (s *MangoDiskService) SetFollowOnExit(enabled bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(enabled)
}

// CreateDesktopShortcut 在桌面创建指向当前使用版本的快捷方式（同名覆盖）。
// resolveActiveVersion 与冷启动保持一致：未指定 active 时自动选择最新可用版本。
func (s *MangoDiskService) CreateDesktopShortcut() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("MangoDisk", exe, filepath.Dir(exe))
}

// RepositoryURL / OpenRepository 提供上游仓库主页（error 恒 nil 为前端统一签名）。
func (s *MangoDiskService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}
func (s *MangoDiskService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// normalizeVersion 统一版本号形态为 "vX.Y.Z"（前端可能传带或不带 v 前缀）。
func normalizeVersion(value string) string {
	return "v" + strings.TrimPrefix(strings.TrimSpace(value), "v")
}
