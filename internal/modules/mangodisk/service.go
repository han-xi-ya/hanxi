package mangodisk

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/modules/mangodisk/instance"
	"hubkit/internal/modules/mangodisk/version"
	"hubkit/internal/notify"
	"hubkit/internal/platform"
	"hubkit/internal/platform/versioncmp"
	"hubkit/internal/settings"
)

const (
	readyTimeout  = 20 * time.Second
	watchInterval = 5 * time.Second
)

// MangoDiskService 只托管原版 GUI；磁盘扫描、清理和系统设置仍在上游窗口内完成。
type MangoDiskService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *mangoDiskStore
	engine  *instance.Engine

	downloadMu sync.Mutex
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}
}

func NewMangoDiskService(plat platform.Platform) *MangoDiskService {
	paths := settings.GetPaths()
	svc := &MangoDiskService{
		plat: plat, manager: version.NewManager(paths.VersionsDir()), store: newMangoDiskStore(paths.DataDir()),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewMangoDiskProbe(), instance.Callbacks{OnState: svc.emitInstanceState})
	return svc
}

func (s *MangoDiskService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("mangodisk instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("mangodisk:instance-state", snap)
	}
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("mangodisk", "MangoDisk 实例异常", snap.Error, "/ext/mangodisk")
	}
}

func (s *MangoDiskService) activate() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watching {
		return
	}
	s.watching = true
	stop := make(chan struct{})
	s.watchStop = stop
	go func() {
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				s.engine.RefreshExternal()
			}
		}
	}()
}

func (s *MangoDiskService) ListReleases() ([]version.MangoDiskRelease, error) {
	return s.manager.ListRemote()
}

func (s *MangoDiskService) ListInstalledVersions() ([]version.MangoDiskVersionInfo, error) {
	return s.manager.ListInstalled()
}

func (s *MangoDiskService) DownloadVersion(targetVersion string) (string, error) {
	targetVersion = normalizeVersion(targetVersion)
	if !s.downloadMu.TryLock() {
		return "", fmt.Errorf("已有 MangoDisk 版本正在下载，请等待完成后再试")
	}
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, item := range installed {
			if item.Version == targetVersion {
				s.downloadMu.Unlock()
				return "already-installed", nil
			}
		}
	}
	go func() {
		defer s.downloadMu.Unlock()
		emit := func(progress version.DownloadProgress) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("mangodisk:version-download", progress)
			}
			if progress.Stage == "done" {
				notify.Success("mangodisk", "版本下载成功", fmt.Sprintf("MangoDisk %s 已成功安装", progress.Version), "/ext/mangodisk")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("mangodisk", "版本下载失败", fmt.Sprintf("MangoDisk %s 下载失败: %v", targetVersion, err), "/ext/mangodisk")
		}
	}()
	return "started", nil
}

func (s *MangoDiskService) RemoveVersion(targetVersion string) error {
	targetVersion = normalizeVersion(targetVersion)
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning && snap.Version == targetVersion {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("")
	}
	return nil
}

func (s *MangoDiskService) SetActiveVersion(targetVersion string) (string, error) {
	targetVersion = normalizeVersion(targetVersion)
	info, err := s.manager.Inspect(targetVersion)
	if err != nil {
		return "", err
	}
	if info.Integrity == version.IntegrityInvalid {
		return "", fmt.Errorf("版本 %s 安装无效：%s", targetVersion, info.IntegrityNote)
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

func (s *MangoDiskService) GetActiveVersion() (string, error) { return s.store.GetActive(), nil }

func (s *MangoDiskService) ImportLocal(srcExe string) (version.MangoDiskVersionInfo, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateRunning || snap.State == instance.StateExternal {
		return version.MangoDiskVersionInfo{}, fmt.Errorf("MangoDisk 正在运行，请先退出再导入")
	}
	return s.manager.ImportLocal(strings.TrimSpace(srcExe))
}

func (s *MangoDiskService) OpenDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("目录路径不能为空")
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

func (s *MangoDiskService) GetStatus() (instance.Snapshot, error) {
	s.engine.RefreshExternal()
	return s.engine.Snapshot(), nil
}

func (s *MangoDiskService) OpenWindow() (ControlOutcome, error) {
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()
	switch snap.State {
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "MangoDisk 正在启动中，请稍候"}, nil
	case instance.StateExternal:
		exe, err := s.resolveInstalledExeAny()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.engine.OpenWindow(exe); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "external-opened", External: true, Message: "已唤起外部运行中的 MangoDisk 窗口"}, nil
	case instance.StateRunning:
		if _, err := s.engine.OpenWindow(s.engine.Exe()); err != nil {
			return ControlOutcome{}, fmt.Errorf("唤起窗口失败: %w", err)
		}
		return ControlOutcome{Action: "opened", Message: "已唤起 MangoDisk 窗口"}, nil
	default:
		selected, exe, err := s.resolveActiveVersion()
		if err != nil {
			return ControlOutcome{}, err
		}
		if _, err := s.manager.VerifyBeforeLaunch(selected); err != nil {
			return ControlOutcome{}, err
		}
		if err := s.engine.Start(instance.StartOptions{Version: selected, Exe: exe, Detached: !s.store.GetFollowOnExit()}); err != nil {
			return ControlOutcome{}, fmt.Errorf("启动 MangoDisk 失败: %w", err)
		}
		if !s.engine.WaitReady(readyTimeout) {
			if current := s.engine.Snapshot(); current.State == instance.StateFailed && current.Error != "" {
				return ControlOutcome{}, fmt.Errorf("%s", current.Error)
			}
			return ControlOutcome{}, fmt.Errorf("等待 MangoDisk 就绪超时（%d 秒），请确认程序完整且已安装 WebView2 Runtime", int(readyTimeout/time.Second))
		}
		if s.store.GetActive() == "" {
			_ = s.store.SetActive(selected)
		}
		return ControlOutcome{Action: "started", Message: fmt.Sprintf("MangoDisk %s 已启动", selected)}, nil
	}
}

func (s *MangoDiskService) Quit() (QuitOutcome, error) {
	if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
		return QuitOutcome{External: true, Message: "当前是外部自行启动的实例，请在 MangoDisk 窗口内退出"}, nil
	}
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Message: "MangoDisk 已退出"}, nil
}

func (s *MangoDiskService) Shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop()
	}
}

func (s *MangoDiskService) resolveActiveVersion() (string, string, error) {
	if active := s.store.GetActive(); active != "" {
		if info, err := s.manager.Inspect(active); err == nil && info.Integrity != version.IntegrityInvalid {
			return active, info.ExePath, nil
		}
		_ = s.store.SetActive("")
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", err
	}
	valid := installed[:0]
	for _, item := range installed {
		if item.Integrity != version.IntegrityInvalid {
			valid = append(valid, item)
		}
	}
	if len(valid) == 0 {
		return "", "", fmt.Errorf("尚未安装可用的 MangoDisk 版本，请先在版本管理下载或导入")
	}
	sort.Slice(valid, func(i, j int) bool {
		return versioncmp.Compare(strings.TrimPrefix(valid[i].Version, "v"), strings.TrimPrefix(valid[j].Version, "v")) > 0
	})
	return valid[0].Version, valid[0].ExePath, nil
}

func (s *MangoDiskService) resolveInstalledExeAny() (string, error) {
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", err
	}
	for _, state := range []version.IntegrityState{version.IntegrityVerified, version.IntegrityLocalBaseline, version.IntegrityDrifted} {
		for _, item := range installed {
			if item.Integrity == state && item.ExePath != "" {
				return item.ExePath, nil
			}
		}
	}
	return "", fmt.Errorf("尚未安装可用的 MangoDisk 版本，无法代为唤起外部实例窗口")
}

func (s *MangoDiskService) GetFollowOnExit() (bool, error) { return s.store.GetFollowOnExit(), nil }
func (s *MangoDiskService) SetFollowOnExit(enabled bool) error {
	return s.store.SetFollowOnExit(enabled)
}

// CreateDesktopShortcut 在桌面创建指向当前使用版本的快捷方式（同名覆盖）。
// resolveActiveVersion 与冷启动保持一致：未指定 active 时自动选择最新可用版本。
func (s *MangoDiskService) CreateDesktopShortcut() error {
	_, exe, err := s.resolveActiveVersion()
	if err != nil {
		return err
	}
	return s.plat.CreateDesktopShortcut("MangoDisk", exe, filepath.Dir(exe))
}

func (s *MangoDiskService) RepositoryURL() (string, error) { return version.RepoURL(), nil }
func (s *MangoDiskService) OpenRepository() error          { return s.plat.OpenURL(version.RepoURL()) }

func normalizeVersion(value string) string {
	return "v" + strings.TrimPrefix(strings.TrimSpace(value), "v")
}
