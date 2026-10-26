package subnetdesk

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

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/modpath"
	"hanxi/internal/modules/subnetdesk/instance"
	"hanxi/internal/modules/subnetdesk/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

const (
	// readyTimeout 冷启动等待内层进程树出现的 RPC 上限：
	// 首启需解压 ~25MB 负载（引擎侧 startGrace=60s），超时不代表失败，
	// 页面状态会经事件自动纠正为 running。
	readyTimeout = 45 * time.Second
	// watchInterval 外部便携实例感知轮询间隔
	watchInterval = 5 * time.Second

	// dataDirName portable 形态默认写 %APPDATA%\SubnetDesk（module.go 包注释实证；与 RustDesk 数据目录相互独立）。
	dataDirName = "SubnetDesk"
)

// SubnetDeskService 向前端暴露 SubnetDesk 版本管理与启停/唤窗能力，双形态纳管：
//   - 便携版（隔离目录 packer exe）：全托管，JobObject 启停联动；
//   - 安装版（MSI 系统安装）：Hanxi 负责取包校验、发起上游安装向导、注册表
//     探测识别与客户端拉起/唤窗；Windows 服务不归 Hanxi 管辖（无人值守
//     的被控常驻正是它的价值），卸载引导至系统设置、不代执行。
//
// 远程桌面画面本身不内嵌（上游 GUI 完整，内嵌不可行）：
// 连接、局域网设置（用户名/密码/CIDR 白名单/端口 21118）均在 SubnetDesk 自有窗口操作。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type SubnetDeskService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *subnetdeskStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	// downloadMu 只保护"在飞下载名册"downloads 的读判与置位；去重靠名册本体
	// （旧实现只持锁跑完同步段即随 RPC 返回放锁，goroutine 下载全程在锁外——
	// 同版本连点照样并发开两路下载、两路 rename 同一目标、双 done 双通知。
	// quicklook ceceabd / translucenttb e65364d 名册先例同型修法：名册在飞即
	// 回 "in-progress" 不起第二路，goroutine 函数头 defer 销账，拒绝早退不粘闸）。
	// downloadDriver/installerDriver 下载驱动缝（生产恒 nil 走 manager 真实
	// Context 形态；单测注入阻塞桩锁死去重与租约记账时序，translucenttb 同法）。
	downloadMu      sync.Mutex
	downloads       map[string]struct{}
	downloadDriver  func(ctx context.Context, targetVersion string, emit func(version.DownloadProgress)) error
	installerDriver func(ctx context.Context, targetVersion string, emit func(version.DownloadProgress)) error

	watchMu   sync.Mutex
	watching  bool
	watchStop chan struct{}
}

// installerRosterPrefix 安装版通道的在飞名册键前缀：便携/安装两路资产与落位
// 目标各不相同、互不冲突，按"通道+版本"分键去重即可；Windows Installer 的
// 系统级单任务互斥仍由 msiexec 退出码 1618 兜底（见 installExitError）。
const installerRosterPrefix = "installer:"

// NewSubnetDeskService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本 service，二者生命周期一致）；构造无 IO。
func NewSubnetDeskService(plat platform.Platform, holder *extapi.LeaseHolder) *SubnetDeskService {
	paths := settings.GetPaths()
	svc := &SubnetDeskService{
		plat:      plat,
		manager:   version.NewManager(paths.VersionsDir()),
		store:     newSubnetDeskStore(paths.StateDir()),
		downloads: make(map[string]struct{}),
		holder:    holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewSubnetDeskProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// emitInstanceState 引擎状态迁移 → 事件 subnetdesk:instance-state。
func (s *SubnetDeskService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("subnetdesk instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("subnetdesk:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("subnetdesk", "SubnetDesk 实例异常", snap.Error, "/ext/subnetdesk")
	}
}

// activate 启动后台外部实例感知：5s 轮询提取目录校正 external/stopped。
// （自有实例由引擎监督协程轮询进程树感知，不依赖本 watcher。）
//
// 与 ccswitch 的偏差：无空闲自动退出巡检——被控端长期驻留（藏托盘继续监听
// TCP 21118）正是远程桌面的常态形态，按空闲杀进程等于掐断托管。
func (s *SubnetDeskService) activate() {
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
func (s *SubnetDeskService) ListReleases() ([]version.SDRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地可用版本列表（两形态合流）：隔离目录便携安装
// 按版本序在前，系统安装版（如探测命中）追加于尾部——安装版全局至一条
// （MSI MajorUpgrade 就地替换，真机实证），且不可按删目录方式卸载。
func (s *SubnetDeskService) ListInstalledVersions() ([]version.SDVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	list, err := s.manager.ListInstalled()
	if err != nil {
		return nil, err
	}
	if si, ok := s.manager.SystemVersion(); ok {
		list = append(list, si)
	}
	return list, nil
}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 subnetdesk:version-download 推送进度。
// 回执词表：started（已受理）/ already-installed（已装免下）/ in-progress（同版本
// 名册在飞，连点去重不起第二路；前端视图对未知词静默，与 started 同路）。
// 后台协程全程持 drain 租约（ADR-0001 §1.4，rustdesk 同族样板），门拒即不启动、
// 以 error 事件如实回执。
func (s *SubnetDeskService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)

	s.downloadMu.Lock()
	if _, busy := s.downloads[targetVersion]; busy {
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

	// 占名册后放锁：锁只圈"读判+置位"同步段，去重由名册本体跨 goroutine 生效
	// （nil 防御兜住单测裸构造，quicklook ceceabd 同型）
	if s.downloads == nil {
		s.downloads = make(map[string]struct{})
	}
	s.downloads[targetVersion] = struct{}{}
	s.downloadMu.Unlock()

	go func() {
		// 收口销账：注册/销账配对，goroutine 任一路径（租约拒/失败/成功）退出
		// 都执行，拒绝早退不粘闸
		defer func() {
			s.downloadMu.Lock()
			delete(s.downloads, targetVersion)
			s.downloadMu.Unlock()
		}()
		emit := func(p version.DownloadProgress) {
			slog.Debug("subnetdesk download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			s.settleAndBroadcastProgress(targetVersion, p, s.emitDownloadProgress)
			if p.Stage == "done" {
				notify.Success("subnetdesk", "版本下载成功", fmt.Sprintf("SubnetDesk %s 已成功安装", p.Version), "/ext/subnetdesk")
			}
		}
		// 后台租约（ADR-0001 §1.4"在途调用纳入 drain 门"）：RPC 段的 Enter() 随本方法
		// 返回就 release，这里不另取租约的话，下载全程无人记账，停用/退出的有界
		// drain 数不到它；取到后把 lease.Context() 贯穿给下载体，模块停用即取消
		// （rustdesk 同族样板，D4 补齐）。
		lease, leaseErr := s.holder.EnterBackground(context.Background())
		if leaseErr != nil {
			slog.Info("subnetdesk: 后台下载未启动（调用门拒绝）", "version", targetVersion, "err", leaseErr)
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: leaseErr.Error()})
			return
		}
		defer lease.Release()
		drive := s.downloadDriver
		if drive == nil {
			drive = s.manager.DownloadContext // 装配缺位时退回真实下载内核，绝不静默空跑
		}
		if err := drive(lease.Context(), targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("subnetdesk", "版本下载失败", fmt.Sprintf("SubnetDesk %s 下载失败: %v", targetVersion, err), "/ext/subnetdesk")
			return
		}
		// 首装自动设使用的落账已在 done 事件发出前经 settleAndBroadcastProgress
		// 收口（与 termora/2ac9b3b 同根修），此处不再重复落账。
	}()

	return "started", nil
}

// settleAndBroadcastProgress 下载回执"先落账、后广播"收口：done 成功回执若尚未
// 设定使用版本，先把刚落位成功的便携版本记为使用中，再把事件广播给前端——前端
// 共享 store 收到 done 即复刷版本区读 GetActiveVersion，事件先行于落账会让瞬时
// 复刷读到空值（"首个版本下载完不显示使用中"，与 termora/2ac9b3b、windterm/rammap
// 同根修）。manager 仅在原子落位成功后才发 done，据此落账不会把失败版本记成
// 使用中。广播以参数注入，供单测锁死该时序。
func (s *SubnetDeskService) settleAndBroadcastProgress(targetVersion string, p version.DownloadProgress, broadcast func(version.DownloadProgress)) {
	if p.Stage == "done" {
		if v, _ := s.store.GetActive(); v == "" {
			_ = s.store.SetActive(targetVersion, version.FormPortable)
		}
	}
	broadcast(p)
}

// emitDownloadProgress 下载进度事件广播（subnetdesk:version-download）。
func (s *SubnetDeskService) emitDownloadProgress(p version.DownloadProgress) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("subnetdesk:version-download", p)
	}
}

// InstallVersion 下载并安装指定版本的安装版（MSI，后台编排）：进度经事件
// subnetdesk:version-download（kind=installer）推送；安装本体是 Windows Installer
// 前台向导（含 UAC），全程由用户操作，Hanxi 只等待结果并以注册表探测做事实核验。
// 与 DownloadVersion 同用"通道+版本"名册去重（安装版通道独立键）：同版本连点
// 不起第二路 MSI 下载、不弹第二个向导；后台协程全程持 drain 租约（rustdesk
// 同族样板）。Windows Installer 的系统级单任务互斥仍由 msiexec 退出码 1618 兜底。
func (s *SubnetDeskService) InstallVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	rosterKey := installerRosterPrefix + targetVersion

	s.downloadMu.Lock()
	if _, busy := s.downloads[rosterKey]; busy {
		s.downloadMu.Unlock()
		return "in-progress", nil
	}

	if si, ok := s.manager.SystemVersion(); ok && si.Version == targetVersion {
		s.downloadMu.Unlock()
		return "already-installed", nil
	}

	if s.downloads == nil {
		s.downloads = make(map[string]struct{})
	}
	s.downloads[rosterKey] = struct{}{}
	s.downloadMu.Unlock()

	go func() {
		defer func() {
			s.downloadMu.Lock()
			delete(s.downloads, rosterKey)
			s.downloadMu.Unlock()
		}()
		emit := func(stage, msg string) {
			s.emitDownloadProgress(version.DownloadProgress{Version: targetVersion, Kind: "installer", Stage: stage, Message: msg})
		}
		// 后台租约：本 goroutine 覆盖"下载→弹向导→等安装结果"整段，全程计入 drain 门
		// （ADR-0001 §1.4，rustdesk 同族样板）。安装向导由用户操作，可能长于 30s
		// 预算——既定取舍是"先拒绝新操作 + 有界 drain + 超时显式告警"，不强杀用户
		// 正在填的向导。
		lease, leaseErr := s.holder.EnterBackground(context.Background())
		if leaseErr != nil {
			slog.Info("subnetdesk: 后台安装流程未启动（调用门拒绝）", "version", targetVersion, "err", leaseErr)
			emit("error", leaseErr.Error())
			return
		}
		defer lease.Release()
		drive := s.installerDriver
		if drive == nil {
			drive = s.manager.DownloadInstallerContext // 装配缺位时退回真实下载内核，绝不静默空跑
		}
		if err := drive(lease.Context(), targetVersion, s.emitDownloadProgress); err != nil {
			emit("error", err.Error())
			notify.Error("subnetdesk", "安装版下载失败", fmt.Sprintf("SubnetDesk %s 安装包下载失败: %v", targetVersion, err), "/ext/subnetdesk")
			return
		}
		emit("install", "安装向导已弹出：请在 Windows Installer 界面（含 UAC 授权）中完成安装")
		if err := s.manager.Install(targetVersion); err != nil {
			emit("error", err.Error())
			notify.Error("subnetdesk", "安装版安装失败", fmt.Sprintf("SubnetDesk %s 安装未完成: %v", targetVersion, err), "/ext/subnetdesk")
			return
		}
		si, _ := s.manager.SystemVersion()
		emit("done", fmt.Sprintf("SubnetDesk 安装版 %s 已就绪", si.Version))
		notify.Success("subnetdesk", "安装版安装成功", fmt.Sprintf("SubnetDesk 安装版 %s 已就绪（含系统服务，可无人值守被控）", si.Version), "/ext/subnetdesk")
	}()

	return "started", nil
}

// RemoveVersion 卸载指定便携版本（删除隔离目录；正在运行的版本拒绝卸载）。
// 仅作用于隔离目录：系统安装版不走本入口（卸载引导至系统设置，见 OpenUninstallSettings）。
func (s *SubnetDeskService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning &&
		strings.EqualFold(strings.TrimPrefix(snap.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	// 卸载的是当前设定的便携版本则清空，下次冷启动自动回退最新已装
	if v, f := s.store.GetActive(); v == targetVersion && f == version.FormPortable {
		_ = s.store.SetActive("", "")
	}
	return nil
}

// SetActiveVersion 设定使用版本与形态（先校验可用，再持久化）。
// form：portable = 隔离目录版本；installed = 系统安装版（版本须与探测一致）。
func (s *SubnetDeskService) SetActiveVersion(targetVersion string, form string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	form = strings.TrimSpace(form)
	if form == "" {
		form = version.FormPortable // 兼容旧调用：缺省便携
	}
	switch form {
	case version.FormPortable:
		if _, err := s.manager.ResolveExe(targetVersion); err != nil {
			return "", err
		}
	case version.FormInstalled:
		if si, ok := s.manager.SystemVersion(); !ok || si.Version != targetVersion {
			return "", fmt.Errorf("系统安装版当前不是 %s（可能已被升级或卸载），请刷新后重选", targetVersion)
		}
	default:
		return "", fmt.Errorf("未知版本形态: %s", form)
	}
	if err := s.store.SetActive(targetVersion, form); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动回退）。
func (s *SubnetDeskService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	v, _ := s.store.GetActive()
	return v, nil
}

// GetActiveForm 返回当前设定形态（portable / installed；未指定时为空）。
func (s *SubnetDeskService) GetActiveForm() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	_, f := s.store.GetActive()
	return f, nil
}

// ImportLocal 导入本机已有的 SubnetDesk 便携 exe（文件路径或所在目录均可）。
// 配置恒在 %APPDATA%\SubnetDesk 不受导入影响；仅迁移 exe。
// 运行中的实例拒绝导入：Windows 下运行中的 exe 被独占，拷贝必然失败。
func (s *SubnetDeskService) ImportLocal(srcPath string) (version.SDVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.SDVersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal || snap.State == instance.StateStarting {
		return version.SDVersionInfo{}, fmt.Errorf("SubnetDesk 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcPath))
	if err != nil {
		return version.SDVersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if v, _ := s.store.GetActive(); v == "" {
		_ = s.store.SetActive(info.Version, version.FormPortable)
	}
	return info, nil
}

// ---------- 控制操作 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe <dir> 直启；
// 刻意不走 explorer.exe <file> 的"执行"语义（markeron「打开安装目录」按钮的事故教训）。
func (s *SubnetDeskService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// OpenConfigDir 打开 SubnetDesk 的用户数据目录（身份密钥与设置所在）——纯托管下用户想看"数据在哪"的直达入口。只读导航，不改写。
func (s *SubnetDeskService) OpenConfigDir() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	dir, err := modpath.UserConfigDir(dataDirName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("SubnetDesk 数据目录尚未创建（程序还未运行过）: %s", dir)
	}
	return windows.RevealDir(dir)
}

// GetStatus 返回引擎当前状态快照（先做一次静止态外部校正，弥补 5s 轮询间隙的即时性）。
func (s *SubnetDeskService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// OpenWindow 窗口唤起编排中枢（SubnetDesk 无唤窗信使，二次拉起 = 新窗口实例）：
//   - external：EnumWindows 唤起外部便携实例窗口；驻留托盘无窗可唤时只给指引，不越权拉起；
//   - running：优先唤起自有窗口；窗口已销毁（藏托盘）则派生新实例开窗（同 Job 托管）；
//   - stopped/failed：解析 active 版本冷启动。
func (s *SubnetDeskService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting",
			Message: "SubnetDesk 正在启动（首次运行需解压内置负载，最长约 1 分钟），请稍候"}, nil

	case instance.StateExternal:
		if n := s.engine.RestoreExternalWindow(); n > 0 {
			return ControlOutcome{Action: "external-opened", External: true,
				Message: fmt.Sprintf("已唤起外部实例的 %d 个窗口", n)}, nil
		}
		return ControlOutcome{Action: "external", External: true,
			Message: "检测到外部自行启动的实例驻留托盘（无窗口可唤）：请点击其托盘图标打开，或先退出该实例再由 Hanxi 托管"}, nil

	case instance.StateRunning:
		if n := s.engine.RestoreWindow(); n > 0 {
			return ControlOutcome{Action: "opened",
				Message: fmt.Sprintf("已唤起 SubnetDesk 窗口（%d 个）", n)}, nil
		}
		// 主窗已销毁、进程驻留托盘：上游无唤回通道，派生第二个托管实例开新窗
		if err := s.engine.SpawnWindow(); err != nil {
			return ControlOutcome{}, fmt.Errorf("重新打开窗口失败: %w", err)
		}
		return ControlOutcome{Action: "spawned-new",
			Message: "实例驻留托盘且窗口已销毁，已派生新窗口实例（同一 Job 托管）；旧实例仍在托盘中"}, nil

	default:
		v, exe, form, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: v, Form: form, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 SubnetDesk 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			if cur := s.engine.Snapshot(); cur.State == instance.StateFailed && cur.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", cur.Error)
			}
			if form == version.FormInstalled {
				return ControlOutcome{Action: "starting",
					Message: fmt.Sprintf("SubnetDesk 安装版 %s 启动中，页面稍后会自动就绪；若长时间未出现请检查杀软是否拦截", v)}, nil
			}
			return ControlOutcome{Action: "starting",
				Message: fmt.Sprintf("SubnetDesk %s 仍在解压/启动中（首次启动较慢），页面稍后会自动就绪；若 1 分钟后仍未出现，请检查杀软是否拦截", v)}, nil
		}
		if active, _ := s.store.GetActive(); active == "" {
			_ = s.store.SetActive(v, form) // 首次冷启动将实际采用的版本与形态回写
		}
		if form == version.FormInstalled {
			return ControlOutcome{Action: "started",
				Message: fmt.Sprintf("SubnetDesk 安装版 %s 已启动（系统服务在位时，客户端退出后仍可持续无人值守被控）", v)}, nil
		}
		return ControlOutcome{Action: "started",
			Message: fmt.Sprintf("SubnetDesk %s 已启动（局域网被控：在其窗口 LAN 设置中启用并配置密码）", v)}, nil
	}
}

// Quit 退出引擎托管的 SubnetDesk（终止整个进程树，含被控监听端）。
// external 状态不越权强杀：仅返回人性化指引。
func (s *SubnetDeskService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	snap := s.engine.Snapshot()
	if snap.State == instance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: "当前是外部自行启动的实例，不在 Hanxi 托管范围：请在其托盘菜单退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	if snap.Form == version.FormInstalled {
		return QuitOutcome{Stopped: true,
			Message: "安装版客户端已退出（系统服务保持运行——无人值守被控不受影响；如需彻底停用服务请到系统服务管理）"}, nil
	}
	return QuitOutcome{Stopped: true,
		Message: "SubnetDesk 已退出（便携版无优雅退出通道，进程树整体终止——进行中的远程会话会断开）"}, nil
}

// Shutdown RPC：经调用门取 operation lease 后执行收尾（与内部版 shutdown 同语义）。
// 停用/阻止态下被门拒属预期——OnDestroy 路径走内部版，不经本入口。
func (s *SubnetDeskService) Shutdown() error {
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
func (s *SubnetDeskService) shutdown() {
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

// resolveActiveVersion 解析当前应使用的版本（版本号/可执行路径/形态）：
// activeVersion+activeForm 优先，失效（便携目录被删 / 安装版被卸载或升级换版）
// 清空自愈；未设定时回退最新已装便携版，无任何便携安装但存在系统安装版时也
// 直接采用安装版（冷启动诚实使用现状）。
func (s *SubnetDeskService) resolveActiveVersion() (string, string, string, error) {
	active, form := s.store.GetActive()
	if active != "" {
		switch form {
		case version.FormInstalled:
			if si, ok := s.manager.SystemVersion(); ok && si.Version == active {
				return active, si.ExePath, version.FormInstalled, nil
			}
		default: // portable（含 form 缺失的旧配置）
			if exe, err := s.manager.ResolveExe(active); err == nil {
				return active, exe, version.FormPortable, nil
			}
		}
		_ = s.store.SetActive("", "") // 已设定的版本被卸载/损坏：清空自愈，回退最新已装
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", "", err
	}
	if len(installed) > 0 {
		sort.Slice(installed, func(i, j int) bool {
			return versionCompare(installed[i].Version, installed[j].Version) > 0
		})
		latest := installed[0]
		return latest.Version, latest.ExePath, version.FormPortable, nil
	}
	if si, ok := s.manager.SystemVersion(); ok {
		return si.Version, si.ExePath, version.FormInstalled, nil
	}
	return "", "", "", fmt.Errorf("尚未安装任何 SubnetDesk 版本，请先在版本管理下载便携版或安装系统版")
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 1.10.0/1.9.0 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）。
	return versioncmp.Compare(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}

// ---------- 联动开关与桌面辅助 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *SubnetDeskService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效）。
func (s *SubnetDeskService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// CreateDesktopShortcut 在桌面为当前使用版本创建快捷方式（同名覆盖）。
func (s *SubnetDeskService) CreateDesktopShortcut() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	_, exe, _, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("SubnetDesk", exe, filepath.Dir(exe))
}

// OpenUninstallSettings 打开系统「安装的应用」设置页，引导用户卸载系统安装版。
// 刻意不代执行 msiexec /x：卸载系统级软件（连带移除服务）属用户决策的高危
// 操作，交还 Windows 原生入口（遵循项目敏感操作确认守则）。
func (s *SubnetDeskService) OpenUninstallSettings() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL("ms-settings:appsfeatures")
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *SubnetDeskService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *SubnetDeskService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// normalizeVersion 版本入参归一（Download/Install/Remove/SetActive 四入口共用）：
// 先 TrimSpace，大小写 v 一律认（剥除既有 v/V 前缀后统一补 v）。旧形制用
// "v" + TrimPrefix(TrimSpace(x), "v") 只认小写 v，大写 V 入参（如 "V1.2.0"）被归成
// "vV1.2.0"：已装去重落空并放行一路注定失败的后台下载，SetActive 的系统安装版比对
// 与 Remove 的目录寻径又对同一输入报"非法版本号/未安装"——三入口互异。修复后裸号/
// v号/V号/带空白同落一个 v 前缀规范形；imported-/vimported- 兜底令牌经"剥既有 v 再补
// v"恒等透传为 vimported-（绝不产出 vvimported-），与 ListInstalled 展示（"v"+目录后缀）
// 及 store 记账口径一致。
func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 0 && (value[0] == 'v' || value[0] == 'V') {
		value = value[1:]
	}
	return "v" + value
}
