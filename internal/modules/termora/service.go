package termora

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
	"hanxi/internal/modules/termora/instance"
	"hanxi/internal/modules/termora/version"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/settings"
	"hanxi/packages/go/externalquit"
)

// readyTimeout Termora 冷启动就绪上限：JVM + Swing 首窗（含插件加载）慢于
// 原生家族，30s 保守上限（就绪判据=单实例互斥体在位，见 instance 包）。
const readyTimeout = 30 * time.Second

// externalQuitRisk confirm-force 档的知情文案（N3 终裁"打扰类"点名 Termora：
// 中断有实际损失——活动 SSH 会话断开、终端现场丢失；确认框与回执共用同一事实源）。
const externalQuitRisk = "该 Termora 是在 Hanxi 之外自行启动的：优雅退出未生效，强制结束将立即断开其全部活动 SSH 会话并丢失终端现场。"

// TermoraService 面向前端的 Termora 托管服务：GitHub releases 版本管理、
// 本地导入与会话内启停。外部实例感知为拉取式（GetStatus 读取前复探，
// snipaste/windterm 同型——引擎状态回调即时推送，无需后台轮询 goroutine）。
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type TermoraService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *termoraStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	downloadMu sync.Mutex
	downloads  map[string]struct{} // 进行中下载的版本号（允许不同版本并行）
}

// NewTermoraService 装配版本管理器、store 与实例引擎；构造无 IO。
func NewTermoraService(plat platform.Platform, holder *extapi.LeaseHolder) *TermoraService {
	paths := settings.GetPaths()
	svc := &TermoraService{
		plat: plat, manager: version.NewManager(paths.VersionsDir()),
		store: newTermoraStore(paths.StateDir()), downloads: make(map[string]struct{}),
		holder: holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), plat.Process(), instance.NewTermoraProbe(plat.Process()),
		instance.Callbacks{OnState: svc.emitInstanceState})
	return svc
}

// emitInstanceState 引擎状态回调：广播 "termora:instance-state" 事件，
// failed 态另发系统通知。
func (s *TermoraService) emitInstanceState(snapshot instance.Snapshot) {
	slog.Debug("termora instance state", "state", snapshot.State, "pid", snapshot.PID, "external", snapshot.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("termora:instance-state", snapshot)
	}
	if snapshot.State == instance.StateFailed && snapshot.Error != "" {
		notify.Error("termora", "Termora 实例异常", snapshot.Error, "/ext/termora")
	}
}

// ---------- 版本管理 RPC ----------

// ListReleases 拉取 GitHub releases 列表（10 分钟缓存；stable+beta 全量，
// 预发布行由面板徽标如实呈现）。
func (s *TermoraService) ListReleases() ([]version.TermoraRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 扫描本地已装版本目录。
func (s *TermoraService) ListInstalledVersions() ([]version.TermoraVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// termoraInstallSteps 托管资产事务的 journal 步骤词汇：zip 形态，verify
// （官方摘要双核）由内核 Fetch 折进 download 步，不造幻影步骤（markeron 同口径）。
var termoraInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 异步下载：立即返回 "started"；已装返回 "already-installed"、
// 同版本下载中返回 "in-progress"（不报错，前端按状态渲染）。进度经
// "termora:version-download" 事件推送；全程持后台租约事务（P0 批 2b）——
// 模块停用/应用退出即时取消，journal 以 operation-cancelled 收口。
// 首个版本装完自动设为使用版本。
func (s *TermoraService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
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
			notify.Error("termora", "版本下载失败", fmt.Sprintf("Termora %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/termora")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, ID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, termoraInstallSteps)
		if terr != nil {
			lease.Release()
			s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			notify.Error("termora", "版本下载失败", fmt.Sprintf("Termora %s 事务开启失败: %v", targetVersion, terr), "/ext/termora")
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
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("termora:version-download", p)
			}
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
				notify.Success("termora", "版本安装成功", fmt.Sprintf("Termora %s 已成功安装", p.Version), "/ext/termora")
			}
		}
		if err := s.manager.DownloadContext(txn.Context(), txnID, targetVersion, emit); err != nil {
			txn.Fail("asset-install-failed", err.Error())
			s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("termora", "版本下载失败", fmt.Sprintf("Termora %s 下载失败: %v", targetVersion, err), "/ext/termora")
			return
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(targetVersion)
		}
		if err := txn.Done(); err != nil {
			s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("termora", "版本下载失败", fmt.Sprintf("Termora %s 事务收口失败: %v", targetVersion, err), "/ext/termora")
			return
		}
	}()
	return "started", nil
}

func (s *TermoraService) emitDownloadProgress(p version.DownloadProgress) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("termora:version-download", p)
	}
}

// ImportLocal 导入本机已有 Termora 便携目录为托管版本（data\ 会话与密钥
// 数据随目录收纳）；未设使用版本时自动激活导入结果。
func (s *TermoraService) ImportLocal(srcDir string) (version.TermoraVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.TermoraVersionInfo{}, gateErr
	}
	defer release()
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.TermoraVersionInfo{}, err
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// RemoveVersion 删除本地版本；本会话正在运行该版本、或该版本为"当前使用
// 版本"时拒绝。
func (s *TermoraService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	snapshot := s.engine.Snapshot()
	if (snapshot.State == instance.StateRunning || snapshot.State == instance.StateStarting || snapshot.State == instance.StateQuitting) &&
		strings.EqualFold(snapshot.Version, targetVersion) {
		return fmt.Errorf("版本 %s 正由本会话运行，请先退出进程", targetVersion)
	}
	if strings.EqualFold(s.store.GetActive(), targetVersion) {
		return fmt.Errorf("当前使用版本 %s 不可卸载，请先选择其他版本", targetVersion)
	}
	return s.manager.Remove(targetVersion)
}

// SetActiveVersion 切换使用版本；ResolveExe 确认 payload 在场后才落盘。
func (s *TermoraService) SetActiveVersion(targetVersion string) (string, error) {
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

// GetActiveVersion 返回使用版本号，未设置时为空串（error 恒 nil，统一前端签名）。
func (s *TermoraService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ---------- 实例控制 RPC ----------

// GetStatus 返回引擎状态快照；读取前做一次外部探针校正（瞬时枚举，
// 内核在运行/启动/退出中自行短路——external 感知为拉取式，前端定时刷新即得）。
func (s *TermoraService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢（单实例信使家族契约，见 instance 包注释）：
//   - running/external：无参二次拉起信使（上游激活通道，自有/外部通用），
//     取在跑实例实测路径；路径不可得时降级按 PID 聚焦（自有）或如实指引；
//   - 静止态：先过外部在场门禁（信使语义下冷启动撞外部实例只会"抬轿后
//     自动退出"，托管根本建立不了——如实拒并给处置路径），再补 portable
//     激活器目录、解析 active 版本冷启动托管实例。
func (s *TermoraService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "Termora 正在启动中，请稍候"}, nil

	case instance.StateRunning, instance.StateExternal:
		exe := snap.ExePath
		if exe == "" {
			if snap.State == instance.StateRunning && s.engine.Focus() {
				return ControlOutcome{Action: "focused", Message: "已唤起 Termora 窗口"}, nil
			}
			return ControlOutcome{Action: "external-unreachable", External: snap.External,
				Message: "检测到 Termora 实例但未能取得其程序路径，无法代发激活请求；请在其窗口/托盘图标操作"}, nil
		}
		if err := s.engine.Messenger(exe); err != nil {
			return ControlOutcome{}, err
		}
		if snap.State == instance.StateExternal {
			return ControlOutcome{Action: "external-activated", External: true,
				Message: "已向外部 Termora 实例发出窗口激活请求（上游单实例转发通道）"}, nil
		}
		return ControlOutcome{Action: "activated", Message: "已唤起 Termora 窗口"}, nil

	default:
		if s.engine.ExternalRunning() {
			return ControlOutcome{}, fmt.Errorf("检测到 Hanxi 之外自行启动的 Termora（单实例应用会转发让位，托管启动建立不起来）：可用「打开窗口」唤起其窗口，或先「退出」该实例再托管启动")
		}
		v, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		// portable 激活器：exe 同级 data\ 存在即数据自包含（上游绿色版实证
		// 语义）；下载链已在落位时建过，此处幂等补齐导入件的缺省场景。
		if err := os.MkdirAll(filepath.Join(filepath.Dir(exe), "data"), 0755); err != nil {
			return ControlOutcome{}, fmt.Errorf("创建 Termora 数据目录失败: %w", err)
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 Termora 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 Termora 就绪超时（%d 秒，JVM 首启较慢），请在版本管理重新安装后重试", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(v)
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("Termora %s 已启动", v)}, nil
	}
}

// Quit 退出实例（confirm 为 confirm-force 档授权位，vscode 安装闸同款往返）：
//   - 外部实例：首入返回 confirm-required + 风险文案，前端全局确认框后携
//     confirm=true 重入执行（优雅 WM_CLOSE → 验证 → 同意后强杀）；提权目标
//     UIPI 拦截如实降级指引；身份不明（探针无 PID）保守回指引；
//   - 自有实例：分层退出（WM_CLOSE → 宽限 → JobObject 兜底）——退出钮本身
//     即用户第一人称"明确要关"，不再二次弹窗。
func (s *TermoraService) Quit(confirm bool) (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	if s.engine.ExternalRunning() {
		return s.quitExternal(confirm)
	}
	result, err := s.engine.Quit()
	out := QuitOutcome{Stopped: result.Stopped, Forced: result.Forced, CloseRequested: result.CloseRequested, Method: result.Method}
	if err != nil {
		return out, err
	}
	switch result.Method {
	case "not-managed":
		out.Message = "当前 Hanxi 会话没有可退出的 Termora 实例"
	case "close-request":
		out.Message = "Termora 已在关闭请求后退出（会话数据已随其落盘）"
	case "already-exited":
		out.Message = "Termora 已经退出"
	case "forced-job", "forced-process":
		out.Message = "Termora 未响应关闭请求（可能存在挂起的确认框），已强制结束本会话启动的实例；其活动 SSH 会话随之断开"
	default:
		out.Message = "Termora 退出操作已完成"
	}
	return out, nil
}

// quitExternal 外部实例的 confirm-force 档执行（N3 终裁：会话类工具"中断有
// 实际损失"，强杀必须经用户明确同意；取消=不越权，回指引）。
func (s *TermoraService) quitExternal(confirm bool) (QuitOutcome, error) {
	snap := s.engine.Snapshot()
	if snap.PID == 0 {
		// 探针在场但枚举不到 PID（身份查询受限）：保守回指引，不猜身份。
		return QuitOutcome{Stopped: false, External: true, Method: "probe-missing-pid",
			Message: "检测到外部自行启动的 Termora，但未能取得其实例身份，已在操作前拒绝——请在其窗口内退出"}, nil
	}
	if !confirm {
		return QuitOutcome{External: true, Action: "confirm-required", Method: "confirm-required",
			Risk: externalQuitRisk, Message: externalQuitRisk}, nil
	}
	res, err := s.engine.QuitExternal(context.Background(), externalquit.PolicyConfirmForce, externalQuitRisk,
		func(string) bool { return true })
	out := QuitOutcome{External: true, Stopped: res.Stopped, Forced: res.Forced,
		CloseRequested: res.CloseRequested, Method: res.Method}
	switch res.Method {
	case externalquit.MethodGraceful:
		out.Message = "外部自行启动的 Termora 已响应关闭请求优雅退出（会话由其自行收口）"
	case externalquit.MethodAlreadyGone:
		out.Message = "外部 Termora 实例已经退出"
	case externalquit.MethodForced:
		out.Message = "外部 Termora 未响应优雅退出，已按你的确认强制结束——其活动 SSH 会话已被断开"
	case externalquit.MethodBlocked:
		out.Message = "外部 Termora 以管理员权限运行，hanxi 无法代为终止，请在其窗口内退出"
	case externalquit.MethodDeclined:
		out.Message = "已取消退出（外部实例保持运行）"
	}
	return out, err
}

// ---------- 联动与辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *TermoraService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *TermoraService) SetFollowOnExit(next bool) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	if err := s.store.SetFollowOnExit(next); err != nil {
		return "", err
	}
	if next {
		return "已开启：Hanxi 退出时一并关闭托管实例（含其活动会话）", nil
	}
	return "已关闭：Hanxi 退出不影响 Termora，托管实例继续独立运行（下次启动生效）", nil
}

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *TermoraService) Shutdown() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.shutdown()
	return nil
}

// shutdown 装配布线：Go 直调路径，不得依赖运行态（见 ADR-0001 Wave 3 注记）。
// 模块停用/应用退出：联动开启才强杀自有实例（外部实例永不受影响）。
func (s *TermoraService) shutdown() {
	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop()
	}
}

// OpenDir 用资源管理器打开版本目录（先校验存在，explorer 自身不报错）。
func (s *TermoraService) OpenDir(dir string) error {
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

// RepositoryURL 上游仓库地址（error 恒 nil，统一前端签名）。
func (s *TermoraService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 在默认浏览器打开上游仓库。
func (s *TermoraService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// ---------- 版本解析 ----------

// resolveActiveVersion 解析启动目标：active 可用则直用（损坏自动清空回退），
// 否则取已装版本中 versioncmp 最高者（prerelease 段错序罕见性见
// version.ListInstalled 排序注记；activeVersion 显式设定是主路径）。
func (s *TermoraService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装 Termora，请先下载或导入一个免安装版本")
	}
	sort.SliceStable(installed, func(i, j int) bool {
		return versioncmp.Compare(installed[i].Version, installed[j].Version) > 0
	})
	return installed[0].Version, installed[0].ExePath, nil
}

// normalizeVersion 版本入参归一（补 v 前缀，与 version 包展示口径统一）。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") || strings.HasPrefix(v, "imported-") {
		return v
	}
	return "v" + v
}
