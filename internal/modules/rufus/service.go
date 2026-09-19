package rufus

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/rufus/instance"
	"hanxi/internal/modules/rufus/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	readyTimeout  = 15 * time.Second // 冷启动就绪上限（进程出现；Rufus 单文件 exe 拉起即弹主对话框）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔
)

// RufusService 向前端暴露 Rufus 版本管理与实例控制能力。
// 启动盘制作（选盘/分区方案/文件系统/镜像写入）全部在上游 GUI 完成，
// 不做任何内嵌（纯托管决策——磁盘级写入的完整确认交互链就是产品本体，
// 见 package rufus 注释）。
//
// 与 litemonitor 引擎的适配差异（上游契约侦查结论，详见 instance 包注释）：
//   - 单实例互斥体 Global\Rufus 名称固定 → 存活探测以互斥体为权威、进程枚举供 PID；
//   - 第二实例弹系统模态错误框而非静默退出 → 唤窗同样只能 Win32 直操作；
//   - 便携模式经预置 rufus.ini 激活（顺带永久关闭上游更新检查）；
//   - 不做空闲自动退出：Rufus 是"用完即关窗"的对话框应用，无驻留形态，
//     进程生命周期与窗口天然同生共死，空闲退出语义不成立。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type RufusService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *rufusStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewRufusService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewRufusService(plat platform.Platform, holder *extapi.LeaseHolder) *RufusService {
	paths := settings.GetPaths()
	svc := &RufusService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newRufusStore(paths.StateDir()),
		holder:  holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewRufusProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 rufus:instance-state。
func (s *RufusService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("rufus instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("rufus:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("rufus", "Rufus 实例异常", snap.Error, "/ext/rufus")
	}
}

// activate 启动后台外部实例感知：5s 轮询互斥体/进程存在性校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
func (s *RufusService) activate() {
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
func (s *RufusService) ListReleases() ([]version.RufusRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表（降序，imported 兜底目录沉底）。
func (s *RufusService) ListInstalledVersions() ([]version.RufusVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// rufusInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：rufus 是单文件
// exe 形态，unpack 步不存在（不造幻影步骤），verify 对应模块的 MZ 魔数落地断言
// （官方摘要双核由内核 Fetch 折进 download 步内完成）。
var rufusInstallSteps = []string{"download", "verify", "place"}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 rufus:version-download
// 推送进度；同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链
// 追加版本，managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段
// 迁移逐步 Advance，收口经观察面 Handle 自动落账并广播 operation:changed
// （与既有模块事件双通道并行，Wave 4-B 接线，markeron 同构）。
func (s *RufusService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
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
	opKind := extapi.OpInstall
	if err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate
	}
	txnID := uuid.NewString()

	go func() {
		txn, terr := ops.BeginTxn(ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, rufusInstallSteps)
		if terr != nil {
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("rufus:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("rufus", "版本下载失败", fmt.Sprintf("Rufus %s 事务开启失败: %v", targetVersion, terr), "/ext/rufus")
			return
		}
		stepIdx := -1
		emit := func(p version.DownloadProgress) {
			slog.Debug("rufus download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("rufus:version-download", p)
			}
			// journal 只在阶段迁移处落盘（§8.2 每步迁移即持久化）；
			// 下载分块进度仅进观察面内存投影，不产生 fsync 风暴
			switch p.Stage {
			case "downloading":
				if stepIdx < 0 {
					stepIdx = 0
					txn.Step(stepIdx)
				}
				txn.Progress(p.Done, p.Total)
			case "verify":
				if stepIdx < 1 {
					stepIdx = 1
					txn.Step(stepIdx)
				}
			case "install":
				if stepIdx < 2 {
					stepIdx = 2
					txn.Step(stepIdx)
				}
			}
			if p.Stage == "done" {
				notify.Success("rufus", "版本下载成功", fmt.Sprintf("Rufus %s 已成功安装", p.Version), "/ext/rufus")
			}
		}
		if err := s.manager.Download(txnID, targetVersion, emit); err != nil {
			txn.Fail("asset-install-failed", err.Error())
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("rufus", "版本下载失败", fmt.Sprintf("Rufus %s 下载失败: %v", targetVersion, err), "/ext/rufus")
			return
		}
		txn.Done()
		// 未设使用版本时自动把刚下载完的版本设为使用版本：
		// 首个版本下载完成后无需再手动点一下设置（与 snipaste 既有行为对齐）。
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(targetVersion)
		}
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
func (s *RufusService) RemoveVersion(targetVersion string) error {
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
	// 卸载的是当前设定版本则清空，下次冷启动自动回退最新已装
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("")
	}
	return nil
}

// SetActiveVersion 设定使用版本（先校验已安装，再持久化）。
func (s *RufusService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *RufusService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的 Rufus 便携 exe（文件路径或所在目录均可；
// 源旁随行 rufus.ini 一并搬运保住便携配置）。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 文件被独占，拷贝必然失败。
func (s *RufusService) ImportLocal(srcPath string) (version.RufusVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.RufusVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.RufusVersionInfo{}, fmt.Errorf("Rufus 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcPath))
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 刻意不复用 AppService.OpenPath：其 explorer.exe <file> 语义在文件对象上是"执行"
// 而非"打开"（markeron「打开安装目录」按钮的事故教训：传 exe 路径直接启动了程序）。
// 这里入参恒为目录，语义安全，但仍走本模块自有实现保持行为显式。
func (s *RufusService) OpenDir(dir string) error {
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

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *RufusService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢：
//   - external：进程枚举拿外部 PID → EnumWindows SW_RESTORE+SetForegroundWindow
//     直接唤窗（Rufus 二次启动弹"已在运行"模态错误框，信使路径有害无益）；
//   - running：自有实例同样直操作窗口；
//   - stopped/failed：解析 active 版本直接无参启动（启动即显示主对话框）。
func (s *RufusService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		// 前序 Start 仍在临界区：不做二次拉起（会触发上游单实例竞速弹错误框）
		return ControlOutcome{Action: "starting", Message: "Rufus 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		s.engine.RestoreExternalWindow(s.engine.ExternalPIDs())
		return ControlOutcome{Action: "external-opened", External: true,
			Message: "已唤起外部运行中的 Rufus 窗口"}, nil

	case instance.StateRunning:
		s.engine.RestoreWindow()
		return ControlOutcome{Action: "opened", Message: "已唤起 Rufus 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 Rufus 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 Rufus 就绪超时（%d 秒），请确认 Hanxi 正以管理员身份运行，或尝试重新安装该版本", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("Rufus %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 Rufus（WM_CLOSE 优雅 + 宽限强杀兜底）。
// external 状态不越权强杀（进程枚举不持句柄）：仅返回人性化指引。
func (s *RufusService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，请在 Rufus 窗口内直接关闭"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "Rufus 已退出"}, nil
}

// Shutdown（RPC 导出版，纯 void）：取得调用门租约后转发内部 shutdown()，
// 拒绝即早退——Wave 3 口径：void 方法不改签名。前端当前不调用本方法，
// 但它属绑定面，必须经门收口。
func (s *RufusService) Shutdown() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.shutdown()
}

// shutdown 模块停用/应用退出：停后台轮询 + 终止自有实例。
// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)。
// 外部实例不受影响（非我方托管）；自有实例另受 JobObject KILL_ON_JOB_CLOSE 内核兜底
// （开关"独立运行"解除联动时除外）。
func (s *RufusService) shutdown() {
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

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，
// 未设定/已失效回退最新已装（ListInstalled 已保证降序且 imported 兜底沉底）。
func (s *RufusService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装任何 Rufus 版本，请先在版本管理下载或导入")
	}
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// ---------- 桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *RufusService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *RufusService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *RufusService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *RufusService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}
