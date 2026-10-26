package heukms

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/heukms/instance"
	"hanxi/internal/modules/heukms/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second // 冷启动就绪上限（进程出现）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// HeuKmsService 向前端暴露 HEU 的版本管理、下载/导入落位与实例控制能力。
//
// 与 litemonitor/ccswitch 家族的适配差异（上游契约侦查结论，详见两个子包注释）：
//   - **解包器是 RAR5 而非 zip**：上游当前只发 iso+rar，iso 真实内容在 UDF 侧读不到，
//     故下载链解 rar（nwaples/rardecode）；事件词表、journal 步骤、进度形状与
//     zip 家族逐字同构，差别只在 version.Manager 内部；
//   - **可安装集更窄**：远程表只列"有 rar + 有官方 digest"的版本（内核 Fetch
//     强制摘要），所以下载失败话术里要如实指路「导入本地」；
//   - **进程名随版本漂移** → 探测与唤窗全部按前缀 + PID 直操作（不赌互斥体、
//     不走二次拉起信使：上游闭源 + UPX 压段，单实例契约无法实证）；
//   - **manifest requireAdministrator** → 未提权拉起直得 740，两条通道（RPC 错误
//     与事件快照）都给含「管理员」关键词的指引，供前端 ElevateRestart 一键通道；
//   - **不做空闲自动退出**：本工具是用户主动打开、用完自关的一次性操作程序，
//     "空闲即退"语义不成立（与 rammap 同类判断）；
//   - **不写上游配置**：不 fork、不播种、不改任何上游设置。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门：未安装/停用/阻止模块的
// 任何方法调用被拒，且调用在途期间停用会等待 drain。
type HeuKmsService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *heukmsStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	// download 下载→解包→落位链的函数接缝：生产装配指向 manager.DownloadContext，
	// 测试注入假实现以隔离网络驱动 emit 收口时序（litemonitor/eartrumpet 同法）。
	download func(ctx context.Context, txnID, targetVersion string, emit func(version.DownloadProgress)) error

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewHeuKmsService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本
// service，二者生命周期一致）；构造无 IO。
func NewHeuKmsService(plat platform.Platform, holder *extapi.LeaseHolder) *HeuKmsService {
	paths := settings.GetPaths()
	svc := &HeuKmsService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newHeuStore(paths.StateDir()),
		holder:  holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewHeuProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	svc.download = svc.manager.DownloadContext
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 heukms:instance-state。
func (s *HeuKmsService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("heukms instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("heukms:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("heukms", "HEU 激活工具实例异常", snap.Error, "/ext/heukms")
	}
}

// activate 启动后台外部实例感知：5s 轮询进程存在性校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *HeuKmsService) activate() {
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

// ---------- 版本台账与导入 ----------

// ListReleases 获取上游版本台账（多镜像回退，10 分钟缓存）。
// 本模块用它做"出新版了"的告知与发布页直达，不参与自动安装。
func (s *HeuKmsService) ListReleases() ([]version.HeuRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表（按版本降序，最新在前，
// imported- 兜底目录由 ListInstalled 的 importedRank 闸沉底）。
func (s *HeuKmsService) ListInstalledVersions() ([]version.HeuVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// heukmsInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：本模块是 RAR5
// 形态，verify（官方摘要必检）由内核 Fetch 折进 download 步、PE 三重闸与落位
// 并入 place 步，模块进度词表不单独可见，如实不造幻影步骤。
var heukmsInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 后台下载并安装指定版本：立即返回 "started"，全程经事件
// heukms:version-download 推送进度；同时开一笔 journal 托管事务（install 首装 /
// update 向已托管工具链追加版本，managed-declarative 资产形态）——journal 先落盘
// 再副作用，阶段迁移逐步 Advance，收口经观察面 Handle 自动落账并广播
// operation:changed（与既有模块事件双通道并行，markeron/ccswitch/litemonitor 同构）。
func (s *HeuKmsService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeToken(targetVersion)

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	// 已安装则直接返回，避免重复下载
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(normalizeToken(v.Version), targetVersion) {
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
				app.Event.Emit("heukms:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error(ID, "HEU 版本下载失败", fmt.Sprintf("HEU KMS Activator %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/heukms")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, heukmsInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("heukms:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error(ID, "HEU 版本下载失败", fmt.Sprintf("HEU KMS Activator %s 事务开启失败: %v", targetVersion, terr), "/ext/heukms")
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
			slog.Debug("heukms download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("heukms:version-download", p)
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
				notify.Success(ID, "HEU 版本下载成功", fmt.Sprintf("HEU KMS Activator %s 已安装", p.Version), "/ext/heukms")
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
				notify.Error(ID, "HEU 版本下载失败", fmt.Sprintf("HEU KMS Activator %s 下载失败: %v", targetVersion, err), "/ext/heukms")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error(ID, "HEU 版本下载失败", fmt.Sprintf("HEU KMS Activator %s 事务收口失败: %v", targetVersion, err), "/ext/heukms")
			return
		}
	}()

	return "started", nil
}

// ImportLocal 导入本地已取出的 HEU 主程序（挂载的 iso 根目录、解压目录或
// exe 所在目录皆可）。
//
// 运行中的实例拒绝导入：Windows 下运行中的 exe 被独占，拷贝必然失败。
func (s *HeuKmsService) ImportLocal(srcDir string) (version.HeuVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.HeuVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.HeuVersionInfo{}, fmt.Errorf("HEU KMS Activator 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.HeuVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// RemoveVersion 卸载指定版本。运行中的同版本拒绝（exe 被独占）。
func (s *HeuKmsService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	token := normalizeToken(targetVersion)
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(normalizeToken(snap.Version), token) {
		return fmt.Errorf("版本 %s 正在运行，请先退出", token)
	}
	if err := s.manager.Remove(token); err != nil {
		return err
	}
	// 卸载的是当前设定版本则清空，下次冷启动自动回退最新已装
	if s.store.GetActive() == token {
		_ = s.store.SetActive("")
	}
	return nil
}

// SetActiveVersion 设定使用版本（先校验已安装，再持久化）。
func (s *HeuKmsService) SetActiveVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	token := normalizeToken(targetVersion)
	if _, err := s.manager.ResolveExe(token); err != nil {
		return "", err
	}
	if err := s.store.SetActive(token); err != nil {
		return "", err
	}
	return token, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）。
func (s *HeuKmsService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir（explorer.exe <dir> 直启）；刻意不走 explorer.exe
// <file> 的"执行"语义（markeron「打开安装目录」按钮的事故教训）。
func (s *HeuKmsService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *HeuKmsService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：进程枚举拿外部 PID → EnumWindows 直操作唤窗；
//   - running：自有实例同样直操作窗口（**不走二次拉起信使**，理由见 instance 包）；
//   - stopped/failed：解析 active 版本直接无参启动。
func (s *HeuKmsService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（上游单实例契约未知，可能多开窗口）
		return ControlOutcome{Action: "starting", Message: "HEU KMS Activator 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		s.engine.RestoreExternalWindow(s.engine.ExternalPIDs())
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 HEU KMS Activator 窗口（若该实例是您以管理员身份启动的，未提权的 Hanxi 受 UIPI 隔离无法置其前台，请以管理员身份重启 Hanxi）"}, nil

	case instance.StateRunning:
		s.engine.RestoreWindow()
		return ControlOutcome{Action: "opened", Message: "已唤起 HEU KMS Activator 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			// requireAdministrator 清单：未提权的 Hanxi 直得 740。RPC 错误文案同样
			// 要含「管理员」——事件快照那条已由引擎改写，两条通道缺一都会让前端
			// 的一键提权重启按钮在某些展示路径上消失。
			if hint := instance.ElevateHint(err); hint != "" {
				return ControlOutcome{}, fmt.Errorf("%s", hint)
			}
			return ControlOutcome{}, fmt.Errorf("启动 HEU KMS Activator 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 HEU KMS Activator 就绪超时（%d 秒），请确认主程序未被安全软件拦截或删除", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("HEU KMS Activator %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 HEU 实例。
// external 状态不越权强杀（进程枚举不持句柄）：仅返回人性化指引。
func (s *HeuKmsService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 HEU KMS Activator 窗口内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "HEU KMS Activator 已退出"}, nil
}

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *HeuKmsService) Shutdown() error {
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
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 兜底。
func (s *HeuKmsService) shutdown() {
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

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，未设定/已失效
// 回退最新已装（versionCompare 自带 importedRank 沉底闸，兜底目录不当"最新"；
// ListInstalled 侧同闸双保险）。
func (s *HeuKmsService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未导入任何 HEU KMS Activator 版本，请在下方版本管理导入本地主程序")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// versionCompare 比较版本串（a>b 返回 1；相等 0；a<b 返回 -1）。
// 数值分段比较收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）；
// imported- 兜底目录属非规范形状，versioncmp 对它退化字典序且 'i' > 数字字符
// （"imported-…" 反被误判比 "9.9.9" 新），故由 importedRank 显式沉底：先比
// rank，同 rank 内再走数值分段比较。
func versionCompare(a, b string) int {
	na, nb := normalizeToken(a), normalizeToken(b)
	if ra, rb := importedRank(na), importedRank(nb); ra != rb {
		return rb - ra
	}
	return versioncmp.Compare(na, nb)
}

// importedRank 排序权重：正常语义版本 0，imported- 兜底目录 1（沉底）。
// 与 version.Manager.ListInstalled 内部闸同形（令牌一律 normalizeToken 归一后比较）。
func importedRank(version string) int {
	if strings.HasPrefix(version, "imported-") {
		return 1
	}
	return 0
}

// ---------- 联动开关与上游直达 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *HeuKmsService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *HeuKmsService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *HeuKmsService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *HeuKmsService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// OpenReleasePage 用默认浏览器打开指定版本（空=仓库 releases 总览）的发布页。
// 定位是**备援通道**：常规安装走「下载并安装」（rar 通道），但当某版本不在可安装
// 表里（无 rar 资产或上游未发布官方摘要）、或用户更想拿免密的 iso 自己取文件时，
// 发布页就是「导入本地」的第一跳——所以做成一键而不止是可复制的链接。
func (s *HeuKmsService) OpenReleasePage(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	token := normalizeToken(targetVersion)
	if token == "" {
		return s.plat.OpenURL(version.RepoURL() + "/releases")
	}
	// 优先用台账里的官方 html_url（含 tag 形状不规范的历史发布页），取不到再按 tag 拼。
	if releases, err := s.manager.ListRemote(); err == nil {
		for _, r := range releases {
			if r.Version == token && r.PageURL != "" {
				return s.plat.OpenURL(r.PageURL)
			}
		}
	}
	return s.plat.OpenURL(version.ReleasePagePrefix() + token)
}
