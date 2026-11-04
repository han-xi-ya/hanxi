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
	ops     *officialOps // 官方安装方式操作集（探测/脚本/PATH 回收，见 official.go）

	downloadMu   sync.Mutex
	downloads    map[string]struct{}
	officialMu   sync.Mutex
	officialBusy bool // 同一时刻只允许一趟官方脚本（install/update 共用槽位）
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
		ops:       newOfficialOps(),
		downloads: make(map[string]struct{}),
	}
}

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

	st := OmpStatus{ActiveVersion: s.store.GetActive()}
	st.OfficialDir, st.OfficialPresent, st.OfficialVersion, st.OnPath = s.ops.probe()
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

// ---------- 官方安装方式（机主拍板的主通道）----------

// InstallOfficial 一键安装=后台跑官方 install.ps1 -Binary（下载最新单文件、
// 官方自校验、落 %LOCALAPPDATA%\omp、写 PATH、Git Bash 接线全套官方行为）。
// 进度经事件 omp:official-op 回流；已在跑则报 in-progress（同一时刻只允许一趟官方脚本）。
func (s *OmpService) InstallOfficial() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.startOfficial("install")
}

// UpdateOfficial 一键更新=再跑一遍官方脚本（脚本语义即"拉最新覆盖装"，幂等）。
func (s *OmpService) UpdateOfficial() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.startOfficial("update")
}

// startOfficial install/update 共用编排（调用门由两个导出 RPC 各自持有，本私有件
// 承接已放行路径；互斥 officialMu 槽位）。
func (s *OmpService) startOfficial(op string) (string, error) {
	s.officialMu.Lock()
	if s.officialBusy {
		s.officialMu.Unlock()
		return "in-progress", nil
	}
	s.officialBusy = true
	s.officialMu.Unlock()

	go func() {
		defer func() {
			s.officialMu.Lock()
			s.officialBusy = false
			s.officialMu.Unlock()
		}()
		emit := func(stage, msg string) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("omp:official-op", OfficialOpProgress{Op: op, Stage: stage, Message: msg})
			}
		}
		emit("running", "正在执行官方安装脚本（下载校验全程由脚本自理）…")
		lease, leaseErr := s.holder.EnterBackground(context.Background())
		if leaseErr != nil {
			emit("error", leaseErr.Error())
			return
		}
		defer lease.Release()
		out, err := s.ops.runOfficialScript(lease.Context())
		if err != nil {
			emit("error", fmt.Sprintf("%s: %v", firstLineFallback(out, "脚本无输出"), err))
			notify.Error("omp", "官方脚本失败", err.Error(), "/ext/omp")
			return
		}
		verb := "安装"
		if op == "update" {
			verb = "更新"
		}
		emit("done", fmt.Sprintf("omp 已%s：%s", verb, s.ops.probeVer(s.ops.exePath())))
		notify.Success("omp", "官方"+verb+"完成", "新开终端敲 omp 即可使用（凭据与数据在 ~/.omp 未动）", "/ext/omp")
	}()
	return "started", nil
}

// UninstallOfficial 一键卸载=删官方目录 + 从用户 PATH 剔除该条目 + 广播；
// ~/.omp 数据根（凭据/会话/记忆）按官方口径保留。危险确认在前端适配层。
func (s *OmpService) UninstallOfficial() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.ops.uninstall()
}

// firstLineFallback 取脚本输出首行做错误简报，空则用兜底文案。
func firstLineFallback(out, fallback string) string {
	if line := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]; line != "" {
		return line
	}
	return fallback
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
