package snipaste

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/snipaste/instance"
	"hanxi/internal/modules/snipaste/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/settings"
	"hanxi/packages/go/externalquit"
)

// SnipasteService 面向前端的 Snipaste 托管服务：官网 zip 下载、本地导入、版本切换与会话内启停。
// downloads 记录进行中的下载版本号（按版本去重，允许不同版本并行下载）。
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：
// 未安装/停用/阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
type SnipasteService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *snipasteStore
	engine  *instance.Engine
	holder  *extapi.LeaseHolder

	downloadMu sync.Mutex
	downloads  map[string]struct{}
}

// NewSnipasteService 装配版本管理器、store 与实例引擎；构造无 IO。
func NewSnipasteService(plat platform.Platform, holder *extapi.LeaseHolder) *SnipasteService {
	paths := settings.GetPaths()
	svc := &SnipasteService{
		plat: plat, manager: version.NewManager(paths.VersionsDir()),
		store: newSnipasteStore(paths.StateDir()), downloads: make(map[string]struct{}),
		holder: holder,
	}
	svc.engine = instance.NewEngine(plat.Job(), plat.Process(), instance.NewSnipasteProbe(plat.Process()),
		instance.Callbacks{OnState: svc.emitInstanceState})
	return svc
}

// emitInstanceState 引擎状态回调：广播 "snipaste:instance-state" 事件，failed 态另发系统通知。
func (s *SnipasteService) emitInstanceState(snapshot instance.Snapshot) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("snipaste:instance-state", snapshot)
	}
	if snapshot.State == instance.StateFailed && snapshot.Error != "" {
		notify.Error("snipaste", "Snipaste 实例异常", snapshot.Error, "/ext/snipaste")
	}
}

// ListReleases 拉取官网发布通道列表（远端缓存），网络失败返回错误。
func (s *SnipasteService) ListReleases() ([]version.SnipasteRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 扫描本地已装版本目录。
func (s *SnipasteService) ListInstalledVersions() ([]version.SnipasteVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// DownloadVersion 异步下载：返回 "started"；目标版本已装返回 "already-installed"、
// 同版本下载中返回 "in-progress"（不报错，前端按状态渲染）。进度经
// "snipaste:version-download" 事件推送；首个版本装完自动设为使用版本。
func (s *SnipasteService) DownloadVersion(targetVersion string) (string, error) {
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

	go func() {
		defer func() {
			s.downloadMu.Lock()
			delete(s.downloads, targetVersion)
			s.downloadMu.Unlock()
		}()
		emit := func(progress version.DownloadProgress) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("snipaste:version-download", progress)
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("snipaste", "版本下载失败", fmt.Sprintf("Snipaste %s 下载失败: %v", targetVersion, err), "/ext/snipaste")
			return
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(targetVersion)
		}
		notify.Success("snipaste", "版本安装成功", fmt.Sprintf("Snipaste %s 已安装", targetVersion), "/ext/snipaste")
	}()
	return "started", nil
}

// ImportLocal 导入本地 Snipaste 目录为托管版本；未设使用版本时自动激活导入结果。
func (s *SnipasteService) ImportLocal(srcDir string) (version.SnipasteVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.SnipasteVersionInfo{}, gateErr
	}
	defer release()
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.SnipasteVersionInfo{}, err
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// RemoveVersion 删除本地版本；本会话正在运行该版本、或该版本为"当前使用版本"时拒绝。
func (s *SnipasteService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	snapshot := instance.Snapshot{}
	if s.engine != nil {
		snapshot = s.engine.Snapshot()
	}
	if (snapshot.State == instance.StateRunning || snapshot.State == instance.StateStarting || snapshot.State == instance.StateQuitting) && strings.EqualFold(snapshot.Version, targetVersion) {
		return fmt.Errorf("版本 %s 正由本会话运行，请先退出进程", targetVersion)
	}
	if strings.EqualFold(s.store.GetActive(), targetVersion) {
		return fmt.Errorf("当前使用版本 %s 不可卸载，请先选择其他版本", targetVersion)
	}
	return s.manager.Remove(targetVersion)
}

// SetActiveVersion 切换使用版本；ResolveExe 确认版本目录内主程序存在后才落盘。
func (s *SnipasteService) SetActiveVersion(targetVersion string) (string, error) {
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
func (s *SnipasteService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// Launch 启动当前使用版本（无 active 时回退最新可运行版本）。启动前复核 exe 为非空常规文件，
// 交给 engine.Start 建身份与 Job 绑定；重复启动由 engine 拒绝。
func (s *SnipasteService) Launch() (LaunchOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return LaunchOutcome{}, gateErr
	}
	defer release()
	// 外部实例在场时二次启动只会转发让位（Qt 单实例），不会建立托管——
	// 提前甄别并如实告知处置选项（N3 分档下页面可直接退出外部实例）。
	s.engine.RefreshExternal()
	if s.engine.ExternalRunning() {
		return LaunchOutcome{}, fmt.Errorf("检测到外部自行启动的 Snipaste 正在运行：可直接在页面退出让 hanxi 接管，或用「显隐贴图」唤起它；托管启动需先退出该实例")
	}
	selected, exe, err := s.resolveActiveVersion()
	if err != nil {
		return LaunchOutcome{}, err
	}
	fi, err := os.Stat(exe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return LaunchOutcome{}, fmt.Errorf("Snipaste %s 安装损坏：可执行文件不可用", selected)
	}
	if err := s.engine.Start(instance.StartOptions{Version: selected, Exe: exe}); err != nil {
		return LaunchOutcome{}, fmt.Errorf("启动 Snipaste %s 失败: %w", selected, err)
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(selected)
	}
	return LaunchOutcome{
		Version: selected,
		Message: fmt.Sprintf("已启动 Snipaste %s；本会话可手动退出，关闭 Hanxi 不会连带结束", selected),
	}, nil
}

// GetStatus 返回引擎状态快照；读取前做一次外部探针校正（瞬时枚举调用，
// 内核在运行/启动/退出中自行短路，不会误探自家进程——external 感知为拉取式，
// 前端定时刷新即得）。
func (s *SnipasteService) GetStatus() (instance.Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return instance.Snapshot{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

// Quit 执行分层退出并把 engine 的 Method 归因翻译成用户文案；
// 身份复核被拒时保留 Stopped/Method 字段返回错误，供前端精确提示"未误杀"场景。
// 外部实例（N3 终裁 force-free 档）走 quitExternal 独立通道，不触碰自有语义。
func (s *SnipasteService) Quit() (QuitOutcome, error) {
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
	if err != nil {
		return QuitOutcome{Stopped: result.Stopped, Forced: result.Forced, CloseRequested: result.CloseRequested, Method: result.Method}, err
	}
	out := QuitOutcome{Stopped: result.Stopped, Forced: result.Forced, CloseRequested: result.CloseRequested, Method: result.Method}
	switch result.Method {
	case "not-managed":
		out.Message = "当前 Hanxi 会话没有可退出的 Snipaste 实例"
	case "close-request":
		out.Message = "Snipaste 已在关闭请求后退出"
	case "already-exited":
		out.Message = "Snipaste 已经退出"
	case "forced-job", "forced-process":
		out.Message = "Snipaste 未响应关闭请求，已强制结束本会话启动的实例；未落盘状态可能丢失"
	default:
		out.Message = "Snipaste 退出操作已完成"
	}
	return out, nil
}

// quitExternal 外部实例按 N3 终裁低损档（force-free）退出：WM_CLOSE 尽力投递
// → 宽限观察 → 身份复核 → 强杀。低损论证：Snipaste 配置即时写盘、贴图有自动
// 备份恢复机制（W1 调研 §2）。提权目标被 UIPI 拦截时如实降级为指引。
func (s *SnipasteService) quitExternal() (QuitOutcome, error) {
	res, err := s.engine.QuitExternal(context.Background(), externalquit.PolicyForceFree, "", nil)
	out := QuitOutcome{Stopped: res.Stopped, Forced: res.Forced, Method: res.Method, External: true,
		CloseRequested: res.Method == externalquit.MethodGraceful}
	switch res.Method {
	case externalquit.MethodGraceful:
		out.Message = "外部自行启动的 Snipaste 已响应关闭请求优雅退出"
	case externalquit.MethodAlreadyGone:
		out.Message = "外部 Snipaste 实例已经退出"
	case externalquit.MethodForced:
		out.Message = "外部 Snipaste 未响应关闭请求，已强制结束（贴图由其自动备份机制兜底，下次启动会恢复）"
	case externalquit.MethodBlocked:
		out.Message = "外部 Snipaste 以管理员权限运行，hanxi 无法代为终止，请在其托盘图标退出"
	case externalquit.MethodDeclined:
		out.Message = "已取消退出"
	}
	return out, err
}

// ShowImages 唤起 Snipaste 贴图显隐——Snipaste 无传统主窗口，官方 `toggle-images`
// 命令即其"唤窗"等价物（Qt 单实例转发通道，免费命令，外部/自有实例通用，
// W1 调研 §2）。命令仅在实例已在运行时有实效，静止态先引导启动。
func (s *SnipasteService) ShowImages() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()
	switch snap.State {
	case instance.StateRunning, instance.StateExternal:
	default:
		return "", fmt.Errorf("Snipaste 未在运行（当前状态 %s）：官方命令仅在实例运行时生效，请先启动", snap.State)
	}
	if snap.ExePath == "" {
		return "", fmt.Errorf("未能取得在运行 Snipaste 实例的可执行路径，请改用其自身热键（默认 Shift+F3 显隐贴图）")
	}
	if err := instance.SpawnCommandMessenger(snap.ExePath, "toggle-images"); err != nil {
		return "", err
	}
	return "已向 Snipaste 发出贴图显隐切换指令（该命令为切换开关：贴图隐藏时将其唤出）", nil
}

// resolveActiveVersion 解析启动目标：active 可用则直用（损坏自动清空回退），
// 否则取已装版本中 versioncmp 最高者。
func (s *SnipasteService) resolveActiveVersion() (string, string, error) {
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
		return "", "", fmt.Errorf("尚未安装 Snipaste，请先下载或导入一个免安装版本")
	}
	sort.SliceStable(installed, func(i, j int) bool {
		return versioncmp.Compare(installed[i].Version, installed[j].Version) > 0
	})
	return installed[0].Version, installed[0].ExePath, nil
}

// OpenDir 用资源管理器打开版本目录（先校验存在，explorer 自身不报错）。
func (s *SnipasteService) OpenDir(dir string) error {
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

// OfficialSiteURL / OpenOfficialSite 提供 Snipaste 官网入口（error 恒 nil，统一前端签名）。
func (s *SnipasteService) OfficialSiteURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.OfficialSiteURL(), nil
}

func (s *SnipasteService) OpenOfficialSite() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.OfficialSiteURL())
}

// normalizeVersion 统一为不带 v 前缀的纯版本号（Snipaste 版本号形态）。
func normalizeVersion(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
}
