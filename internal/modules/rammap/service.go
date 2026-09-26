package rammap

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
	"hanxi/internal/modules/rammap/instance"
	"hanxi/internal/modules/rammap/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
	"hanxi/packages/go/externalquit"
)

// readyTimeout RAMMap 冷启动就绪上限：轻量 Win32 工具，首窗秒开，10s 富余。
const readyTimeout = 10 * time.Second

// RAMMapService 面向前端的 RAMMap 托管服务：官方直链版本管理（日期版模型）
// 与会话内启停。外部实例感知为拉取式（GetStatus 读取前复探）。
//
// 提权现实（#17 三重契约，本模块与 rufus/litemonitor/bcu 同族）：
// ① 未提权 Hanxi spawn 载荷必撞 740——引擎层改写为含"管理员"关键词的指引
//
//	文案（ElevateRestart 一键通道），RPC 错误如实上抛不谎报成功；
//
// ② 提权态 Hanxi 对同完整性 RAMMap 实例治理完整可用；对外部**自启且被
//
//	UAC 提权**的实例，medium-IL Hanxi 的 WM_CLOSE/Terminate 被 UIPI 拦 →
//	externalquit 如实回 blocked 降级指引；
//
// ③ 前端 stopped 引导行经 ElevationStatus RPC 预告提权要求（见该方法注释）。
type RAMMapService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *rammapStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	// isElevated 宿主提权态探测接缝（默认真实实现，单测注入）。
	isElevated func() bool

	downloadMu sync.Mutex
	downloads  map[string]struct{}
}

// NewRAMMapService 装配版本管理器、store 与实例引擎；构造无 IO。
func NewRAMMapService(plat platform.Platform, holder *extapi.LeaseHolder) *RAMMapService {
	paths := settings.GetPaths()
	svc := &RAMMapService{
		plat: plat, manager: version.NewManager(paths.VersionsDir()),
		store: newRammapStore(paths.StateDir()), downloads: make(map[string]struct{}),
		holder: holder, isElevated: windows.IsElevated,
	}
	svc.engine = instance.NewEngine(plat.Job(), plat.Process(), instance.NewRAMMapProbe(plat.Process()),
		instance.Callbacks{OnState: svc.emitInstanceState})
	return svc
}

// emitInstanceState 引擎状态回调：广播 "rammap:instance-state"，failed 态另发系统通知。
func (s *RAMMapService) emitInstanceState(snapshot instance.Snapshot) {
	slog.Debug("rammap instance state", "state", snapshot.State, "pid", snapshot.PID, "external", snapshot.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("rammap:instance-state", snapshot)
	}
	if snapshot.State == instance.StateFailed && snapshot.Error != "" {
		notify.Error("rammap", "RAMMap 实例异常", snapshot.Error, "/ext/rammap")
	}
}

// ---------- 版本管理 RPC ----------

// ListReleases 拉取"当前最新版"单条列表（HEAD 探测 Last-Modified 日期版）。
func (s *RAMMapService) ListReleases() ([]version.RammapRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 扫描本地已装版本目录。
func (s *RAMMapService) ListInstalledVersions() ([]version.RammapVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// rammapInstallSteps 托管资产事务的 journal 步骤词汇（verify 折进 download，
// 不造幻影步骤——家族口径）。
var rammapInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 异步下载最新版：立即返回 "started"；已装返回
// "already-installed"、同版本下载中返回 "in-progress"。进度经
// "rammap:version-download" 事件推送；全程持后台租约事务（P0 批 2b）。
// version 入参传空串 = 上游当前最新版。
func (s *RAMMapService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = strings.TrimSpace(targetVersion)
	releases, err := s.manager.ListRemote()
	if err != nil {
		return "", err
	}
	if len(releases) == 0 {
		return "", fmt.Errorf("官方源无可用版本")
	}
	if targetVersion == "" {
		targetVersion = releases[0].Version
	}
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, item := range installed {
			if strings.EqualFold(item.Version, targetVersion) {
				return "already-installed", nil
			}
		}
	}

	s.downloadMu.Lock()
	if _, ok := s.downloads[targetVersion]; ok {
		s.downloadMu.Unlock()
		return "in-progress", nil
	}
	s.downloads[targetVersion] = struct{}{}
	s.downloadMu.Unlock()

	opKind := extapi.OpInstall
	if err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate
	}
	txnID := uuid.NewString()

	go func() {
		defer func() {
			s.downloadMu.Lock()
			delete(s.downloads, targetVersion)
			s.downloadMu.Unlock()
		}()
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			notify.Error("rammap", "版本下载失败", fmt.Sprintf("RAMMap 后台租约开启失败: %v", lerr), "/ext/rammap")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, rammapInstallSteps)
		if terr != nil {
			lease.Release()
			s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			notify.Error("rammap", "版本下载失败", fmt.Sprintf("RAMMap 事务开启失败: %v", terr), "/ext/rammap")
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
			s.settleAndBroadcastProgress(targetVersion, p, s.emitDownloadProgress)
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
				notify.Success("rammap", "版本安装成功", fmt.Sprintf("RAMMap（%s）已成功安装", p.Version), "/ext/rammap")
			}
		}
		if err := s.manager.DownloadContext(txn.Context(), txnID, targetVersion, emit); err != nil {
			// N26 用户主动取消：按 2b 纪律如实收口，票面话术不露 ctx 原始错误；
			// 主动动作不发"失败"系统通知（前端票面已呈现「已取消」）。
			if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
				s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: "托管下载已取消"})
			} else {
				txn.Fail("asset-install-failed", err.Error())
				s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
				notify.Error("rammap", "版本下载失败", fmt.Sprintf("RAMMap 下载失败: %v", err), "/ext/rammap")
			}
			return
		}
		// done 事件发出前已在 settleAndBroadcastProgress 收口"首装自动设使用"，此处不再重复落账。
		if err := txn.Done(); err != nil {
			s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("rammap", "版本下载失败", fmt.Sprintf("RAMMap 事务收口失败: %v", err), "/ext/rammap")
			return
		}
	}()
	return "started", nil
}

// settleAndBroadcastProgress 下载回执"先落账、后广播"收口：done 成功回执若尚未
// 设定使用版本，先把刚落位成功的版本记为使用中，再把事件广播给前端——前端共享
// store 收到 done 即复刷版本区读 GetActiveVersion，事件先行于落账会让瞬时复刷读到
// 空值（"首个版本下载完不显示使用中"，与 termora/2ac9b3b 同根修）。manager 仅在
// 原子落位成功后才发 done，据此落账不会把失败版本记成使用中。广播以参数注入，
// 供单测锁死该时序。
func (s *RAMMapService) settleAndBroadcastProgress(targetVersion string, p version.DownloadProgress, broadcast func(version.DownloadProgress)) {
	if p.Stage == "done" && s.store.GetActive() == "" {
		_ = s.store.SetActive(targetVersion)
	}
	broadcast(p)
}

func (s *RAMMapService) emitDownloadProgress(p version.DownloadProgress) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("rammap:version-download", p)
	}
}

// ImportLocal 导入本机已有 RAMMap 目录为托管版本；未设使用版本时自动激活。
func (s *RAMMapService) ImportLocal(srcDir string) (version.RammapVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.RammapVersionInfo{}, gateErr
	}
	defer release()
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.RammapVersionInfo{}, err
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// RemoveVersion 删除本地版本；本会话运行中时拒绝（优先级最高）。
// "当前使用版本"通常不可卸载（先切走再卸），但它是唯一已装版本时放行——
// 否则用户被 guard 死锁，卸载成功后清空 active 回到"未指定"。
func (s *RAMMapService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = strings.TrimSpace(targetVersion)
	snapshot := s.engine.Snapshot()
	if (snapshot.State == instance.StateRunning || snapshot.State == instance.StateStarting || snapshot.State == instance.StateQuitting) &&
		strings.EqualFold(snapshot.Version, targetVersion) {
		return fmt.Errorf("版本 %s 正由本会话运行，请先退出进程", targetVersion)
	}
	removingActive := strings.EqualFold(s.store.GetActive(), targetVersion)
	if removingActive {
		if installed, err := s.manager.ListInstalled(); err != nil || len(installed) != 1 {
			return fmt.Errorf("当前使用版本 %s 不可卸载，请先选择其他版本", targetVersion)
		}
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	if removingActive {
		_ = s.store.SetActive("")
	}
	return nil
}

// SetActiveVersion 切换使用版本（ResolveExe 校验载荷在场后落盘）。
func (s *RAMMapService) SetActiveVersion(targetVersion string) (string, error) {
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

// GetActiveVersion 返回使用版本号（未设置为空串，error 恒 nil）。
func (s *RAMMapService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ---------- 实例控制 RPC ----------

// GetStatus 返回引擎状态快照；读取前做一次外部探针校正（拉取式感知）。
func (s *RAMMapService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// ElevationStatus 提权预告（三重契约之③）：静态 manifest 已知要求管理员；
// executionLevel 仍经平台读取器复核，读取失败回 unknown，740 运行时兜底不变。
func (s *RAMMapService) ElevationStatus() (StatusInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return StatusInfo{}, gateErr
	}
	defer release()
	level := string(windows.ExecutionRequireAdministrator)
	if v, err := s.activeExecutable(); err == nil {
		if parsed, perr := windows.ReadExecutionLevel(v); perr == nil {
			level = string(parsed)
		}
	}
	return StatusInfo{RequiresElevation: level == string(windows.ExecutionRequireAdministrator), HostElevated: s.isElevated(), ExecutionLevel: level}, nil
}

func (s *RAMMapService) activeExecutable() (string, error) {
	_, exe, err := s.resolveActiveVersion()
	return exe, err
}

// OpenWindowElevated 仅以管理员启动 RAMMap 目标，不把进程纳入 Hanxi JobObject。
// 这是 N22 A 路：Hanxi 保持普通权限，目标作为外部高权限实例存在；不返回 running
// 假账、不提供后续 Quit 托管承诺。B 路由现有 AppService.RestartElevated。
func (s *RAMMapService) OpenWindowElevated() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	if s.isElevated() {
		return ControlOutcome{}, fmt.Errorf("Hanxi 当前已是管理员权限运行，请直接启动 RAMMap")
	}
	v, exe, err := s.resolveActiveVersion()
	if err != nil {
		return ControlOutcome{}, err
	}
	result, err := windows.RunElevatedDetached(exe, filepath.Dir(exe), nil)
	if err != nil {
		return ControlOutcome{}, err
	}
	if result.Cancelled {
		return ControlOutcome{Action: "elevation-cancelled", Message: "已取消 UAC 授权，RAMMap 未启动"}, nil
	}
	if !result.Started {
		return ControlOutcome{}, fmt.Errorf("RAMMap 提权启动未完成")
	}
	return ControlOutcome{
		Action: "started-external-elevated", External: true, Elevated: true,
		Managed: false, CanQuit: false, LaunchMode: LaunchModeExternalElevated,
		Message: fmt.Sprintf("已单独以管理员权限启动 RAMMap %s；该实例不属于 Hanxi 托管范围，请在 RAMMap 窗口内关闭", v),
	}, nil
}

//   - running：聚焦自有实例主窗口；
//   - external：唤回用户自开窗口（RAMMap 窗口无最小化常驻语义，聚焦即达；
//     无可聚焦窗口如实回指引——二次拉起只会另开一个新窗，不是唤回，
//     不做"假装唤回"的把戏）；
//   - stopped/failed：解析 active 版本冷启动（多实例共存无障碍，无需
//     launch gate）。
func (s *RAMMapService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "RAMMap 正在启动中，请稍候"}, nil

	case instance.StateExternal:
		if s.engine.FocusExternal() {
			return ControlOutcome{Action: "external-focused", External: true, Managed: false, CanQuit: true, LaunchMode: LaunchModeExternal,
				Message: "已唤回正在运行的 RAMMap 窗口"}, nil
		}
		return ControlOutcome{Action: "external-unreachable", External: true, Managed: false, CanQuit: true, LaunchMode: LaunchModeExternal,
			Message: "检测到外部 RAMMap 实例但无可聚焦窗口；可再次「启动 RAMMap」由 Hanxi 另开一个托管实例"}, nil

	case instance.StateRunning:
		if !s.engine.Focus() {
			return ControlOutcome{Action: "starting", Message: "托管实例窗口尚未出现，请稍候片刻再试"}, nil
		}
		return ControlOutcome{Action: "focused", Managed: true, CanQuit: true, LaunchMode: LaunchModeManaged, Message: "已唤起 RAMMap 窗口"}, nil

	default:
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, err // 740 原始错误如实上抛；指引文案走状态广播（引擎暂扣改写层）
		}
		if !s.engine.WaitReady(readyTimeout) {
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 RAMMap 窗口就绪超时（%d 秒），请在版本管理重新安装后重试", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v)
		}
		return ControlOutcome{Action: "started", Managed: true, CanQuit: true, LaunchMode: LaunchModeManaged, Message: fmt.Sprintf("RAMMap %s 已启动", v)}, nil
	}
}

// Quit 退出实例：自有走分层退出（WM_CLOSE→宽限→Job 兜底）；外部按
// force-free 档（优雅优先，不生效直接强杀不打扰——零状态观察工具损失≈0；
// 自启且被提权的外部实例 UIPI 拦截，如实降级指引）。
func (s *RAMMapService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	if s.engine.ExternalRunning() {
		return s.quitExternal()
	}
	result, err := s.engine.Quit()
	out := QuitOutcome{Stopped: result.Stopped, Forced: result.Forced, CloseRequested: result.CloseRequested, Method: result.Method}
	if err != nil {
		return out, err
	}
	switch result.Method {
	case "not-managed":
		out.Message = "当前 Hanxi 会话没有可退出的 RAMMap 实例"
	case "close-request":
		out.Message = "RAMMap 已在关闭请求后退出"
	case "already-exited":
		out.Message = "RAMMap 已经退出"
	case "forced-job", "forced-process":
		out.Message = "RAMMap 未响应关闭请求，已强制结束本会话启动的实例（观察工具无状态损失）"
	default:
		out.Message = "RAMMap 退出操作已完成"
	}
	return out, nil
}

// quitExternal 外部实例 force-free 档执行（N3 终裁：低损档有优雅先试、
// 不生效直杀不弹窗；提权目标如实 blocked 降级）。
func (s *RAMMapService) quitExternal() (QuitOutcome, error) {
	res, err := s.engine.QuitExternal(context.Background(), externalquit.PolicyForceFree, "", nil)
	out := QuitOutcome{Stopped: res.Stopped, Forced: res.Forced, CloseRequested: res.CloseRequested,
		Method: res.Method, External: true}
	switch res.Method {
	case externalquit.MethodGraceful:
		out.Message = "外部自行启动的 RAMMap 已响应关闭请求退出"
	case externalquit.MethodAlreadyGone:
		out.Message = "外部 RAMMap 实例已经退出"
	case externalquit.MethodForced:
		out.Message = "外部 RAMMap 未响应关闭请求，已强制结束（观察工具无状态损失）"
	case externalquit.MethodBlocked:
		out.Message = "外部 RAMMap 以管理员权限运行，当前 Hanxi 无法代为终止——请以管理员身份重新启动 Hanxi，或在其窗口右上角关闭"
	case externalquit.MethodProbeMissingPID:
		out.Message = "检测到外部自行启动的 RAMMap，但未能取得其实例身份，已在操作前拒绝——请在其窗口右上角关闭"
	case externalquit.MethodDeclined:
		out.Message = "已取消退出（外部实例保持运行）"
	}
	return out, err
}

// ---------- 联动与辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *RAMMapService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *RAMMapService) SetFollowOnExit(next bool) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	if err := s.store.SetFollowOnExit(next); err != nil {
		return "", err
	}
	if next {
		return "已开启：Hanxi 退出时一并关闭托管实例", nil
	}
	return "已关闭：Hanxi 退出不影响 RAMMap（下次启动生效）", nil
}

// Shutdown RPC：经调用门执行收尾（与内部版 shutdown 同语义）。
func (s *RAMMapService) Shutdown() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.shutdown()
	return nil
}

// shutdown 装配布线：联动开启才强杀自有实例（外部实例永不受影响）。
func (s *RAMMapService) shutdown() {
	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop()
	}
}

// OpenDir 用资源管理器打开版本目录（先校验存在）。
func (s *RAMMapService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	dir = strings.TrimSpace(dir)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

// OfficialSiteURL / OpenOfficialSite 官方工具页入口。
func (s *RAMMapService) OfficialSiteURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.SiteURL(), nil
}

// OpenOfficialSite 在默认浏览器打开官方工具页。
func (s *RAMMapService) OpenOfficialSite() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.SiteURL())
}

// ---------- 版本解析 ----------

// resolveActiveVersion 解析启动目标：active 可用直用（损坏清空回退），否则取
// 已装中 versioncmp 最高者（日期令牌字典序=时间序，versioncmp 非数字段退化
// 字典序恰与其兼容）。
func (s *RAMMapService) resolveActiveVersion() (string, string, error) {
	if active := s.store.GetActive(); active != "" {
		if exe, err := s.manager.ResolveExe(active); err == nil {
			return active, exe, nil
		}
		_ = s.store.SetActive("")
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", err
	}
	if len(installed) == 0 {
		return "", "", fmt.Errorf("尚未安装 RAMMap，请先下载或导入")
	}
	sort.SliceStable(installed, func(i, j int) bool {
		return versioncmp.Compare(installed[i].Version, installed[j].Version) > 0
	})
	return installed[0].Version, installed[0].ExePath, nil
}

// iconSourceExe 供集中式 RuntimeIconService 取本机图标提取源（extapi.
// IconSourceProvider 契约的 service 侧实现，Module 层转发）：与冷启动同源
// 的活动载荷路径，只读无副作用；未安装如实上抛，由服务落负缓存。
func (s *RAMMapService) iconSourceExe() (string, error) {
	_, exe, err := s.resolveActiveVersion()
	return exe, err
}
