package dbx

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
	"hanxi/internal/modules/dbx/instance"
	"hanxi/internal/modules/dbx/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（单实例互斥体出现；拒探场为主窗在场）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	// dataDirChildName 托管受控数据目录名：Hanxi 数据根下 `dbx` 子目录
	// （paths.DataDir() 家族惯例——memo/mcp/everything 皆为数据根下小写模块名
	// 子目录）。阶段 0 裁决：托管启动注入 env DBX_DATA_DIR=<该路径>（上游实证
	// env 优先级最高）；路径与 activeVersion 无关、跨版本共享——不注入则出厂
	// portable 数据随版本目录走、删版本丢数据，绝不允许。
	dataDirChildName = "dbx"
)

// versionEngine 版本引擎依赖面（冻结契约的 version 子包公开面投影）：
// 生产装配 version.NewManager 具体实现，单测注入假 Manager 以隔离磁盘与
// 网络、确定性驱动漂移投影/卸载清账路径。签名以 version 线在盘实面为准
// （manager.go 已落盘面逐字对齐），本接口即联编收口点。
type versionEngine interface {
	ListRemote() ([]version.DBXRelease, error)
	ListInstalled() ([]version.DBXVersionInfo, error)
	DownloadContext(ctx context.Context, txnID, targetVersion string, progress func(version.DownloadProgress)) error
	ImportLocal(srcDir string) (version.DBXVersionInfo, error)
	Remove(targetVersion string) error
	ResolveExe(targetVersion string) (string, error)
	VerifyLedger(targetVersion string) (drifted bool, note string)
	CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error)
}

// DBXService 向前端暴露 DBX 版本管理、实例控制与账本漂移感知能力。
// 数据库连接与 SQL 操作全部在 DBX 自有窗口内完成（纯托管决策：其界面即
// 产品价值，且直连用户数据库，内嵌重做风险与性价比都不成立）。
//
// 与 ccswitch 引擎的适配差异（上游契约侦查结论，详见 instance 包注释）：
//   - 同 Tauri v2 家族：互斥体探测 + 信使唤窗逐字同形制（唤窗 = 再拉起一次
//     exe 触发 single-instance handoff，自动 show+focus，无 CLI 参数）；
//     探针分治——互斥体拒探（elevated 外部实例等）兜底进程名 DBX.exe 枚举；
//   - WM_CLOSE 目标取自有 PID 的可见主窗（标题 "DBX"）而非 ccswitch 的
//     隐藏消息窗实证路径（DBX 上游未验证后者的 CloseRequested 路由）；
//   - 托管启动注入 DBX_DATA_DIR（数据改道 Hanxi 数据根，ccswitch 无此层）；
//   - 特有坑治理：用户开"托管备份"会以 schtasks 再拉起 --managed-backup-worker
//     进程（脱出 JobObject）——Quit 后检见同名残留如实 external 呈现并给
//     设置内关闭指引，不强杀托管目录外的 DBX.exe；
//   - 空闲自动退出不做：关窗驻托盘正是许多用户的挂账姿势，DBX 会话长驻
//     与其"托管备份"后台形态冲突面大（gonavi 同族结论：不引入未要求行为）。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type DBXService struct {
	plat    platform.Platform
	manager versionEngine
	store   *dbxStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	// dataDir 托管受控数据目录（Hanxi 数据根稳定路径，构造期定格）。
	dataDir string

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

// NewDBXService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewDBXService(plat platform.Platform, holder *extapi.LeaseHolder) *DBXService {
	paths := settings.GetPaths()
	svc := &DBXService{
		plat:      plat,
		manager:   version.NewManager(paths.VersionsDir()),
		store:     newDBXStore(paths.StateDir()),
		downloads: make(map[string]struct{}),
		holder:    holder,
		dataDir:   filepath.Join(paths.DataDir(), dataDirChildName),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewDBXProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 dbx:instance-state。
func (s *DBXService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("dbx instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("dbx:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("dbx", "DBX 实例异常", snap.Error, "/ext/dbx")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体（拒探兜底进程名）校正
// external/stopped。（自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *DBXService) activate() {
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
func (s *DBXService) ListReleases() ([]version.DBXRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表（含账外漂移复查明细 HashDrifted/DriftNote）。
func (s *DBXService) ListInstalledVersions() ([]version.DBXVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// dbxInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：dbx 是
// 便携 zip 形态，verify（官方摘要双核）由内核 Fetch 折进 download 步内完成，
// 模块进度词表不单独可见，如实不造幻影步骤。
var dbxInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 dbx:version-download
// 推送进度；同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链
// 追加版本，managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段
// 迁移逐步 Advance，收口经观察面 Handle 自动落账并广播 operation:changed
// （与既有模块事件双通道并行，Wave 4-B 接线，markeron/ccswitch 同构）。
func (s *DBXService) DownloadVersion(targetVersion string) (string, error) {
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
				app.Event.Emit("dbx:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error("dbx", "版本下载失败", fmt.Sprintf("DBX %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/dbx")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, dbxInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("dbx:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("dbx", "版本下载失败", fmt.Sprintf("DBX %s 事务开启失败: %v", targetVersion, terr), "/ext/dbx")
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
			slog.Debug("dbx download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
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
				app.Event.Emit("dbx:version-download", p)
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
				notify.Success("dbx", "版本下载成功", fmt.Sprintf("DBX %s 已成功安装", p.Version), "/ext/dbx")
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
				notify.Error("dbx", "版本下载失败", fmt.Sprintf("DBX %s 下载失败: %v", targetVersion, err), "/ext/dbx")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("dbx", "版本下载失败", fmt.Sprintf("DBX %s 事务收口失败: %v", targetVersion, err), "/ext/dbx")
			return
		}
		// "首装自动设使用"已在 done 事件发出前的 emit 收口落账（见上），此处不再重复。
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的自有版本拒绝卸载）。
// 数据面纪律：卸载只删版本隔离目录（exe 与 portable.dbx 标记等五条目），
// **绝不触碰** Hanxi 数据根下的 DBX 受控数据目录——删版本不丢数据；
// 数据只有在用户明示时才允许清理（本模块不提供任何删数据通道）。
// 末版可卸口径：允许卸载到"零版本"——卸载后列表为空、activeVersion 清空、
// 下一次冷启动给出"先下载或导入"的指引，模块不因此死锁。外部实例正占用
// 文件时由 manager 的 rename 隔离删除如实报错，不谎报成功。
func (s *DBXService) RemoveVersion(targetVersion string) error {
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
func (s *DBXService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *DBXService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已安装的 DBX（安装版/绿色版均可）。
// 导入物仅版本隔离目录内的 exe 与伴生文件（portable.dbx 标记必须保留——
// 缺失即拒的闸门在 version 线，错误如实透传不吞）；数据不受导入影响
// （托管启动恒注入 DBX_DATA_DIR，改道 Hanxi 数据根）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *DBXService) ImportLocal(srcDir string) (version.DBXVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.DBXVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.DBXVersionInfo{}, fmt.Errorf("DBX 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.DBXVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮；受控数据目录
// 同由前端持 GetStatus.dataDir 走本方法打开——explorer 目录语义，绝不传
// 文件路径）。收口至 windows.RevealDir（markeron「打开安装目录」事故教训）。
func (s *DBXService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// GetStatus 返回引擎当前状态快照 + 使用版本账本漂移投影 + 数据改道账目
// （先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
// 漂移复查对象：设定版本优先，未设定时取快照携带的最近自有版本；
// 无版本可查（未安装）时不复查、driftNote 留空。VerifyLedger 为 mtime 闸控
// 只读复查，轮询成本纪律由 version 线保证。
func (s *DBXService) GetStatus() (DBXStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return DBXStatus{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()
	st := DBXStatus{Snapshot: snap, DataDir: s.dataDir, DataDirInjected: s.store.GetDataDirInjected()}
	v := s.store.GetActive()
	if v == "" {
		v = snap.Version
	}
	if v != "" {
		st.Drifted, st.DriftNote = s.manager.VerifyLedger(v)
	}
	return st, nil
}

// ensureDataDir 托管启动前保证受控数据目录存在（DBX 对不存在的
// DBX_DATA_DIR 目标行为未实证，宁可先建空目录也不赌上游）。
func (s *DBXService) ensureDataDir() error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return fmt.Errorf("创建 DBX 受控数据目录失败: %w", err)
	}
	return nil
}

// OpenWindow 窗口唤起编排中枢（信使路线，ccswitch 同形制）：
//   - external：任一已装 exe 充当单实例信使，插件回调 show+focus 外部主窗口；
//   - running：自有实例直接信使唤窗；
//   - stopped/failed：解析 active 版本，注入 DBX_DATA_DIR 无参启动
//     （DBX 唯一启动语义即"开窗"）。
//
// 信使与冷启动两路都携带受控数据目录：正常场 env 无人消费（信使转发即退），
// 接管场（信使转正为新主实例）数据改道纪律不落空。
func (s *DBXService) OpenWindow() (ControlOutcome, error) {
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
		return ControlOutcome{Action: "starting", Message: "DBX 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.engine.OpenWindow(exe, s.dataDir); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 DBX 窗口"}, nil

	case instance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe(), s.dataDir); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 DBX 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.ensureDataDir(); err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{
			Version:  v,
			Exe:      exe,
			Detached: !s.store.GetFollowOnExit(),
			DataDir:  s.dataDir,
		}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 DBX 失败: %w", err)
		}
		// spawn 成功即数据改道事实成立（进程读 env 在自身初始化内完成，
		// 注入账先记；WaitReady 失败的回执由下方失败分支如实给出）。
		if err := s.store.SetDataDirInjected(true); err != nil {
			slog.Warn("dbx dataDirInjected 落账失败（不阻断启动）", "err", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 DBX 就绪超时（%d 秒），请确认已安装 WebView2 Runtime（Win11 系统自带）", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("DBX %s 已启动", v)}, nil
	}
}

// QuitAdvisory 托管退出预告文案（前端"退出"按钮确认框在执行 Quit 前展示）：
// DBX 关窗语义按用户设置驻托盘或弹询问确认框，WM_CLOSE 可能只藏窗或挂
// 模态框——托管退出在优雅宽限后以 JobObject 强杀兜底（自有实例）。请在
// 托管退出前保存未完成的工作。静态文案单点收口在后端，避免前端各入口
// 话术漂移（gonavi 同法）。
func (s *DBXService) QuitAdvisory() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return "DBX 关窗默认驻托盘或弹询问；托管退出在宽限后将强制终止自有实例，未保存的编辑内容可能丢失，请先在 DBX 内处理。", nil
}

// externalResidueHint 应用自带"托管备份"计划任务的复拉坑位指引
// （阶段 0 实证：schtasks 以 --managed-backup-worker 再拉起 DBX.exe，
// 脱出 JobObject——Quit 后 RefreshExternal 检见同名残留时如实给出）。
const externalResidueHint = "应用自带「托管备份」的计划任务可能重新拉起进程，可在 DBX 设置内关闭该功能；" +
	"Hanxi 不强杀托管外的 DBX 进程，如需退出请在 DBX 窗口或系统托盘内操作。"

// Quit 退出引擎托管的 DBX（优雅 WM_CLOSE + 宽限 + JobObject 强杀兜底，
// 语义见 QuitAdvisory 预告）。
// external 状态不越权强杀（互斥体场拿不到外部 PID，worker 残留更不归管辖）：
// 仅返回人性化指引。自有实例退出后复检一次外部感知——"托管备份"复拉的
// 残留进程如实 external 呈现并附指引（外部实例治理口径）。
func (s *DBXService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动（或计划任务拉起）的实例，未越权终止。" + externalResidueHint}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	// 退出后复检外部感知：自有实例归零，但"托管备份"worker 或用户自启实例
	// 可能同名在场——如实呈现 external + 指引，不静默也不越权处置。
	s.engine.RefreshExternal()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: true, External: true,
			Message: "托管的 DBX 已退出，但检测到同名进程仍在运行。" + externalResidueHint}, nil
	}
	return QuitOutcome{Stopped: true, Message: "DBX 已退出"}, nil
}

// MetaHints 如实披露账（前端 meta 区消费，一次拉全）：数据留存策略、
// 完整性信任根、备份 worker 治理、WebView2 依赖降级预告。所有话术单点
// 收口在后端（QuitAdvisory 同纪律），前端不得自造第二套口径。
func (s *DBXService) MetaHints() ([]string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return []string{
		fmt.Sprintf("托管启动注入 DBX_DATA_DIR，数据统一留存在 Hanxi 数据根（%s），跨版本共享；卸载托管版本不删数据，除非您明示。", s.dataDir),
		"版本目录内的 portable.dbx 标记文件照常保留（数据模式双保险），请勿手工删除。",
		"完整性以 GitHub API digest 官方 sha256 为唯一信任根：上游同名 .sig 属 minisign 通道，本托管不验；镜像备位回退通道未经真机实测，宁拒不猜。",
		"应用自带「托管备份」计划任务可能在 Hanxi 之外重新拉起 DBX.exe——残留进程按外部实例如实呈现，Hanxi 不强杀，可在 DBX 设置内关闭该功能。",
		"DBX 依赖 WebView2 Runtime（Win11 系统自带；Win10/精简镜像缺失时启动即退，请先安装）。",
	}, nil
}

// Shutdown（RPC 导出版，纯 void）：取得调用门租约后转发内部 shutdown()，
// 拒绝即早退——Wave 3 口径：void 方法不改签名。前端当前不调用本方法，
// 但它属绑定面，必须经门收口。
func (s *DBXService) Shutdown() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.shutdown()
}

// shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)。
// 外部实例与托管备份 worker 不受影响（非我方托管）；自有实例另受
// JobObject KILL_ON_JOB_CLOSE 内核兜底。
func (s *DBXService) shutdown() {
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
func (s *DBXService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 DBX 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// resolveInstalledExeAny 返回任一已装版本 exe 路径（信使用途，与版本号无关）。
func (s *DBXService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 DBX 版本，无法代为唤起外部实例窗口")
	}
	return installed[0].ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 0.10.0/0.9.0 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）。
	return versioncmp.Compare(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}

// ---------- 联动开关与上游直达 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *DBXService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *DBXService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库官方地址 github.com/t8y2/dbx（页面展示与复制）。
func (s *DBXService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *DBXService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}
