package translucenttb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/translucenttb/instance"
	"hanxi/internal/modules/translucenttb/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（单实例互斥体出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	msixQueryTimeout     = 20 * time.Second // Get-AppxPackage 查询兜底（PowerShell 冷启动，nanaZip 同值）
	msixPrepareTimeout   = 15 * time.Minute // msixbundle 下载/复核（版本线内部 fetchBudget 再收一层）
	msixDeployTimeout    = 10 * time.Minute // Add-AppxPackage 部署
	msixUninstallTimeout = 10 * time.Minute // Remove-AppxPackage 卸载
	msixLaunchTimeout    = 30 * time.Second // 激活派发（explorer shell:AppsFolder，nanaZip Launch 同值）
)

// TranslucentTBService 向前端暴露 TranslucentTB 版本管理与托管启停能力。
// 任务栏透明样式设置不内嵌：上游 UI 就是系统托盘 XAML 飞控（无主窗口可唤），
// Hanxi 侧提供版本管理、启停、状态重设与 settings.json 所在目录直达。
// 本模块禁用空闲自动退出（常驻特效工具：退出 = 任务栏特效消失，与托管诉求正相反）。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type TranslucentTBService struct {
	plat     platform.Platform
	manager  *version.Manager
	packages apppackage.API // MSIX 打包形态系统生命周期通道（platform 既有 PowerShell appx 通道）
	msix     msixPackageManager
	store    *translucenttbStore
	engine   *instance.Engine
	holder   *extapi.LeaseHolder

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	// AV 崩溃落账（A 档）：avMu 护 lastAVAt 幂等位——同一次崩溃的终态广播
	// 理论上一笔（transition 只在状态变更时 emit），按 StoppedAt 防重兜底。
	avMu     sync.Mutex
	lastAVAt time.Time
}

// avCrashCode TranslucentTB 原生访问违例退出码：AV 档话术与崩溃落账共用判据
// （Windows 退出码在本机账目为正的 32 位值，反解码统一走 uint32 比较）。
const avCrashCode = 0xC0000005

// NewTranslucentTBService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewTranslucentTBService(plat platform.Platform, holder *extapi.LeaseHolder) *TranslucentTBService {
	paths := settings.GetPaths()
	svc := &TranslucentTBService{
		plat:     plat,
		manager:  version.NewManager(paths.VersionsDir()),
		packages: plat.AppPackage(),
		store:    newTranslucentTBStore(paths.StateDir()),
		holder:   holder,
	}
	// 便携线与 MSIX 线同居一个 version.Manager（msix.go 是其打包形态方法面），
	// seam 指针指向同一实例——共享 versionsDir 与远程列表缓存，不重建第二台。
	svc.msix = svc.manager
	svc.engine = instance.NewEngine(plat.Job(), instance.NewTBProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	// AV 档话术接本机事实（构造期布线，先于任何 Start/快照路径）：
	// 已装旧版点名 + 跨重启 AV 记账数。
	svc.engine.SetAVFacts(svc.avCrashFacts)
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 translucenttb:instance-state。
func (s *TranslucentTBService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("translucenttb instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if snap.State == instance.StateFailed && uint32(snap.ExitCode) == avCrashCode {
		// AV 崩溃落账（"可反解码"事实：ExitCode 仅在引擎解出内核退出码时非零入账）
		s.accountAVCrash(snap)
	}
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("translucenttb:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("translucenttb", "TranslucentTB 实例异常", snap.Error, "/ext/translucenttb")
	}
}

// accountAVCrash AV 崩溃落账：记 {code, version, at} 并跨重启累计计数
// （store 原子写）。记账失败仅降级话术引用，绝不断事件广播/通知主链。
func (s *TranslucentTBService) accountAVCrash(snap instance.Snapshot) {
	at := snap.StoppedAt
	if at.IsZero() {
		at = time.Now()
	}
	s.avMu.Lock()
	defer s.avMu.Unlock()
	if !s.lastAVAt.IsZero() && at.Equal(s.lastAVAt) {
		return // 同一笔崩溃的重复广播：不重账
	}
	s.lastAVAt = at
	if err := s.store.RecordAVCrash(snap.ExitCode, snap.Version, at); err != nil {
		slog.Warn("translucenttb AV 崩溃记账失败", "error", err)
	}
}

// avCrashFacts 引擎 AV 档话术的本机事实（instance.SetAVFacts 注入，在引擎
// 快照锁内执行）：早于崩溃版本的已装旧版（新者在前）+ 跨重启记账数。
// 保持轻量——小目录扫描 + 内存计数；ListInstalled 失败话术照常出通用档。
func (s *TranslucentTBService) avCrashFacts(crashVersion string) instance.AVCrashFacts {
	facts := instance.AVCrashFacts{CrashCount: s.store.AVCrashCount()}
	if crashVersion == "" {
		return facts
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return facts
	}
	for _, v := range installed {
		// 严格早于崩溃版本才算"旧版在场"（imported- 兜底版本退化字典序不入列，宁缺毋滥）
		if versionCompare(v.Version, crashVersion) < 0 {
			facts.OlderVersions = append(facts.OlderVersions, v.Version)
		}
	}
	sort.Slice(facts.OlderVersions, func(i, j int) bool {
		return versionCompare(facts.OlderVersions[i], facts.OlderVersions[j]) > 0
	})
	return facts
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表。
func (s *TranslucentTBService) ListInstalledVersions() ([]version.TBVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// translucenttbInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：translucenttb 是
// 便携 zip 形态，verify（官方摘要双核）由内核 Fetch 折进 download 步内完成，
// 模块进度词表不单独可见，如实不造幻影步骤。
var translucenttbInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 translucenttb:version-download
// 推送进度；同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链
// 追加版本，managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段
// 迁移逐步 Advance，收口经观察面 Handle 自动落账并广播 operation:changed
// （与既有模块事件双通道并行，Wave 4-B 接线，markeron/ccswitch 同构）。
func (s *TranslucentTBService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
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
	opKind := extapi.OpInstall
	if err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate
	}
	txnID := uuid.NewString()

	go func() {
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("translucenttb:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error("translucenttb", "版本下载失败", fmt.Sprintf("TranslucentTB %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/translucenttb")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, translucenttbInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("translucenttb:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("translucenttb", "版本下载失败", fmt.Sprintf("TranslucentTB %s 事务开启失败: %v", targetVersion, terr), "/ext/translucenttb")
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
			slog.Debug("translucenttb download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			s.settleAndBroadcastProgress(targetVersion, p, func(pp version.DownloadProgress) {
				if app := application.Get(); app != nil && app.Event != nil {
					app.Event.Emit("translucenttb:version-download", pp)
				}
			})
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
				notify.Success("translucenttb", "版本下载成功", fmt.Sprintf("TranslucentTB %s 已成功安装", p.Version), "/ext/translucenttb")
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
				notify.Error("translucenttb", "版本下载失败", fmt.Sprintf("TranslucentTB %s 下载失败: %v", targetVersion, err), "/ext/translucenttb")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("translucenttb", "版本下载失败", fmt.Sprintf("TranslucentTB %s 事务收口失败: %v", targetVersion, err), "/ext/translucenttb")
			return
		}
		// done 事件发出前已在 settleAndBroadcastProgress 收口"首装自动设使用"
		// （首个版本下载完成后无需再手动点一下设置，与 ccswitch 既有行为对齐），此处不再重复落账。
	}()

	return "started", nil
}

// settleAndBroadcastProgress 下载回执"先落账、后广播"收口：done 成功回执若尚未
// 设定使用版本，先把刚落位成功的版本记为使用中，再把事件广播给前端——前端共享
// store 收到 done 即复刷版本区读 GetActiveVersion，事件先行于落账会让瞬时复刷读到
// 空值（"首个版本下载完不显示使用中"，与 termora/2ac9b3b 同根修）。manager 仅在
// 原子落位成功后才发 done，据此落账不会把失败版本记成使用中。广播以参数注入，
// 供单测锁死该时序。
func (s *TranslucentTBService) settleAndBroadcastProgress(targetVersion string, p version.DownloadProgress, broadcast func(version.DownloadProgress)) {
	if p.Stage == "done" && s.store.GetActive() == "" {
		_ = s.store.SetActive(targetVersion)
	}
	broadcast(p)
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
// 注意：TranslucentTB 的 settings.json 就在版本目录内，卸载连同用户配置一起删除
// （前端卸载确认框如实预告）。
func (s *TranslucentTBService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已安装的 TranslucentTB 便携版（settings.json 在 exe 同目录，
// 整套随目录迁移，与 ccswitch 的单 exe 导入不同）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *TranslucentTBService) ImportLocal(srcDir string) (version.TBVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.TBVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.TBVersionInfo{}, fmt.Errorf("TranslucentTB 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.TBVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// Start 冷启动编排中枢：
//   - external：不越权拉起第二个实例（上游单实例协议会让它信使化自退并弹
//     "已在运行"气泡，且托管归属混乱），仅如实告知；
//   - running：幂等告知；
//   - stopped/failed：解析 active 版本无参启动。首启会弹上游欢迎授权窗口，
//     互斥体先于该 UI 创建，WaitReady 不受阻塞。
func (s *TranslucentTBService) Start() (ControlOutcome, error) {
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 TranslucentTB 托盘菜单中退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "TranslucentTB 已退出，任务栏已还原默认外观"}, nil
}

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *TranslucentTBService) Shutdown() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.shutdown()
	return nil
}

// shutdown 装配布线：Go 直调路径，不得依赖运行态（见 ADR-0001 Wave 3 注记）。
// 模块停用/应用退出：停后台轮询 + 终止自有实例（仅在联动开启时）。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject 联动口径约束。
func (s *TranslucentTBService) shutdown() {
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *TranslucentTBService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *TranslucentTBService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *TranslucentTBService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *TranslucentTBService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// ---------- MSIX 打包形态系统生命周期（与便携托管线共存，互不干预） ----------

// msixIdentity 打包形态固定身份（常量与三源实证见 models.go；装配风格对齐
// nanaZip packageIdentity，一次成型不再变化）。
var msixIdentity = apppackage.Identity{Name: MsixPackageName, Family: MsixPackageFamily, Publisher: MsixPublisher, AppID: MsixMainAppID}

// msixPackageManager 版本线 MSIX 缓存/下载能力的解耦缝（真实实现 *version.Manager，
// 便携线与其同一实例；单测注入替身）。契约按冻结口径取用：PreparePackage 交回
// 自家缓存目录（versions/translucenttb/packages/<ver>/bundle.msixbundle）终路径，
// 本服务面不消费全量发布列表（HasMsixRelease 判定已足够，ListMsixReleases 不预声明）。
type msixPackageManager interface {
	HasMsixRelease(version string) bool
	PreparePackage(ctx context.Context, version string, progress func(percent float64, stage string)) (string, error)
	PackageCachePaths() []version.PackageCached
	RemovePackageCache(version string) error
}

// GetMsixState 实时查询当前用户 TranslucentTB 打包版注册状态 + 本地容器缓存清单。
// 注册态无服务端缓存（每次经 platform PowerShell appx 通道 Get-AppxPackage 实查，
// 20s 超时兜底冷启动），因此便携线引擎状态缓存与此无同步义务，也不存在"安装
// 成功后刷新缓存"的动作——下一次 GetMsixState 即最新事实。
func (s *TranslucentTBService) GetMsixState() (MsixState, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return MsixState{}, gateErr
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), msixQueryTimeout)
	defer cancel()
	return s.queryMsixState(ctx)
}

// queryMsixState GetMsixState 的未接门内部版：供 RemoveMsixCache 等门后路径复用，
// 避免同 goroutine 对 holder.Enter 的嵌套取租（调用方必须已在门内）。
func (s *TranslucentTBService) queryMsixState(ctx context.Context) (MsixState, error) {
	pkg, err := s.packages.Query(ctx, msixIdentity)
	if err != nil {
		return MsixState{}, describeMsixFailure("注册状态查询", err)
	}
	state := MsixState{PackageFamily: MsixPackageFamily, Cache: s.msix.PackageCachePaths()}
	if pkg != nil {
		state.Installed = true
		state.Version = normalizeMsixVersion(pkg.Version)
	}
	return state, nil
}

// InstallMsix 下载并安装指定版本的官方 bundle.msixbundle（用户级注册，免提权）。
//
// 全程：HasMsixRelease 前置判定（"有 msix 资产但无官方摘要"的版本不入版本线
// 列表，与降级钮判定同一口径，无需在本面二次甄别）→ PreparePackage（摘要必检
// 的缓存落位，终路径只来自自家缓存目录）→ apppackage.Install → Add-AppxPackage
// （platform 既有 PowerShell 通道，子进程非提权态：正常路径不可能出现 UAC，
// 若系统仍回报需要提升的错误码，Detail/HResult 原样上抛不吞）→ 回查注册版本
// 与目标一致才算成功。
//
// AllowDowngrade 恒真：调用方是明确点选版本的用户（崩溃对照降级钮是本线主场景，
// nanaZip 需二次确认的降级在这里就是功能本身），强制走 -ForceUpdateFromAnyVersion。
//
// 共存语义（只陈述、不干预，与便携托管线既有行为对齐）：
//   - 便携版与打包版共用上游单实例协议（Local 互斥体 344635E9-…，
//     instance/probe_windows.go 与上游 main.cpp 同源实证）：后启动者信使化自退，
//     同一时刻任务栏特效只有一个进程持有；
//   - 托管启停线只管 Hanxi 拉起的便携进程——InstallMsix/UninstallMsix/LaunchMsix
//     都不触碰任何在跑实例：不杀便携去装包，也不杀包去留便携；打包版经
//     LaunchMsix/开始菜单启动后游离于 JobObject 之外，引擎按"外部实例"如实感知；
//   - 打包版本身正在运行时卸载/升级由部署器裁决：脚本通道带
//     -DeferRegistrationWhenPackagesAreInUse，冲突时报 APP_PACKAGE_IN_USE
//     （可重试）归因，同样不静默强杀。
func (s *TranslucentTBService) InstallMsix(version string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	version = strings.TrimSpace(version)
	if !s.msix.HasMsixRelease(version) {
		return fmt.Errorf("TranslucentTB %s 上游无可用打包形态（仅收录带 bundle.msixbundle 资产且有官方摘要的版本）", version)
	}

	prepareCtx, prepareCancel := context.WithTimeout(context.Background(), msixPrepareTimeout)
	defer prepareCancel()
	path, err := s.msix.PreparePackage(prepareCtx, version, logMsixPrepareProgress)
	if err != nil {
		return failMsix("安装包准备", fmt.Errorf("TranslucentTB %s 安装包准备失败: %w", version, err))
	}

	deployCtx, deployCancel := context.WithTimeout(context.Background(), msixDeployTimeout)
	defer deployCancel()
	// ExpectedVersion 交脚本侧精确核对：上游发布 tag 恒映射四段 "<tag>.0.0"
	// （update-version.ps1 release 路径：tag + commits_since_tag(=0) + ".0"，
	// 2026.1/2026.2 真实 bundle Identity 实证），Go 侧回查再做一次归一化比对。
	if _, err := s.packages.Install(deployCtx, apppackage.InstallOptions{
		PackagePath:     path,
		Expected:        msixIdentity,
		ExpectedVersion: version + ".0.0",
		AllowDowngrade:  true,
	}); err != nil {
		return failMsix("安装", err)
	}

	pkg, err := s.packages.Query(deployCtx, msixIdentity)
	if err != nil {
		return failMsix("安装后回查", err)
	}
	if pkg == nil || normalizeMsixVersion(pkg.Version) != version {
		return failMsix("安装后核对", fmt.Errorf("TranslucentTB 安装完成后系统注册版本不是 %s（实际 %s）", version, msixVersionOrEmpty(pkg)))
	}
	// 成功路径末尾无需刷新任何包状态缓存：GetMsixState 本就是实查（无缓存可刷）。
	return nil
}

// UninstallMsix 卸载当前用户的 TranslucentTB 打包版注册（Remove-AppxPackage 按
// 实查到的包完整名称执行）。不碰任何缓存文件——容器缓存清理是 RemoveMsixCache
// 的独立语义；便携托管线实例不受触碰（共存口径同 InstallMsix）。
func (s *TranslucentTBService) UninstallMsix() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), msixUninstallTimeout)
	defer cancel()
	pkg, err := s.packages.Query(ctx, msixIdentity)
	if err != nil {
		return failMsix("卸载前查询", err)
	}
	if pkg == nil {
		return fmt.Errorf("TranslucentTB 打包版尚未安装（卸载本就不动缓存文件，缓存请用清理缓存入口）")
	}
	if err := s.packages.Uninstall(ctx, msixIdentity, pkg.PackageFullName); err != nil {
		return failMsix("卸载", err)
	}
	after, err := s.packages.Query(ctx, msixIdentity)
	if err != nil {
		return failMsix("卸载后回查", err)
	}
	if after != nil {
		return failMsix("卸载后核对", fmt.Errorf("卸载后 Windows 回查仍显示 TranslucentTB 打包版已安装"))
	}
	return nil
}

// RemoveMsixCache 删除指定版本的 msixbundle 容器缓存文件（不注销系统包——卸载
// 是 UninstallMsix 的独立语义）。版本线层不判注册态，拦截在本面：目标版本当前
// 正注册在系统（其注册版本可能仍在从缓存目录被 servicing 引用）时拒绝，须先
// UninstallMsix。判定即 GetMsixState 的内容（复用未接门内部查询，语义同
// "调 GetMsixState 判"，只取实查的 Installed/Version 两事实）。
func (s *TranslucentTBService) RemoveMsixCache(version string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	version = strings.TrimSpace(version)
	ctx, cancel := context.WithTimeout(context.Background(), msixQueryTimeout)
	defer cancel()
	state, err := s.queryMsixState(ctx)
	if err != nil {
		return fmt.Errorf("清理缓存前检查打包版状态失败: %w", err)
	}
	if state.Installed && state.Version == version {
		return fmt.Errorf("TranslucentTB %s 打包版正在系统中注册，请先卸载再清理该版本缓存", version)
	}
	return s.msix.RemovePackageCache(version)
}

// LaunchMsix 激活已注册的 TranslucentTB 打包版主应用（对齐 nanaZip 启动通路
// 实证：apppackage.Activate 经 explorer.exe shell:AppsFolder\<Family>!<AppID> 派发，
// 等价点击开始菜单磁贴；进程归系统生命周期，不入 JobObject）。未安装明确报错。
func (s *TranslucentTBService) LaunchMsix() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), msixLaunchTimeout)
	defer cancel()
	pkg, err := s.packages.Query(ctx, msixIdentity)
	if err != nil {
		return describeMsixFailure("启动前查询", err)
	}
	if pkg == nil {
		return fmt.Errorf("TranslucentTB 打包版尚未安装，无法启动")
	}
	if err := s.packages.Activate(ctx, msixIdentity); err != nil {
		return describeMsixFailure("启动", err)
	}
	return nil
}

// logMsixPrepareProgress 版本线下载进度回调的服务侧落点：Debug 日志
// （同步调用面，前端进度契约不在本轮冻结范围，不造幻影事件）。
func logMsixPrepareProgress(percent float64, stage string) {
	slog.Debug("translucenttb msix prepare", "percent", percent, "stage", stage)
}

// normalizeMsixVersion 把系统注册四段版本 "<发布号>.0.0" 归一回发布号两段
// （2026.2.0.0 → 2026.2）：与版本线列表行、activeVersion 同一比较口径。
// 后缀非 0.0 的形态（CI 构建号漂移等）原样返回——宁如实不猜。
func normalizeMsixVersion(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) == 4 && parts[2] == "0" && parts[3] == "0" {
		return parts[0] + "." + parts[1]
	}
	return v
}

// msixVersionOrEmpty 回查失败话术里对 nil 包的安全取值。
func msixVersionOrEmpty(pkg *apppackage.Package) string {
	if pkg == nil {
		return "未注册"
	}
	return pkg.Version
}

// msixFailureError 中文归因包装：话术文本自带，Unwrap 保住原 apppackage.Error，
// errors.As/Is 分支错误码不丢。
type msixFailureError struct {
	message string
	cause   error
}

func (e *msixFailureError) Error() string { return e.message }
func (e *msixFailureError) Unwrap() error { return e.cause }

// msixInfraGuide 包注册基础设施缺失族（0x80073CF6 注册失败 / 0x80073D05 内部错 /
// 0x80073CF3 部署失败）的统一指路话术。实证来路：机主瘦系统实跑 InstallMsix 捕获
// 0x80073CF6+内部 0x80073D05——该机微软商店缺席、AppModelUnlock 侧载/开发者模式键
// 从未存在、部署事件日志通道都不存在，注册不了任何新 appx；话术先给唯一可试的
// 系统级开关（开发者模式），开不动就如实判死刑并指回便携版（本就是正解）。
const msixInfraGuide = "本系统缺少包注册基础设施（微软商店/侧载授权缺失），可在 设置→系统→对于开发人员 开启开发者模式后重试；仍失败则本机打包线不可用，便携版即正解"

// msixInfraFailure 识别基础设施族指纹：CF6/D05 出现在 HResult 或原始 Detail
// （Detail 全文判定，不受话术尾部截断影响）。CF3 由脚本通道归入
// CodeDependency，在归因表里并入依赖缺失话术并附带本指引。
func msixInfraFailure(perr *apppackage.Error) bool {
	fingerprint := strings.ToUpper(perr.HResult + " " + perr.Detail)
	return strings.Contains(fingerprint, "0X80073CF6") || strings.Contains(fingerprint, "0X80073D05")
}

// describeMsixFailure 把 apppackage 通道错误包一层 TranslucentTB 语境的中文归因：
// 通道自带中文 Message，此处拼接输出尾部明细（Detail 最后一行截断）与 HResult，
// 对包注册基础设施缺失/依赖缺失/签名无效三类高频部署失败补针对性指引；
// 原文经 Unwrap 保留，调用方仍可分支错误码。
func describeMsixFailure(op string, err error) error {
	var perr *apppackage.Error
	if !errors.As(err, &perr) {
		return fmt.Errorf("TranslucentTB 打包版%s失败: %w", op, err)
	}
	cause := perr.Message
	if tail := lastLine(perr.Detail); tail != "" {
		cause = fmt.Sprintf("%s: %s", cause, tail)
	}
	if perr.HResult != "" {
		cause = fmt.Sprintf("%s（%s）", cause, perr.HResult)
	}
	hint := ""
	switch {
	case msixInfraFailure(perr):
		hint = "；" + msixInfraGuide
	case perr.Code == apppackage.CodeDependency:
		hint = "；系统缺少框架包（VCLibs / WinUI 2.8），可改用上游 TranslucentTB.appinstaller 安装（自带依赖解析）；也可能是" + msixInfraGuide
	case perr.Code == apppackage.CodeSignature:
		hint = "；bundle 签名证书链未获系统信任，多为上游换签名主体所致，请核对官方发布渠道"
	}
	return &msixFailureError{message: fmt.Sprintf("TranslucentTB 打包版%s失败: %s%s", op, cause, hint), cause: err}
}

// failMsix 装/卸面失败收口（可观测性纪律）：apppackage 通道错误先经中文归因，
// 随即 slog.Warn 落一行完整摘要——toast 一闪而没时，事后台账只认日志
// （0x80073CF6 事故的直接教训：现场零留痕）。WARN 字段含操作名、稳定错误码、
// HRESULT 与归因后一行话术；预构话术（准备失败/回查不符等）原样透传不再套壳。
// GetMsixState/LaunchMsix 不在此列：状态查询会被前端周期轮询，失败留痕若入
// WARN 会刷屏，其失败已有返回值路径。
func failMsix(op string, err error) error {
	attributed := err
	code, hresult := "", ""
	var perr *apppackage.Error
	if errors.As(err, &perr) {
		attributed = describeMsixFailure(op, err)
		code, hresult = perr.Code, perr.HResult
	}
	slog.Warn("translucenttb 打包版操作失败", "op", op, "code", code, "hresult", hresult,
		"summary", strings.Join(strings.Fields(attributed.Error()), " "))
	return attributed
}

// lastLine 明细取最后一行非空文本并按 rune 截尾（"按输出尾部归因"的取值纪律，
// 中文话术不吞原始事实也不刷屏；rune 口径防中英混排截出乱码）：上限 200 rune。
func lastLine(detail string) string {
	lines := strings.Split(strings.TrimSpace(detail), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > 200 {
			return "…" + string(r[len(r)-200:])
		}
		return line
	}
	return ""
}
