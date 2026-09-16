package douzy

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/douzy/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// DouzyService 向前端暴露「Douzy 全能下载器」（多平台：抖音/TikTok/YouTube/Telegram/X）
// 的版本管理与安装包下载能力。
// 刻意窄于 ccswitch/rustdesk 等托管模块：本模块**不做进程托管**——
// 仅"列版本 → 下载官方安装包 → sha256 校验 → 拉起上游安装向导（发射后不管）"。
// 原因见 version 包注释：桌面版内测、Electron 壳闭源、Windows 仅 NSIS 安装版。
type DouzyService struct {
	plat    platform.Platform
	manager *version.Manager

	downloadMu sync.Mutex
	downloads  map[string]struct{}
}

// NewDouzyService 装配版本管理器与下载槽位；构造无 IO（store/引擎由后续流程按形态创建）。
func NewDouzyService(plat platform.Platform) *DouzyService {
	paths := settings.GetPaths()
	return &DouzyService{
		plat:      plat,
		manager:   version.NewManager(paths.VersionsDir()),
		downloads: make(map[string]struct{}),
	}
}

// ---------- 版本管理 ----------

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存，按上游发布顺序新在前）。
func (s *DouzyService) ListReleases() ([]version.DouzyRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已下载安装包列表（按版本数值降序，最新在前）。
func (s *DouzyService) ListInstalledVersions() ([]version.DouzyVersionInfo, error) {
	list, err := s.manager.ListInstalled()
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool {
		return versionCompare(list[i].Version, list[j].Version) > 0
	})
	return list, nil
}

// DownloadVersion 后台下载指定版本安装包：立即返回，全程经事件 douzy:version-download 推送进度。
// 返回 "started" / "in-progress" / "already-installed"，语义与 ccswitch 对齐供前端复用。
func (s *DouzyService) DownloadVersion(targetVersion string) (string, error) {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")

	s.downloadMu.Lock()
	if _, ok := s.downloads[targetVersion]; ok {
		s.downloadMu.Unlock()
		return "in-progress", nil
	}
	// 已下载则直接返回，避免重复下载
	if installed, err := s.manager.ListInstalled(); err == nil {
		for _, v := range installed {
			if strings.EqualFold(strings.TrimPrefix(v.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
				s.downloadMu.Unlock()
				return "already-installed", nil
			}
		}
	}
	s.downloads[targetVersion] = struct{}{}
	s.downloadMu.Unlock()

	go func() {
		defer func() {
			s.downloadMu.Lock()
			delete(s.downloads, targetVersion)
			s.downloadMu.Unlock()
		}()
		emit := func(p version.DownloadProgress) {
			slog.Debug("douzy download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("douzy:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("douzy", "安装包下载完成",
					fmt.Sprintf("Douzy %s 安装包已下载并校验，点「运行安装程序」或到目录双击安装", p.Version), "/ext/douzy")
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("douzy", "安装包下载失败", fmt.Sprintf("Douzy %s 下载失败: %v", targetVersion, err), "/ext/douzy")
			return
		}
	}()

	return "started", nil
}

// LaunchInstaller 拉起指定版本已下载安装包的上游安装向导（NSIS，交互式）。
// 与托管引擎无关：不 Wait、不绑 JobObject、不探测生命周期——UAC 授权与向导
// 全程由用户操作，Hanxi 只负责"把包交出去"。安装结果本模块无从得知（壳闭源）。
func (s *DouzyService) LaunchInstaller(targetVersion string) error {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	exe, err := s.manager.InstallerPath(targetVersion)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe) // 安装器工作目录置于其所在隔离目录
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动安装程序: %w", err)
	}
	return nil
}

// RemoveVersion 删除指定版本的已下载安装包目录。
func (s *DouzyService) RemoveVersion(targetVersion string) error {
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	return s.manager.Remove(targetVersion)
}

// ---------- 目录与仓库导航 ----------

// OpenDir 在资源管理器中打开版本隔离目录（"打开位置"按钮）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe <dir> 直启；
// 刻意不走 explorer.exe <file> 的"执行"语义（markeron「打开安装目录」按钮的事故教训）。
func (s *DouzyService) OpenDir(dir string) error {
	return windows.RevealDir(dir)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *DouzyService) RepositoryURL() (string, error) {
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *DouzyService) OpenRepository() error {
	return s.plat.OpenURL(version.RepoURL())
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名字典序对 0.11.x/0.8.x 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）。
	return versioncmp.Compare(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}
