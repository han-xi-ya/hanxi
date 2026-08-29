package snipaste

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/snipaste/instance"
	"hanxi/internal/modules/snipaste/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/settings"
)

type SnipasteService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *snipasteStore
	engine  *instance.Engine

	downloadMu sync.Mutex
	downloads  map[string]struct{}
}

func NewSnipasteService(plat platform.Platform) *SnipasteService {
	paths := settings.GetPaths()
	svc := &SnipasteService{
		plat: plat, manager: version.NewManager(paths.VersionsDir()),
		store: newSnipasteStore(paths.DataDir()), downloads: make(map[string]struct{}),
	}
	svc.engine = instance.NewEngine(plat.Job(), plat.Process(), instance.Callbacks{OnState: svc.emitInstanceState})
	return svc
}

func (s *SnipasteService) emitInstanceState(snapshot instance.Snapshot) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("snipaste:instance-state", snapshot)
	}
	if snapshot.State == instance.StateFailed && snapshot.Error != "" {
		notify.Error("snipaste", "Snipaste 实例异常", snapshot.Error, "/ext/snipaste")
	}
}

func (s *SnipasteService) ListReleases() ([]version.SnipasteRelease, error) {
	return s.manager.ListRemote()
}

func (s *SnipasteService) ListInstalledVersions() ([]version.SnipasteVersionInfo, error) {
	return s.manager.ListInstalled()
}

func (s *SnipasteService) DownloadVersion(targetVersion string) (string, error) {
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

func (s *SnipasteService) ImportLocal(srcDir string) (version.SnipasteVersionInfo, error) {
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return version.SnipasteVersionInfo{}, err
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

func (s *SnipasteService) RemoveVersion(targetVersion string) error {
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

func (s *SnipasteService) SetActiveVersion(targetVersion string) (string, error) {
	targetVersion = normalizeVersion(targetVersion)
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

func (s *SnipasteService) GetActiveVersion() (string, error) {
	return s.store.GetActive(), nil
}

func (s *SnipasteService) Launch() (LaunchOutcome, error) {
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

func (s *SnipasteService) GetStatus() (instance.Snapshot, error) {
	return s.engine.Snapshot(), nil
}

func (s *SnipasteService) Quit() (QuitOutcome, error) {
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

func (s *SnipasteService) OpenDir(dir string) error {
	dir = strings.TrimSpace(dir)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	return exec.Command("explorer.exe", dir).Start()
}

func (s *SnipasteService) OfficialSiteURL() (string, error) {
	return version.OfficialSiteURL(), nil
}

func (s *SnipasteService) OpenOfficialSite() error {
	return s.plat.OpenURL(version.OfficialSiteURL())
}

func normalizeVersion(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
}
