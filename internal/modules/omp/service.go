package omp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/omp/version"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// OmpService 向前端暴露「Oh My Pi (omp)」的版本管理能力：远程列表、官方
// sha256 校验下载、本地导入、激活切换与数据目录导航。
// 刻意窄于 ccswitch/rustdesk 等托管模块：**本模块不做进程托管**——omp 是
// 纯 TUI 编码代理，必须附着控制台终端才能交互（运行入口是用户 PATH 里的
// omp / hanxi 嵌入终端 / Paseo 驱动，见 version 包注释），启停/探测/唤窗
// 在此形态下不成立。所有业务 RPC 方法经 holder.Enter() 接入统一调用门。
type OmpService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *ompStore
	holder  *extapi.LeaseHolder
	launch  *launcher
	binDir  string // 稳定命令入口目录 <BaseDir>/bin/omp（见 launcher.go 设计注释）
	baseDir string // 便携根，供前端展示入口位置

	downloadMu sync.Mutex
	downloads  map[string]struct{}
}

// NewOmpService 装配版本管理器与激活 store；构造无 IO 副作用之外的开销
// （store 读档为唯一启动读盘，损坏容忍不阻断）。
func NewOmpService(plat platform.Platform, holder *extapi.LeaseHolder) *OmpService {
	paths := settings.GetPaths()
	return &OmpService{
		plat:      plat,
		manager:   version.NewManager(paths.VersionsDir()),
		store:     newOmpStore(paths.StateDir()),
		holder:    holder,
		launch:    newWindowsLauncher(),
		binDir:    filepath.Join(paths.BaseDir(), "bin", "omp"),
		baseDir:   paths.BaseDir(),
		downloads: make(map[string]struct{}),
	}
}

// InstallLauncher 一键"命令入口安装"（复刻官方脚本装完即敲 omp 的体验，归属 hanxi 版本树）：
// 把激活版本的 exe 硬链接到稳定入口 `<BaseDir>\bin\omp\omp.exe`，首次安装幂等写入
// 用户 PATH 并广播 WM_SETTINGCHANGE；此后升级/切换只需再点一次（只换链接不动注册表）。
func (s *OmpService) InstallLauncher() (LauncherOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return LauncherOutcome{}, gateErr
	}
	defer release()
	activeExe, err := s.manager.ResolveExe(strings.TrimPrefix(s.store.GetActive(), "v"))
	if err != nil {
		if activeExe, err = s.manager.ResolveExe(""); err != nil {
			return LauncherOutcome{}, fmt.Errorf("版本树尚无可用 omp——请先下载或导入一个版本: %v", err)
		}
	}
	return s.launch.install(s.binDir, activeExe)
}

// UninstallLauncher 回收命令入口：删本模块建的入口文件与自己那条 PATH；
// 官方脚本装的 %LOCALAPPDATA%\omp 条目与目录不属本模块，原样不动。
func (s *OmpService) UninstallLauncher() (LauncherOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return LauncherOutcome{}, gateErr
	}
	defer release()
	return s.launch.uninstall(s.binDir)
}

// ---------- 版本管理 ----------

// ListReleases 获取远程可用版本列表（多镜像回退，10 分钟缓存，按上游发布顺序新在前）。
func (s *OmpService) ListReleases() ([]version.OmpRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地版本树已落位列表（版本数值降序，内核单点负责），
// 并标注激活位：store 为空时按"最新已装"回退高亮（与 ResolveExe 兜底口径一致）。
func (s *OmpService) ListInstalledVersions() ([]version.OmpVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	list, err := s.manager.ListInstalled()
	if err != nil {
		return nil, err
	}
	active := s.store.GetActive()
	if active == "" && len(list) > 0 {
		active = list[0].Version
	}
	for i := range list {
		list[i].Active = list[i].Version == active
	}
	return list, nil
}

// GetStatus 模块状态快照：激活版本、exe 路径与 omp 数据根（~/.omp）。
func (s *OmpService) GetStatus() (OmpStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return OmpStatus{}, gateErr
	}
	defer release()

	st := OmpStatus{ActiveVersion: s.store.GetActive(), BinDir: s.binDir}
	st.LauncherInstalled, st.OnPath = s.launch.status(s.binDir)
	exe, err := s.manager.ResolveExe(strings.TrimPrefix(st.ActiveVersion, "v"))
	if err != nil {
		// 未指定或指定版本已不可用：回退"最新已装"如实展示
		if fallback, ferr := s.manager.ResolveExe(""); ferr == nil {
			st.ExePath = fallback
			if st.ActiveVersion == "" {
				st.ActiveVersion = activeVersionFromPath(fallback)
			}
		}
	} else {
		st.ExePath = exe
	}
	if home, herr := os.UserHomeDir(); herr == nil {
		st.DataDir = filepath.Join(home, ".omp")
	}
	return st, nil
}

// ---------- 下载 / 导入 / 切换 / 删除 ----------

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 omp:version-download
// 推送进度。返回 "started" / "in-progress" / "already-installed"，
// 语义与 douzy/ccswitch 对齐供前端复用。
func (s *OmpService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)

	s.downloadMu.Lock()
	if _, ok := s.downloads[targetVersion]; ok {
		s.downloadMu.Unlock()
		return "in-progress", nil
	}
	// 已落位则直接返回，避免重复下载
	if installed, err := s.manager.ListInstalled(); err == nil {
		for _, v := range installed {
			if strings.EqualFold(v.Version, targetVersion) {
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
			slog.Debug("omp download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if p.Stage == "done" && s.store.GetActive() == "" {
				// "首装自动激活"的落账必须发生在 done 事件发出之前（ccswitch 同款
				// 纪律）：done 先到而 active 后写，前端共享 store 会闪一帧"未激活"。
				_ = s.store.SetActive(p.Version)
			}
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("omp:version-download", p)
			}
			if p.Stage == "done" {
				notify.Success("omp", "版本落位完成",
					fmt.Sprintf("Oh My Pi %s 已下载并通过官方 SHA-256 校验，落位托管版本树", p.Version), "/ext/omp")
			}
		}
		// 后台租约（ADR-0001 §1.4"在途调用纳入 drain 门"）：244MB 单文件下载
		// 全程须有租约记账，模块停用/退出经 lease.Context() 即时取消（内核 Fetch
		// 在退避间隙/换源前/读循环内均响应取消）。
		lease, leaseErr := s.holder.EnterBackground(context.Background())
		if leaseErr != nil {
			// 门拒（模块已停用/正在收口）：下载根本不启动，如实回执并销账，
			// 形态与 ccswitch/douzy 的后台租约失败分支一致。
			slog.Info("omp: 后台下载未启动（调用门拒绝）", "version", targetVersion, "err", leaseErr)
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: leaseErr.Error()})
			return
		}
		defer lease.Release()
		// 首装自动激活的落账必须发生在 done 事件发出之前（前端共享 store 语义，
		// ccswitch 同款纪律）：done 先到而 active 后写，页面会闪一帧"未激活"。
		txnID := uuid.NewString()
		if err := s.manager.DownloadContext(lease.Context(), txnID, targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error("omp", "版本下载失败", fmt.Sprintf("Oh My Pi %s 下载失败: %v", targetVersion, err), "/ext/omp")
			return
		}
	}()

	return "started", nil
}

// ImportLocalVersion 导入本地已有的 omp exe（如官方脚本装的
// %LOCALAPPDATA%\omp\omp.exe）进托管版本树；返回导入后的版本信息。
// 版本令牌三级嗅探（远程摘要命中 → --version 探测 → imported 哈希兜底），
// 详见 version.Manager.ImportLocal。
func (s *OmpService) ImportLocalVersion(srcPath string) (version.OmpVersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return version.OmpVersionInfo{}, gateErr
	}
	defer release()
	srcPath = strings.TrimSpace(srcPath)
	info, err := s.manager.ImportLocal(context.Background(), uuid.NewString(), srcPath)
	if err != nil {
		return version.OmpVersionInfo{}, err
	}
	// 首装自动激活（与下载通道同一口径）
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
		info.Active = true
	}
	return info, nil
}

// SetActiveVersion 切换激活版本（目标版本必须已在版本树且 exe 可用）。
func (s *OmpService) SetActiveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if _, err := s.manager.ResolveExe(strings.TrimPrefix(targetVersion, "v")); err != nil {
		return err
	}
	return s.store.SetActive(targetVersion)
}

// DeleteVersion 卸载指定版本；若其正是激活版本则清空激活（回退最新已装）。
func (s *OmpService) DeleteVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = normalizeVersion(targetVersion)
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	if s.store.GetActive() == targetVersion {
		return s.store.SetActive("")
	}
	return nil
}

// ---------- 目录与仓库导航 ----------

// OpenDir 在资源管理器中打开指定目录（版本隔离目录 / ~/.omp 数据根）。
// 收口至 windows.RevealDir：非空与目录存在性校验及中文报错内置，explorer.exe
// <dir> 直启；刻意不走 explorer.exe <file> 的"执行"语义（markeron 事故教训）。
func (s *OmpService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// RepositoryURL 上游 GitHub 仓库地址（页面展示与复制）。
func (s *OmpService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return version.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面。
func (s *OmpService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.plat.OpenURL(version.RepoURL())
}

// ---------- 纯函数工具 ----------

// activeVersionFromPath 从版本目录路径反推展示版本（GetStatus 回退通道用）：
// .../omp_18.16.0/omp-windows-x64.exe → v18.16.0；imported 兜底目录同形透传。
func activeVersionFromPath(exePath string) string {
	dir := filepath.Base(filepath.Dir(exePath))
	if !strings.HasPrefix(dir, "omp_") {
		return ""
	}
	return "v" + strings.TrimPrefix(dir, "omp_")
}

// normalizeVersion 版本入参归一：先 TrimSpace，大小写 v 一律认（剥除既有 v/V
// 前缀后统一补 v）。imported-/vimported- 兜底令牌经"剥既有 v 再补 v"恒等透传
// 为 vimported-（绝不产出 vvimported-），与 ListInstalled 展示及 store 记账口径
// 一致——教训原文见 douzy/service.go 同函数注释（V 前缀三入口互异事故）。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	return "v" + v
}
