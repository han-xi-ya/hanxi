package papertodo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/papertodo/instance"
	"hanxi/internal/modules/papertodo/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（单实例互斥体出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	defaultVariant = version.VariantSelfContained // 未设定时的下载变体
)

// validVariant 变体合法性校验（store 与 service 共用，委托 version 协议常量）。
func validVariant(v string) bool { return version.ValidVariant(v) }

// PaperTodoService 向前端暴露 PaperTodo 版本管理、运行库变体与窗口控制能力。
// 便签编辑本身不内嵌：全程在 PaperTodo 自有纸片窗口操作（数据在其托管目录，
// Hanxi 只负责官方原版 exe 的下载托管与启停唤窗）。
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type PaperTodoService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *papertodoStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewPaperTodoService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewPaperTodoService(plat platform.Platform, holder *extapi.LeaseHolder) *PaperTodoService {
	paths := settings.GetPaths()
	svc := &PaperTodoService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newPapertodoStore(paths.StateDir()),
		holder:  holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewPaperProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 papertodo:instance-state。
func (s *PaperTodoService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("papertodo instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("papertodo:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("papertodo", "PaperTodo 实例异常", snap.Error, "/ext/papertodo")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
// 刻意无空闲自动退出：桌面便签是常驻环境型工具，纸片不应因"无人点"被收走。
func (s *PaperTodoService) activate() {
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
func (s *PaperTodoService) ListReleases() ([]version.PaperRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// GetInstalledVersion 返回当前托管安装信息；未安装时 Version 为空串（不报错，
// 前端以空版本号判"未安装"渲染引导卡）。
func (s *PaperTodoService) GetInstalledVersion() (version.PaperVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.PaperVersionInfo{}, gateErr
	}
	defer release()
	return s.getInstalledVersion()
}

// getInstalledVersion 无门内部版：供已持租约的编排方法（OpenWindow）复用，
// 避免在 stopping 窗口内嵌套申请第二把租约被误拒。
func (s *PaperTodoService) getInstalledVersion() (version.PaperVersionInfo, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil || len(installed) == 0 {
		return version.PaperVersionInfo{}, err
	}
	return installed[0], nil
}

// papertodoInstallSteps 托管资产事务的 journal 步骤词汇（Wave 5）：papertodo 是
// 单文件 exe 形态，unpack 步不存在（不造幻影步骤）；verify 对应本模块完整性降级链
// （内核 Fetch 的官方摘要双核折进 download 步，place 对应 staging→托管目录原子换入，
// 该步由收口 Done 记成——换入在事件词表里无独立阶段，不为记账发明新词）。
var papertodoInstallSteps = []string{"download", "verify", "place"}

// DownloadVersion 后台下载指定版本与变体：立即返回，全程经事件
// papertodo:version-download 推送进度；同时开一笔 journal 托管事务（install 首装 /
// update 覆盖升级与换变体重装，managed-declarative 资产形态）——journal 先落盘
// 再副作用，进度阶段迁移逐步 Advance，收口经观察面 Handle 自动落账并广播
// operation:changed（与既有模块事件双通道并行，Wave 4-B 接线，ccswitch 同款）。
// variant 传空回退 store 偏好。
// 运行中的实例拒绝覆盖：Windows 独占运行中的 exe，改名必失败，提前给出友好指引。
func (s *PaperTodoService) DownloadVersion(targetVersion, variant string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	variant = strings.TrimSpace(variant)
	if variant == "" {
		variant = s.store.GetVariant()
	}
	if !validVariant(variant) {
		return "", fmt.Errorf("未知运行库变体: %q", variant)
	}
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateStarting {
		return "", fmt.Errorf("PaperTodo 正在运行，请先退出后再安装/升级")
	}

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	// 同版本同变体已安装则直接返回，避免重复下载；换变体视为重装，放行
	opKind := extapi.OpInstall
	if installed, err := s.manager.ListInstalled(); err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate // 已有托管安装：覆盖安装按 update 记账
		if strings.EqualFold(installed[0].Version, targetVersion) && installed[0].Variant == variant {
			return "already-installed", nil
		}
	}
	txnID := uuid.NewString()

	go func() {
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("papertodo:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error("papertodo", "PaperTodo 下载失败", fmt.Sprintf("PaperTodo %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/papertodo")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, papertodoInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("papertodo:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("papertodo", "PaperTodo 下载失败", fmt.Sprintf("PaperTodo %s 事务开启失败: %v", targetVersion, terr), "/ext/papertodo")
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
			slog.Debug("papertodo download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("papertodo:version-download", p)
			}
			// journal 只在阶段迁移处落盘（§8.2 每步迁移即持久化）；
			// 下载分块进度仅进观察面内存投影，不产生 fsync 风暴
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
			case "verify":
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
				notify.Success("papertodo", "PaperTodo 安装成功", fmt.Sprintf("PaperTodo %s（%s）已就绪，便签数据原地保留", p.Version, variantName(variant)), "/ext/papertodo")
			}
		}
		if err := s.manager.DownloadContext(txn.Context(), txnID, targetVersion, variant, emit); err != nil {
			// N26 用户主动取消：按 2b 纪律如实收口，票面话术不露 ctx 原始错误；
			// 主动动作不发"失败"系统通知（前端票面已呈现「已取消」）。
			if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: "托管下载已取消"})
			} else {
				txn.Fail("asset-install-failed", err.Error())
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
				notify.Error("papertodo", "PaperTodo 下载失败", fmt.Sprintf("PaperTodo %s 下载失败: %v", targetVersion, err), "/ext/papertodo")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("papertodo", "PaperTodo 下载失败", fmt.Sprintf("PaperTodo %s 事务收口失败: %v", targetVersion, err), "/ext/papertodo")
			return
		}
	}()

	return "started", nil
}

// RemoveInstalled 卸载当前托管安装。运行中拒绝；
// 卸载只删程序本体与元信息，**便签数据（data.json、图片库、plugins）原地保留**。
func (s *PaperTodoService) RemoveInstalled() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateStarting {
		return fmt.Errorf("PaperTodo 正在运行，请先退出再卸载")
	}
	return s.manager.Remove()
}

// ImportLocal 导入本地已有的 PaperTodo 目录（连便签数据一起收编进托管目录）。
// 运行中的实例（自有或外部）拒绝导入：Windows 下运行中的 exe 被独占，拷贝必然失败。
func (s *PaperTodoService) ImportLocal(srcDir string) (version.PaperVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.PaperVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.PaperVersionInfo{}, fmt.Errorf("PaperTodo 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcDir))
}

// ---------- 变体与联动偏好 ----------

// GetVariant 返回下载变体偏好（self-contained / no-runtime）。
func (s *PaperTodoService) GetVariant() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetVariant(), nil
}

// SetVariant 设定下载变体（下次下载生效，不追溯已装版本）。
func (s *PaperTodoService) SetVariant(variant string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetVariant(variant)
}

// GetRuntimeStatus 探测系统 .NET 桌面运行时（no-runtime 变体的可用性判断）。
func (s *PaperTodoService) GetRuntimeStatus() (RuntimeStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return RuntimeStatus{}, gateErr
	}
	defer release()
	vers := version.DesktopRuntimeVersions()
	return RuntimeStatus{
		DesktopRuntimes: vers,
		HasDesktop10:    version.HasDesktopRuntimeMajor(vers, version.RequiresDesktopMajor),
	}, nil
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *PaperTodoService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *PaperTodoService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// ---------- 控制操作 ----------

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *PaperTodoService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢（show 命令信使 → 上游 ShowAllPapers 找回全部纸片）：
//   - external：托管 exe 充当单实例信使，主实例经命名管道收到命令；
//   - running：自有实例直接派信使；
//   - stopped/failed：解析托管安装直接启动（PaperTodo 启动语义即纸片上桌面）。
func (s *PaperTodoService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会产生两个进程竞速单实例锁）
		return ControlOutcome{Action: "starting", Message: "PaperTodo 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.manager.ResolveExe()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起纸片失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已向外部运行中的 PaperTodo 发送找回纸片命令"}, nil

	case instance.StateRunning:
		if err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起纸片失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已找回 PaperTodo 全部纸片"}, nil

	default:
		info, err := s.getInstalledVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if info.Version == "" {
			return ControlOutcome{}, fmt.Errorf("尚未安装 PaperTodo，请先在版本管理在线下载或用「导入本地」收编已有副本")
		}
		if err := s.engine.Start(instance.StartOptions{Version: info.Version, Exe: info.ExePath, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 PaperTodo 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 PaperTodo 就绪超时（%d 秒）%s", int(readyTimeout/time.Second), variantHint(info.Variant))
		}
		return ControlOutcome{Action: "started", Message: fmt.Sprintf("PaperTodo %s 已启动", info.Version)}, nil
	}
}

// HidePapers 收拢全部纸片（hide 命令信使；主实例不在场时拒绝）。
// 双变体差异仅影响启动依赖，与唤窗/收拢通道无关。
func (s *PaperTodoService) HidePapers() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	snap := s.engine.Snapshot()
	switch snap.State {
	case instance.StateRunning:
		if err := s.engine.HidePapers(s.engine.Exe()); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "hidden", Message: "已收拢 PaperTodo 全部纸片"}, nil
	case instance.StateExternal:
		exe, err := s.manager.ResolveExe()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.HidePapers(exe); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "hidden", External: true, Message: "已向外部实例发送收拢命令"}, nil
	default:
		return ControlOutcome{}, fmt.Errorf("PaperTodo 未在运行，无需收拢")
	}
}

// Quit 退出引擎托管的 PaperTodo（exit 命令优雅退出，宽限后强杀兜底）。
// external 状态不越权强杀（互斥体探测拿不到 PID）：仅返回人性化指引。
func (s *PaperTodoService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 PaperTodo 纸片顶栏或托盘菜单中退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "PaperTodo 已退出（便签数据已自动保存）"}, nil
}

// Shutdown（RPC 导出版，纯 void）：取得调用门租约后转发内部 shutdown()，
// 拒绝即早退——Wave 3 口径：void 方法不改签名。前端当前不调用本方法，
// 但它属绑定面，必须经门收口。
func (s *PaperTodoService) Shutdown() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.shutdown()
}

// shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *PaperTodoService) shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop() // 联动开启才杀；关闭则完全不影响便签（Job 已解除 kill-on-close）
	}
}

// OpenDir 在资源管理器中打开托管目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *PaperTodoService) OpenDir(dir string) error {
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
	if err != nil {
		return fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("目标不是目录: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// CreateDesktopShortcut 在桌面为托管安装创建快捷方式（同名覆盖）。
func (s *PaperTodoService) CreateDesktopShortcut() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	exe, err := s.manager.ResolveExe()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("PaperTodo", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *PaperTodoService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *PaperTodoService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// OpenReleasesPage 用默认浏览器打开上游 Releases 页（绕过托管自行下载兜底）。
func (s *PaperTodoService) OpenReleasesPage() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.ReleasesPageURL())
}

// variantName 变体中文名（通知文案用）。
func variantName(v string) string {
	if v == version.VariantNoRuntime {
		return "no-runtime 精简版"
	}
	return "self-contained 完整版"
}

// variantHint 就绪超时的变体针对性提示：no-runtime 缺运行库是最常见死法。
func variantHint(v string) string {
	if v == version.VariantNoRuntime {
		return "：no-runtime 变体需要系统已安装 .NET 10 桌面运行时，可在「开发环境检测」页查看或改用完整版"
	}
	return "，请稍后重试或在版本管理中重新安装"
}
