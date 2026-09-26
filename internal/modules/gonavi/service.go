package gonavi

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/gonavi/instance"
	"hanxi/internal/modules/gonavi/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（"GoNavi" 标题主窗出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	// dataDirName 上游数据目录约定：连接配置与查询收藏恒在 ~/.gonavi
	// （阶段 0 实证）。托管刻意**不注入**任何数据目录改道环境变量（与
	// Termora/WindTerm 同口径：不动用户数据）——数据不随版本目录隔离，
	// 升降级/多版本共存共享同一份 ~/.gonavi，卸载托管版本不触碰它。
	dataDirName = ".gonavi"
)

// versionEngine 版本引擎依赖面（冻结契约的 version 子包公开面投影）：
// 生产装配 version.NewManager 具体实现，单测注入假 Manager 以隔离磁盘与
// 网络、确定性驱动漂移投影/卸载清账路径。签名以 version 线在盘实面为准，
// 联编窗口期如有出入由主会话收口（本接口即收口点）。
type versionEngine interface {
	ListRemote() ([]version.GoNaviRelease, error)
	ListInstalled() ([]version.GoNaviVersionInfo, error)
	DownloadContext(ctx context.Context, txnID, targetVersion string, progress func(version.DownloadProgress)) error
	ImportLocal(srcDir string) (version.GoNaviVersionInfo, error)
	Remove(targetVersion string) error
	ResolveExe(targetVersion string) (string, error)
	VerifyLedger(targetVersion string) (drifted bool, note string)
	CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error)
}

// GoNaviService 向前端暴露 GoNavi 版本管理、实例控制与账本漂移感知能力。
// 数据库管理操作全部在 GoNavi 自有窗口内完成（纯托管决策：其界面本就是
// 产品价值，且直连用户数据库，内嵌重做风险与性价比都不成立）。
//
// 与 ccswitch 引擎的适配差异（上游契约侦查结论，详见 instance 包注释）：
//   - 便携态无单实例互斥体 → 探测/唤窗全部走进程名枚举 + Win32 直操作，
//     二次拉起"信使"路径不存在（拉起即多开）；
//   - 关窗可能被"未保存 SQL"模态确认框挂住 → Quit 宽限后 JobObject 强杀
//     兜底，QuitAdvisory 供前端在执行前如实预告；
//   - 数据恒在 ~/.gonavi 不隔离、不注入环境改道；
//   - 常驻工作台定位 → 不做空闲自动退出（与 litemonitor 同产品约束：
//     会话长驻正是其用途，空闲即退语义不成立）。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type GoNaviService struct {
	plat    platform.Platform
	manager versionEngine
	store   *gonaviStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	downloadMu sync.Mutex
	downloads  map[string]struct{}
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	// 单测 seam（生产装配恒不注入，零成本）：downloadDriver 替代版本下载驱动
	// （缺省走 version.Manager.DownloadContext，测试注入假驱动以隔离网络并同步
	// 驱动 emit）；downloadProbe 为下载进度事件观测点，在广播位（Wails Emit
	// 之前）同步收到事件，锁死"done 成功回执送达时首装落账已完成"的时序
	// （termora 2ac9b3b 同型病灶的回归护栏）。
	downloadDriver func(ctx context.Context, txnID, targetVersion string, emit func(version.DownloadProgress)) error
	downloadProbe  func(p version.DownloadProgress)
}

// NewGoNaviService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewGoNaviService(plat platform.Platform, holder *extapi.LeaseHolder) *GoNaviService {
	paths := settings.GetPaths()
	svc := &GoNaviService{
		plat:      plat,
		manager:   version.NewManager(paths.VersionsDir()),
		store:     newGoNaviStore(paths.StateDir()),
		downloads: make(map[string]struct{}),
		holder:    holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewGoNaviProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 gonavi:instance-state。
func (s *GoNaviService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("gonavi instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("gonavi:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("gonavi", "GoNavi 实例异常", snap.Error, "/ext/gonavi")
	}
}

// activate 启动后台外部实例感知：5s 轮询进程名校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *GoNaviService) activate() {
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

// ListReleases 获取远程可用版本列表（GitHub API digest 官方摘要，10 分钟缓存）。
func (s *GoNaviService) ListReleases() ([]version.GoNaviRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表（含账外漂移复查明细 HashDrifted/DriftNote）。
func (s *GoNaviService) ListInstalledVersions() ([]version.GoNaviVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// gonaviInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：gonavi 是
// 便携 zip 形态，verify（官方摘要双核）由内核 Fetch 折进 download 步内完成，
// 模块进度词表不单独可见，如实不造幻影步骤。
var gonaviInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 gonavi:version-download
// 推送进度；同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链
// 追加版本，managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段
// 迁移逐步 Advance，收口经观察面 Handle 自动落账并广播 operation:changed
// （与既有模块事件双通道并行，Wave 4-B 接线，markeron/ccswitch 同构）。
func (s *GoNaviService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")

	s.downloadMu.Lock()
	if _, ok := s.downloads[targetVersion]; ok {
		s.downloadMu.Unlock()
		return "in-progress", nil
	}

	// 已安装则直接返回，避免重复下载
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(strings.TrimPrefix(v.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
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

	s.downloads[targetVersion] = struct{}{}
	s.downloadMu.Unlock()

	go func() {
		defer func() {
			s.downloadMu.Lock()
			delete(s.downloads, targetVersion)
			s.downloadMu.Unlock()
		}()
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("gonavi:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error("gonavi", "版本下载失败", fmt.Sprintf("GoNavi %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/gonavi")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, gonaviInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("gonavi:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("gonavi", "版本下载失败", fmt.Sprintf("GoNavi %s 事务开启失败: %v", targetVersion, terr), "/ext/gonavi")
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
			slog.Debug("gonavi download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			// "首装自动设使用"的落账必须发生在 done 事件发出之前：前端共享 store
			// 收到 done 即复刷 GetActiveVersion，旧实现放在 DownloadContext 返回
			// 之后，事件先行于落账，瞬时复刷读到空值（termora 2ac9b3b 同型病灶、
			// 同型修法）。done 仅在 manager 落位 Commit 成功后发出。
			if p.Stage == "done" && s.store.GetActive() == "" {
				_ = s.store.SetActive(targetVersion)
			}
			if s.downloadProbe != nil {
				s.downloadProbe(p)
			}
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("gonavi:version-download", p)
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
				notify.Success("gonavi", "版本下载成功", fmt.Sprintf("GoNavi %s 已成功安装", p.Version), "/ext/gonavi")
			}
		}
		drive := s.downloadDriver
		if drive == nil {
			drive = s.manager.DownloadContext
		}
		if err := drive(txn.Context(), txnID, targetVersion, emit); err != nil {
			// N26 用户主动取消：按 2b 纪律如实收口，票面话术不露 ctx 原始错误；
			// 主动动作不发"失败"系统通知（前端票面已呈现「已取消」）。
			if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: "托管下载已取消"})
			} else {
				txn.Fail("asset-install-failed", err.Error())
				emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
				notify.Error("gonavi", "版本下载失败", fmt.Sprintf("GoNavi %s 下载失败: %v", targetVersion, err), "/ext/gonavi")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("gonavi", "版本下载失败", fmt.Sprintf("GoNavi %s 事务收口失败: %v", targetVersion, err), "/ext/gonavi")
			return
		}
		// "首装自动设使用"已在 done 事件发出前的 emit 收口落账（见上），此处不再重复。
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的自有版本拒绝卸载）。
// 末版可卸口径：允许卸载到"零版本"——卸载后列表为空、activeVersion 清空、
// 下一次冷启动给出"先下载或导入"的指引，模块不因此死锁（新模块第一天就按
// 该口径落地，不留旧模块"最后一版卸不掉"的死锁账）。外部实例正占用文件时
// 由 manager 的 rename 隔离删除如实报错，不谎报成功。
func (s *GoNaviService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(strings.TrimPrefix(snap.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	// 卸载的是当前设定版本则清空，下次冷启动自动回退最新已装（末版卸空即指引下载）
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("")
	}
	return nil
}

// SetActiveVersion 设定使用版本（先校验已安装，再持久化）。
func (s *GoNaviService) SetActiveVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
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
func (s *GoNaviService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已安装的 GoNavi（安装版/绿色版均可）。
// 数据恒在 ~/.gonavi 不受导入影响；仅迁移 GoNavi.exe 与许可证文件。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *GoNaviService) ImportLocal(srcDir string) (version.GoNaviVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.GoNaviVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.GoNaviVersionInfo{}, fmt.Errorf("GoNavi 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.GoNaviVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir：其 explorer.exe <dir> 语义即"打开目录"，
// 刻意不走 explorer.exe <file> 的"执行"语义（markeron「打开安装目录」按钮的事故教训）。
func (s *GoNaviService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// OpenConfigDir 打开 GoNavi 的用户数据目录（连接配置与查询收藏所在，home/.gonavi）
// ——纯托管下用户想看"数据在哪"的直达入口。只读导航，不改写；
// 该目录不随版本目录隔离、不受卸载托管版本影响（见 dataDirName 注释）。
func (s *GoNaviService) OpenConfigDir() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("GoNavi 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return windows.RevealDir(dir)
}

// userConfigDir GoNavi 数据目录恒在用户主目录下的 .gonavi（上游约定，跨版本共享不迁移）。
func userConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %v", err)
	}
	return filepath.Join(home, dataDirName), nil
}

// GetStatus 返回引擎当前状态快照 + 使用版本账本漂移投影
// （先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
// 漂移复查对象：设定版本优先，未设定时取快照携带的最近自有版本；
// 无版本可查（未安装）时不复查、driftNote 留空。VerifyLedger 为 mtime 闸控
// 只读复查，轮询成本纪律由 version 线保证。
func (s *GoNaviService) GetStatus() (GoNaviStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return GoNaviStatus{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()
	st := GoNaviStatus{Snapshot: snap}
	v := s.store.GetActive()
	if v == "" {
		v = snap.Version
	}
	if v != "" {
		st.Drifted, st.DriftNote = s.manager.VerifyLedger(v)
	}
	return st, nil
}

// OpenWindow 窗口唤起编排中枢（GoNavi 无信使协议，分流即"已运行则
// EnumWindows 唤回，未运行则拉起"）：
//   - external：进程枚举拿外部 PID → Win32 直操作唤回其窗口；
//   - running：自有实例同样按 PID 直操作唤回；
//   - stopped/failed：解析 active 版本直接无参启动（GoNavi 唯一启动语义即开窗）。
//
// 刻意绝不二次拉起：便携态无单实例互斥体，唤窗信使路线在本上游不成立。
func (s *GoNaviService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（GoNavi 无让位协议，二拉即多开）
		return ControlOutcome{Action: "starting", Message: "GoNavi 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		s.engine.RestoreExternalWindow(s.engine.ExternalPIDs())
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 GoNavi 窗口"}, nil

	case instance.StateRunning:
		s.engine.RestoreWindow()
		return ControlOutcome{Action: "opened", Message: "已唤起 GoNavi 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 GoNavi 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 GoNavi 就绪超时（%d 秒），请确认已安装 WebView2 Runtime（Win11 系统自带）", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("GoNavi %s 已启动", v)}, nil
	}
}

// QuitAdvisory 托管退出预告文案（前端"退出"按钮确认框在执行 Quit 前展示）：
// GoNavi 关窗语义对未保存 SQL 草稿会弹确认框，WM_CLOSE 可能被模态框挂住——
// 托管退出在优雅宽限后以 JobObject 强杀兜底，未经确认的未保存草稿将丢失。
// 静态文案单点收口在后端，避免前端各入口话术漂移。
func (s *GoNaviService) QuitAdvisory() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return "GoNavi 若有未保存 SQL 草稿会弹确认框；托管退出在宽限后将强制终止进程，请在托管退出前自行处理未保存草稿。", nil
}

// Quit 退出引擎托管的 GoNavi（优雅 WM_CLOSE + 宽限 + JobObject 强杀兜底，
// 语义见 QuitAdvisory 预告）。
// external 状态不越权强杀（进程枚举不持句柄）：仅返回人性化指引。
func (s *GoNaviService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 GoNavi 窗口内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "GoNavi 已退出"}, nil
}

// Shutdown（RPC 导出版，纯 void）：取得调用门租约后转发内部 shutdown()，
// 拒绝即早退——Wave 3 口径：void 方法不改签名。前端当前不调用本方法，
// 但它属绑定面，必须经门收口。
func (s *GoNaviService) Shutdown() {
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
func (s *GoNaviService) shutdown() {
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
func (s *GoNaviService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 GoNavi 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 0.10.0/0.9.0 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）。
	return versioncmp.Compare(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *GoNaviService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *GoNaviService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
// 快捷方式属原生入口（用户双击即开，不经 Hanxi 托管）——GoNavi 数据不隔离，
// 从快捷方式启动的实例会被外部感知轮询如实甄别为 external，语义闭环。
func (s *GoNaviService) CreateDesktopShortcut() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("GoNavi", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *GoNaviService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *GoNaviService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}
