package paseo

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
	"hanxi/internal/modules/modpath"
	"hanxi/internal/modules/paseo/instance"
	"hanxi/internal/modules/paseo/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	// ChannelStable / ChannelBeta 更新通道常量。上游 beta 线（v0.8.0-beta.1 等）
	// 是带完整资产的预发布真实版本，但属"尝鲜"性质——默认 stable，beta 必须用户主动切换。
	ChannelStable = "stable"
	ChannelBeta   = "beta"

	readyTimeout  = 45 * time.Second // 冷启动就绪上限（Electron 主窗口出现；内置 daemon 随主进程拉起，比裸 Electron 应用放宽）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	// electronDataDirName / daemonDirName 上游用户数据目录字面量：
	// Electron 数据恒在 %APPDATA%\Paseo，daemon 数据恒在 ~/.paseo（module.go 包注释实证）。
	electronDataDirName = "Paseo"
	daemonDirName       = ".paseo"
)

// PaseoService 向前端暴露 Paseo 版本管理与窗口唤起能力。
// agent 编排操作不内嵌：打开 Paseo 自有窗口完成（界面完整、协议在 daemon 侧，
// 内嵌重做性价比为零——决策记录见 module.go 包注释）。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type PaseoService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *paseoStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	// download 下载→落位链的函数接缝：生产装配指向 manager.DownloadContext，
	// 测试注入假实现以隔离网络驱动 emit 收口时序（eartrumpet 注入接缝同法）。
	download func(ctx context.Context, txnID, targetVersion string, emit func(version.DownloadProgress)) error

	downloadMu sync.Mutex
	downloads  map[string]struct{} // 在途下载版本集（防同版本并发触发双链）
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

// NewPaseoService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewPaseoService(plat platform.Platform, holder *extapi.LeaseHolder) *PaseoService {
	paths := settings.GetPaths()
	svc := &PaseoService{
		plat:      plat,
		manager:   version.NewManager(paths.VersionsDir()),
		store:     newPaseoStore(paths.StateDir()),
		downloads: make(map[string]struct{}),
		holder:    holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewPaseoProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	svc.download = svc.manager.DownloadContext
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 paseo:instance-state。
func (s *PaseoService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("paseo instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("paseo:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("paseo", "Paseo 实例异常", snap.Error, "/ext/paseo")
	}
}

// activate 启动后台外部实例感知：5s 轮询进程名校正 external/stopped。
// （自有实例的存活由引擎 hold 的进程句柄感知，不需要轮询。）
// 刻意不做空闲自动退出：Paseo 是 daemon 宿主——无窗运行 ≠ 空闲，
// 其上的 coding agent 会话可能正在进行，杀进程即毁会话（rustdesk 先例：
// 服务型常驻禁用空闲退出）。
func (s *PaseoService) activate() {
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

// GetReleaseChannel 返回当前更新通道（stable/beta）。
func (s *PaseoService) GetReleaseChannel() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetReleaseChannel(), nil
}

// SetReleaseChannel 切换更新通道（beta 为上游预发布，资产完整但属尝鲜性质）。
func (s *PaseoService) SetReleaseChannel(channel string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	if err := s.store.SetReleaseChannel(channel); err != nil {
		return "", err
	}
	return channel, nil
}

// ListReleases 获取当前通道可用版本（多镜像回退，10 分钟缓存；
// 数据源为 GitHub releases，无本机架构 win zip 或无官方 digest 的版本不入列表）。
func (s *PaseoService) ListReleases() ([]version.PaseoRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote(s.store.GetReleaseChannel() == ChannelBeta)
}

// ListInstalledVersions 获取本地托管已装版本（多版本目录并存，新→旧排序）。
func (s *PaseoService) ListInstalledVersions() ([]version.PaseoVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	list, err := s.manager.ListInstalled()
	if err != nil {
		return nil, err
	}
	sortInstalled(list)
	return list, nil
}

// paseoInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：paseo 是
// electron unpacked zip 形态，verify 对应内核 Fetch 的官方摘要双核完成点
// （模块进度词表既有 verify 阶段的真实迁移，如实进步骤不造幻影）。
var paseoInstallSteps = []string{"download", "verify", "unpack", "place"}

// DownloadVersion 后台下载官方 win zip 并保布局解压到 versions/paseo_<ver>/：
// 立即返回，全程经事件 paseo:version-download 推送进度；同时开一笔 journal
// 托管事务（install 首装 / update 向已托管工具链追加版本，managed-declarative
// 资产形态）——journal 先落盘再副作用，进度阶段迁移逐步 Advance，收口经观察面
// Handle 自动落账并广播 operation:changed（与既有模块事件双通道并行，
// Wave 4-B 接线，markeron/ccswitch 同构）。
// 运行中不拒绝：解压目标是独立的新版本目录，不触碰在跑实例的文件。
func (s *PaseoService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")

	s.downloadMu.Lock()
	if _, ok := s.downloads[targetVersion]; ok {
		s.downloadMu.Unlock()
		return "in-progress", nil
	}

	// 已安装则直接返回，避免重复下载
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(strings.TrimPrefix(v.Version, "v"), targetVersion) {
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
				app.Event.Emit("paseo:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error("paseo", "版本下载失败", fmt.Sprintf("Paseo %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/paseo")
			return
		}
		defer lease.Release()
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, paseoInstallSteps)
		if terr != nil {
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("paseo:version-download", version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error("paseo", "版本下载失败", fmt.Sprintf("Paseo %s 事务开启失败: %v", targetVersion, terr), "/ext/paseo")
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
			slog.Debug("paseo download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("paseo:version-download", p)
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
			case "extract":
				if stepIdx < 2 {
					stepIdx = 2
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
			}
			if p.Stage == "done" {
				notify.Success("paseo", "安装成功", fmt.Sprintf("Paseo %s 已解压进托管目录", p.Version), "/ext/paseo")
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
				notify.Error("paseo", "安装失败", fmt.Sprintf("Paseo %s 安装失败: %v", targetVersion, err), "/ext/paseo")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("paseo", "安装失败", fmt.Sprintf("Paseo %s journal 收口失败: %v", targetVersion, err), "/ext/paseo")
			return
		}
		// "首装自动设使用"已在 emit 闭包收口 done 时先于事件落账，此处不再重复。
	}()

	return "started", nil
}

// RemoveVersion 卸载指定托管版本（该版本正在运行则拒绝）。
// 用户数据（%APPDATA%\Paseo 与 ~/.paseo）恒不动——共享数据集成决策，
// 与 recordly"卸载保数据"先例一致。
func (s *PaseoService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning && snap.Version == targetVersion {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("") // 回退"自动最新已装"
	}
	return nil
}

// SetActiveVersion 指定启动使用的托管版本（须已安装）。
func (s *PaseoService) SetActiveVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定的使用版本（空串 = 自动最新已装）。
func (s *PaseoService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地 Paseo 程序目录（整套 Electron 目录拷入托管版本目录）。
// 数据恒在 %APPDATA%\Paseo 与 ~/.paseo（与 exe 位置无关），导入不搬数据。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 被独占，拷贝必然失败。
func (s *PaseoService) ImportLocal(srcDir string) (version.PaseoVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.PaseoVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.PaseoVersionInfo{}, fmt.Errorf("Paseo 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return info, err
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开指定目录（"打开位置"按钮）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe <dir> 直启；
// 刻意不走 explorer.exe <file> 的"执行"语义（markeron 事故教训）。
func (s *PaseoService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// OpenElectronDataDir 打开 Electron 数据目录 %APPDATA%\Paseo（窗口状态/桌面设置）。
// 共享数据决策下的直达入口：托管实例与自装实例同用此目录，只读导航不改写。
func (s *PaseoService) OpenElectronDataDir() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	dir, err := modpath.UserConfigDir(electronDataDirName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("Paseo Electron 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return windows.RevealDir(dir)
}

// OpenDaemonHome 打开 daemon 数据主目录 ~/.paseo（持久配置、实例注册、
// 会话与配对状态所在）。与 exe 位置无关，删托管版本不丢配对。
func (s *PaseoService) OpenDaemonHome() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	base := os.Getenv("USERPROFILE")
	if base == "" {
		base = os.Getenv("HOME")
	}
	if base == "" {
		return fmt.Errorf("无法定位用户主目录")
	}
	dir := filepath.Join(base, daemonDirName)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("Paseo daemon 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return windows.RevealDir(dir)
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *PaseoService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢。Paseo 上游 second-instance 语义是
// "再开一个新窗口"而非聚焦（main.ts 实证），故唤窗优先 Win32 直唤已有窗口，
// 仅无窗可用时才以二次拉起请求开新窗（与 recordly 信使唤窗族不同，决策见包注释）。
//   - external：共享数据同锁组下外部实例即全局唯一主实例，可直唤；
//   - running：自有实例直唤；
//   - stopped/failed：解析使用版本直接无参冷启动。
func (s *PaseoService) OpenWindow() (ControlOutcome, error) {
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
		return ControlOutcome{Action: "starting", Message: "Paseo 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		if s.engine.FocusWindow() {
			return ControlOutcome{Action: "external-focused", External: true,
				Message: "Paseo 实例正在运行（非 Hanxi 托管，共享数据），已唤起其窗口"}, nil
		}
		exe, err := s.resolveAnyInstalledExe()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.OpenMessenger(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("请求窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-messenger", External: true,
			Message: "运行中的 Paseo 实例无可见窗口（可能仅 daemon 在场），已请求其打开新窗口"}, nil

	case instance.StateRunning:
		if s.engine.FocusWindow() {
			return ControlOutcome{Action: "focused", Message: "已唤起 Paseo 窗口"}, nil
		}
		if err := s.engine.OpenMessenger(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("请求窗口失败: %w", err)
		}
		return ControlOutcome{Action: "messenger",
			Message: "Paseo 运行中但无可见窗口，已请求其打开新窗口（上游多窗口语义）"}, nil

	default:
		v, exe, err := s.resolveStartTarget()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{
			Version:  v,
			Exe:      exe,
			Detached: !s.store.GetFollowOnExit(),
		}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 Paseo 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			// 等待超时：优先读取引擎已记录的失败原因给出针对性提示
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 Paseo 主窗口就绪超时（%d 秒），请检查杀毒软件是否拦截后重试", int(readyTimeout/time.Second))
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("Paseo %s 已启动", v)}, nil
	}
}

// Quit 退出引擎托管的 Paseo。
// external 状态不越权强杀（进程名探测拿不到主进程 PID，且那是用户的实例）：
// 仅返回人性化指引。
func (s *PaseoService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的 Paseo 实例，请在 Paseo 窗口内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "Paseo 已退出"}, nil
}

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *PaseoService) Shutdown() error {
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
func (s *PaseoService) shutdown() {
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

// resolveStartTarget 冷启动目标（版本标签与 exe）：activeVersion 优先，
// 未设定/失效回退最新已装（并自愈清空）。
func (s *PaseoService) resolveStartTarget() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装 Paseo，请先在版本管理在线安装或导入本地副本")
	}
	sortInstalled(installed)
	_ = s.store.SetActive(installed[0].Version) // 首次冷启动将实际采用的版本回写
	return installed[0].Version, installed[0].ExePath, nil
}

// resolveAnyInstalledExe 外部态信使 exe（信使归属实例组由共享 user-data 决定，
// 与具体版本号无关——取最新已装即可）。
func (s *PaseoService) resolveAnyInstalledExe() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装任何 Paseo 托管版本，无法代为唤起外部实例窗口")
	}
	sortInstalled(installed)
	return installed[0].ExePath, nil
}

// sortInstalled 版本新→旧原地排序：规范 semver 优先（预发布规则），
// imported 时间戳目录恒排最后（其字典序大于数字版本，交给 Compare 会误判为最新）。
func sortInstalled(list []version.PaseoVersionInfo) {
	sort.SliceStable(list, func(i, j int) bool {
		ci, cj := version.IsCanonical(list[i].Version), version.IsCanonical(list[j].Version)
		switch {
		case ci && cj:
			return version.Compare(list[i].Version, list[j].Version) > 0
		case ci != cj:
			return ci
		default:
			return list[i].Version > list[j].Version
		}
	})
}

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *PaseoService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。开启后 Hanxi 退出将连带终止
// Paseo 及其 daemon 上正在运行的 agent 会话——UI 提示条必须如实预告。
func (s *PaseoService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
// 上游安装器才会建开始菜单项；便携托管下这是显式提供的入口。
func (s *PaseoService) CreateDesktopShortcut() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	_, exe, err := s.resolveStartTarget()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("Paseo", exe, filepath.Dir(exe))
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *PaseoService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *PaseoService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}
