package airsnip

import (
	"context"
	"errors"
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
	"hanxi/internal/modules/airsnip/instance"
	"hanxi/internal/modules/airsnip/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 15 * time.Second // 冷启动就绪上限（进程出现；未提权时 spawn 直得 740，不占满本窗口）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// AirsnipService 向前端暴露 AirSnip 版本管理与实例控制能力。
// 截图覆盖层、标注工具栏、长截图、贴图窗、设置面板全部留在上游本体
// （纯托管决策——热键驱动的全屏冻结形态无法内嵌，内嵌等于重做整个产品）。
//
// 与 litemonitor/ccswitch 引擎的适配差异（上游契约侦查结论，详见 instance 包注释）：
//   - 无主窗口 + 无 CLI + 官方唤窗通道是上游死代码 → 控制面走上游自己的
//     托盘菜单命令 ID（PostMessage WM_COMMAND），四个动作全部"投递即返回、
//     以状态复探为准"，不承诺必达；
//   - manifest requireAdministrator → 740 直拒特判（双通道：广播快照 + RPC 错误）；
//   - 配置不随 exe 目录（%APPDATA%\airsnip\config.toml）→ 启动前就地接管
//     auto_check_update 单键，而非首启播种；
//   - 托盘常驻工具定位 → 不做空闲自动退出（截图工具待机 0% CPU 本就是卖点，
//     空闲即退语义不成立，与 litemonitor/snipaste 同一产品约束）。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type AirsnipService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *airsnipStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	// download 下载→落位链的函数接缝：生产装配指向 manager.DownloadContext，
	// 测试注入假实现以隔离网络驱动 emit 收口时序（eartrumpet 注入接缝同法）。
	download func(ctx context.Context, txnID, targetVersion string, emit func(version.DownloadProgress)) error

	// command 内部命令通道接缝：生产由 engine 直投 Win32 消息，service 层单测
	// 注入 fake 以断言"哪个动作投了哪个 ID、失败时的话术"，不假装验过真 Win32。
	command func(kind string) (posted bool, focused bool)

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewAirsnipService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回
// 本 service，二者生命周期一致）；构造无 IO。
func NewAirsnipService(plat platform.Platform, holder *extapi.LeaseHolder) *AirsnipService {
	paths := settings.GetPaths()
	svc := &AirsnipService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newAirsnipStore(paths.StateDir()),
		holder:  holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewAirsnipProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	svc.download = svc.manager.DownloadContext
	svc.command = svc.engineCommand
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 airsnip:instance-state。
func (s *AirsnipService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("airsnip instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("airsnip:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("airsnip", "AirSnip 实例异常", snap.Error, "/ext/airsnip")
	}
}

// activate 启动后台外部实例感知：5s 轮询进程存在性校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *AirsnipService) activate() {
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
				if s.engine != nil { // 装配前/测试夹具态无引擎：轮询无事可做（snipaste 同纪律）
					s.engine.RefreshExternal()
				}
			}
		}
	}()
}

// ---------- 版本管理（委托 manager） ----------

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存）。
func (s *AirsnipService) ListReleases() ([]version.AirsnipRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表（按版本降序，最新在前，
// imported- 兜底目录由 ListInstalled 的 importedRank 闸沉底）。
func (s *AirsnipService) ListInstalledVersions() ([]version.AirsnipVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// airsnipInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：airsnip 是
// 便携 zip 形态，verify（官方摘要双核）由内核 Fetch 折进 download 步内完成，
// 布局自检与落位并入 place 步，模块进度词表不单独可见，如实不造幻影步骤。
var airsnipInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 airsnip:version-download
// 推送进度；同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链
// 追加版本，managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段
// 迁移逐步 Advance，收口经观察面 Handle 自动落账并广播 operation:changed
// （与既有模块事件双通道并行，litemonitor/ccswitch 同构）。
func (s *AirsnipService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)

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
	opKind := extapi.OpInstall
	if err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate
	}
	txnID := uuid.NewString()

	go func() {
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			s.emitDownload(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			notify.Error("airsnip", "版本下载失败", fmt.Sprintf("AirSnip %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/airsnip")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, airsnipInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			s.emitDownload(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			notify.Error("airsnip", "版本下载失败", fmt.Sprintf("AirSnip %s 事务开启失败: %v", targetVersion, terr), "/ext/airsnip")
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
			// 首装自动设使用的落账必须在 done 事件发出之前（termora 2ac9b3b 同构
			// 竞态）：前端共享 store 收到 done 即复刷版本区读 GetActiveVersion，
			// 事件先行于落账时瞬时复刷读到空值。done 仅在落位 Commit 成功后发出，
			// 此时版本已真实在场；未设使用时才立首装，后续升级不改动用户手选的 active。
			if p.Stage == "done" && s.store.GetActive() == "" {
				_ = s.store.SetActive(targetVersion)
			}
			slog.Debug("airsnip download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			s.emitDownload(p)
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
			case "extract":
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
				notify.Success("airsnip", "版本下载成功", fmt.Sprintf("AirSnip %s 已成功安装", p.Version), "/ext/airsnip")
			}
		}
		if err := s.download(txn.Context(), txnID, targetVersion, emit); err != nil {
			// N26 用户主动取消：按 2b 纪律如实收口，票面话术不露 ctx 原始错误；
			// 主动动作不发"失败"系统通知（前端票面已呈现「已取消」）。
			if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: "托管下载已取消"})
			} else {
				txn.Fail("asset-install-failed", err.Error())
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
				notify.Error("airsnip", "版本下载失败", fmt.Sprintf("AirSnip %s 下载失败: %v", targetVersion, err), "/ext/airsnip")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("airsnip", "版本下载失败", fmt.Sprintf("AirSnip %s 事务收口失败: %v", targetVersion, err), "/ext/airsnip")
			return
		}
		// "首装自动设使用"已在 emit 闭包收口 done 时先于事件落账，此处不再重复。
	}()

	return "started", nil
}

// emitDownload 版本进度事件下发（app 未就绪时静默丢弃，与全家族一致）。
func (s *AirsnipService) emitDownload(p version.DownloadProgress) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("airsnip:version-download", p)
	}
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
func (s *AirsnipService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if s.engine != nil {
		if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
			strings.EqualFold(strings.TrimPrefix(snap.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
			return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
		}
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
func (s *AirsnipService) SetActiveVersion(targetVersion string) (string, error) {
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

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）。
func (s *AirsnipService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的 airsnip.exe（配置在 %APPDATA%，不随 exe 走，
// 因此只收纳主程序与在场文档，不做整树迁移）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *AirsnipService) ImportLocal(srcDir string) (version.AirsnipVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.AirsnipVersionInfo{}, gateErr
	}
	defer release()
	if s.engine != nil {
		if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
			return version.AirsnipVersionInfo{}, fmt.Errorf("AirSnip 正在运行，请先退出再导入")
		}
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.AirsnipVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe <dir> 直启；
// 刻意不走 explorer.exe <file> 的"执行"语义（markeron「打开安装目录」按钮的事故教训）。
func (s *AirsnipService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// OpenConfigDir 打开上游配置目录 %APPDATA%\airsnip（config.toml 所在处）。
// 快捷键改键、贴图位置等全部在上游设置面板内完成，本模块不代改配置；
// 唯一被 Hanxi 写入的键是 auto_check_update（版本升级单源收口，见 instance 包）。
func (s *AirsnipService) OpenConfigDir() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	dir := instance.ConfigDir()
	if dir == "" {
		return fmt.Errorf("未能定位 AirSnip 配置目录（当前环境缺少 APPDATA）")
	}
	return windows.RevealDir(dir)
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *AirsnipService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	if s.engine == nil {
		return instance.Snapshot{}, errNoEngine
	}
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// errNoEngine 引擎未装配（仅测试夹具/装配半途的异常态可见）：如实报错而不是
// 拿 nil 引擎跑出 panic（snipaste 的 RemoveVersion 显式防护同纪律）。
var errNoEngine = errors.New("AirSnip 运行引擎未装配，请重启 Hanxi")

// engineCommand 命令通道分发（接缝实现）：kind ∈ shot/pin/settings。
func (s *AirsnipService) engineCommand(kind string) (posted bool, focused bool) {
	switch kind {
	case "shot":
		return s.engine.Shot(), false
	case "pin":
		return s.engine.Pin(), false
	case "settings":
		return s.engine.ShowSettings()
	default:
		return false, false
	}
}

// uipiAdvice 命令投递失败时的统一人话指引：AirSnip 以管理员运行，Hanxi 未提权时
// PostMessage 会被 UIPI 静默丢弃（API 返成功、目标毫无反应），这是本模块唯一的
// "看起来什么都没发生"故障面，必须把话说清而不是假装成功。
const uipiAdvice = "未能把命令送达 AirSnip：目标窗口不在场（已退出），或 Hanxi 与实例权限等级不同被系统拦截——" +
	"请以管理员身份重启 Hanxi 后重试，或直接用上游热键/托盘"

// OpenWindow 打开上游设置面板（AirSnip 无主窗口，托盘左键=设置面板；
// 上游"第二实例唤起已有实例"路径是其自身未实现的死代码，不能当信使用）。
// stopped/failed 态先冷启动再投命令，一次点击走完。
func (s *AirsnipService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	return s.dispatch("settings")
}

// Shot 触发一次截图（等价于按上游全局热键 F1 或托盘「截图」项）。
// 未运行时先冷启动再投递——用户点"截图"就是要截图，不该要求他先点启动。
func (s *AirsnipService) Shot() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	return s.dispatch("shot")
}

// Pin 把剪贴板内容贴到屏幕（等价于上游热键 F3 / 托盘「从剪贴板贴图」）。
func (s *AirsnipService) Pin() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	return s.dispatch("pin")
}

// dispatch 三个内部命令动作的共用编排：
//   - starting：前序启动在途，不二次拉起（会与上游单实例竞速）；
//   - running/external：直接向上游消息窗投命令（外部实例同样可投，完整性
//     对等时才真正可达）；
//   - stopped/failed：解析 active 版本冷启动 → 就绪后投命令。
func (s *AirsnipService) dispatch(kind string) (ControlOutcome, error) {
	if s.engine == nil {
		return ControlOutcome{}, errNoEngine
	}
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()
	external := snap.State == instance.StateExternal

	if snap.State == instance.StateStarting {
		return ControlOutcome{Action: "starting", Message: "AirSnip 正在启动中，请稍候"}, nil
	}

	startedNow := false
	var startedVersion string
	if snap.State != instance.StateRunning && !external {
		v, err := s.ensureRunning()
		if err != nil {
			return ControlOutcome{}, err
		}
		startedNow, startedVersion = true, v
	}

	posted, focused := s.command(kind)
	if !posted {
		return ControlOutcome{}, errors.New(uipiAdvice)
	}

	action, message := commandResult(kind, focused)
	if startedNow {
		// 一次点击走完"冷启动 + 动作"：话术如实说明顺手启动的是哪个版本。
		// 纯唤窗路径沿 house 口径记作 started；截图/贴图保留自身动作词
		// （信息量更高），外部实例由 External 标志承载，不叠 action 前缀。
		message = fmt.Sprintf("AirSnip %s 已启动；%s", startedVersion, message)
		if kind == "settings" {
			action = "started"
		}
	}
	return ControlOutcome{Action: action, External: external, Message: message}, nil
}

// commandResult 投递成功后的动作词表与话术（纯函数，单测穷举）。
// focused 仅对 settings 有意义：截图/贴图本就没有"可聚焦主窗"概念。
func commandResult(kind string, focused bool) (action, message string) {
	switch kind {
	case "shot":
		return "shot", "已触发截图：屏幕已冻结，框选区域即可标注"
	case "pin":
		return "pin", "已请求把剪贴板内容贴到屏幕（剪贴板无位图时上游不会创建贴图）"
	default:
		if focused {
			return "opened", "已打开 AirSnip 设置面板"
		}
		return "opened", "已请求打开 AirSnip 设置面板（未能置前，请在任务栏查看）"
	}
}

// ensureRunning 冷启动前置：解析 active 版本 → Start → 等进程在场。
// 740 直拒经 instance.ElevateHint 改写为含「管理员」的指引（与广播快照通道同源）。
func (s *AirsnipService) ensureRunning() (string, error) {
	v, exe, err := s.resolveActiveVersion()
	if err != nil {
		return "", err
	}
	if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
		if hint := instance.ElevateHint(err); hint != "" {
			return "", errors.New(hint)
		}
		return "", fmt.Errorf("启动 AirSnip 失败: %w", err)
	}
	if !s.engine.WaitReady(readyTimeout) {
		if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
			return "", errors.New(cur.Error)
		}
		return "", fmt.Errorf("等待 AirSnip 就绪超时（%d 秒），请确认该版本安装完整后重试", int(readyTimeout/time.Second))
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
	}
	return v, nil
}

// Quit 退出引擎托管的 AirSnip（上游消息窗 WM_CLOSE → 消息循环退出，与点托盘
// 「退出」逐字等价）。贴图/覆盖层随同进程一并收口。
// external 状态不越权强杀（进程枚举不持句柄）：仅返回人性化指引。
func (s *AirsnipService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if s.engine == nil {
		return QuitOutcome{}, errNoEngine
	}
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 AirSnip 托盘右键菜单内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "AirSnip 已退出"}, nil
}

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *AirsnipService) Shutdown() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.shutdown()
	return nil
}

// shutdown 装配布线：Go 直调路径，不得依赖运行态（见 ADR-0001 Wave 3 注记）。
// 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *AirsnipService) shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	if s.store.GetFollowOnExit() && s.engine != nil {
		_ = s.engine.Stop() // 联动开启才杀；关闭则完全不影响工具（Job 已解除 kill-on-close）
	}
}

// ---------- 版本解析 ----------

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，未设定/已失效
// 回退最新已装（versionCompare 自带 importedRank 沉底闸，兜底目录不当"最新"；
// ListInstalled 侧同闸双保险）。
func (s *AirsnipService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 AirSnip 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 1.10.0/1.9.0 这类多位数段有误，必须数值分段比较。
// imported- 兜底目录属非规范形状，versioncmp 对它退化字典序且 'i' > 数字字符
// （"vimported-…" 反被误判比 "v0.9.0" 新），故显式沉底：先比 rank，同 rank 内
// 再走数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）。
	na, nb := strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	if ra, rb := importedRank(na), importedRank(nb); ra != rb {
		return rb - ra
	}
	return versioncmp.Compare(na, nb)
}

// importedRank 排序权重：正常语义版本 0，imported- 兜底目录 1（沉底）。
// 与 version.Manager.ListInstalled 内部闸同形（剥 v 前缀归一后判 imported- 起头）。
func importedRank(version string) int {
	if strings.HasPrefix(version, "imported-") {
		return 1
	}
	return 0
}

// ---------- 桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *AirsnipService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *AirsnipService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
// 上游 exe 声明 requireAdministrator，快捷方式仍走系统正常提权路径（双击弹 UAC）。
func (s *AirsnipService) CreateDesktopShortcut() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("AirSnip 轻截", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *AirsnipService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *AirsnipService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// normalizeVersion 版本入参归一：先 TrimSpace，大小写 v 一律认（剥除既有 v/V 前缀后统一补 v）。
// 裸号/v号/V号/带空白必须落进同一个规范形，否则已装去重、SetActive/Remove 三入口
// 对同一输入各说各话；imported-/vimported- 兜底令牌经"剥既有 v 再补 v"恒等透传
// 为 vimported-（绝不产出 vvimported-），与 ListInstalled 展示及 store 记账口径一致。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	return "v" + v
}
