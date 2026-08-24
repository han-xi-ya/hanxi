package frpc

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/domain"
	"hubkit/internal/modules/frpc/docgen"
	"hubkit/internal/modules/frpc/instance"
	"hubkit/internal/modules/frpc/version"
	"hubkit/internal/platform"
	"hubkit/internal/settings"
)

// FrpcService 向前端暴露 frp 版本管理与项目实例管理能力
// （M4.1 版本管理 + M4.2 项目 CRUD + M4.3 多实例运行引擎）。
type FrpcService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *frpcStore
	engine  *instance.Manager

	runDir     string // 实例配置落盘目录（RuntimeDir）
	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
}

func NewFrpcService(plat platform.Platform) *FrpcService {
	paths := settings.GetPaths()
	svc := &FrpcService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newFrpcStore(paths.DataDir()),
		runDir:  filepath.Join(paths.RuntimeDir(), "frpc"),
	}
	svc.engine = instance.NewManager(plat.Job(), instance.Callbacks{
		OnState: svc.emitInstanceState,
		OnLog:   svc.emitInstanceLog,
	})
	return svc
}

// ---------- 实例事件推送（M4.3） ----------

// emitInstanceState 实例状态迁移 → 事件 frpc:instance-state。
func (s *FrpcService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("frpc instance state", "project", snap.ProjectID, "state", snap.State, "pid", snap.PID)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("frpc:instance-state", snap)
	}
}

// emitInstanceLog 实例日志行 → 事件 frpc:instance-log。
func (s *FrpcService) emitInstanceLog(projectID, line string) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("frpc:instance-log", instance.LogEntry{ProjectID: projectID, Line: line})
	}
}

// ListReleases 获取远程可用版本列表（GitHub 官方源 + 镜像回退，10 分钟缓存）
func (s *FrpcService) ListReleases() ([]version.FrpRelease, error) {
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表
func (s *FrpcService) ListInstalledVersions() ([]version.FrpVersionInfo, error) {
	return s.manager.ListInstalled()
}

// DownloadVersion 后台下载指定版本：立即返回，全程经由事件 frpc:version-download 推送进度。
func (s *FrpcService) DownloadVersion(targetVersion string) (string, error) {
	targetVersion = strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	targetVersion = "v" + targetVersion

	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()

	// 已安装则直接返回，避免重复下载
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(strings.TrimPrefix(v.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
				return "already-installed", nil
			}
		}
	}

	go func() {
		emit := func(p version.DownloadProgress) {
			slog.Debug("frpc download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("frpc:version-download", p)
			}
		}
		if err := s.manager.Download(targetVersion, emit); err != nil {
			emit(version.DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
		}
	}()

	return "started", nil
}

// ImportLocalFrpc 弹窗选择本地 frpc.exe 并导入（自动探测版本号）
func (s *FrpcService) ImportLocalFrpc() (version.FrpVersionInfo, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return version.FrpVersionInfo{}, fmt.Errorf("dialog unavailable")
	}
	dialog := app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter("frpc 可执行文件", "*.exe").
		SetTitle("选择 frpc.exe")
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return version.FrpVersionInfo{}, err
	}
	if path == "" {
		return version.FrpVersionInfo{}, nil // 用户取消
	}
	slog.Info("importing local frpc", "path", path)
	return s.manager.ImportLocal(path)
}

// RemoveVersion 卸载已安装版本
func (s *FrpcService) RemoveVersion(targetVersion string) error {
	return s.manager.Remove(strings.TrimSpace(targetVersion))
}

// ---------- M4.2 项目 CRUD ----------

// ListProjects 返回全部 frpc 项目
func (s *FrpcService) ListProjects() ([]domain.Project, error) {
	return s.store.List()
}

// GetProject 按 ID 查询单个项目
func (s *FrpcService) GetProject(id string) (*domain.Project, error) {
	p, ok := s.store.Get(strings.TrimSpace(id))
	if !ok {
		return nil, fmt.Errorf("项目 %s 不存在", id)
	}
	return &p, nil
}

// SaveProject 新建或更新项目（空 ID 表示新建）
func (s *FrpcService) SaveProject(p domain.Project) (domain.Project, error) {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" && p.ID == "" {
		return domain.Project{}, fmt.Errorf("项目名称不能为空")
	}

	// 校验服务端地址必填（TOML 生成要求）
	if _, err := docgen.Generate(&p); err != nil {
		return domain.Project{}, err
	}

	if err := s.store.Save(&p); err != nil {
		return domain.Project{}, err
	}
	saved, _ := s.store.Get(p.ID)
	return saved, nil
}

// DeleteProject 删除项目（若实例在运行则先停止再移除）
func (s *FrpcService) DeleteProject(id string) error {
	id = strings.TrimSpace(id)
	if err := s.StopProject(id); err != nil {
		slog.Warn("stop before delete failed", "project", id, "err", err)
	}
	s.engine.Remove(id)
	_ = os.Remove(filepath.Join(s.runDir, projectConfigName(id))) // 清理已生成的运行时配置
	return s.store.Delete(id)
}

// projectConfigName 实例运行配置文件名（frpc-<projectID>.toml）
func projectConfigName(id string) string {
	return "frpc-" + id + ".toml"
}

// GenerateToml 生成项目配置的 TOML 预览文本（不落盘，供编辑页展示与校验）
func (s *FrpcService) GenerateToml(p domain.Project) (string, error) {
	return docgen.Generate(&p)
}

// ParseToml 从用户粘贴/导入的 frp TOML 配置解析回领域模型（兼容 v1.x 与 v0.x 格式）
func (s *FrpcService) ParseToml(content string) (domain.Project, error) {
	p, err := docgen.Parse(content)
	if err != nil {
		return domain.Project{}, err
	}
	// TOML 中无项目命名，自动起名
	p.Name = ""
	p.ID = ""
	return p, nil
}

// ---------- M4.3 多实例运行 ----------

// StartProject 启动项目实例：解析绑定版本 → 生成 TOML 落盘 → 拉起 frpc.exe 并绑定 JobObject。
// 启动后状态/日志经事件 frpc:instance-state / frpc:instance-log 持续推送。
func (s *FrpcService) StartProject(id string) error {
	id = strings.TrimSpace(id)
	p, ok := s.store.Get(id)
	if !ok {
		return fmt.Errorf("项目 %s 不存在", id)
	}
	if p.Version == "" {
		return fmt.Errorf("项目 %s 未绑定 frp 版本，请先在版本管理导入并编辑项目绑定", p.Name)
	}

	exe, err := s.manager.ResolveExe(p.Version)
	if err != nil {
		return err
	}

	toml, err := docgen.Generate(&p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.runDir, 0755); err != nil {
		return err
	}
	cfgPath := filepath.Join(s.runDir, projectConfigName(id))
	if err := os.WriteFile(cfgPath, []byte(toml), 0644); err != nil {
		return err
	}

	var redact []string
	if p.Server.Token != "" {
		redact = append(redact, p.Server.Token)
	}
	if err := s.engine.Start(instance.StartOptions{
		ProjectID:   id,
		ProjectName: p.Name,
		Version:     p.Version,
		FrpcExe:     exe,
		ConfigPath:  cfgPath,
		Redact:      redact,
	}); err != nil {
		return err
	}
	slog.Info("frpc instance started", "project", p.Name, "version", p.Version, "exe", exe)
	return nil
}

// StopProject 停止项目实例（幂等），并清除生成的运行时临时配置。
func (s *FrpcService) StopProject(id string) error {
	id = strings.TrimSpace(id)
	if err := s.engine.Stop(id); err != nil {
		return fmt.Errorf("停止实例失败: %w", err)
	}
	// 立即擦除生成的临时 TOML 配置文件，防止敏感 Token 闲置残留
	cfgPath := filepath.Join(s.runDir, projectConfigName(id))
	_ = os.Remove(cfgPath)
	return nil
}

// ListInstanceStates 返回全部项目实例状态（含已停止的历史实例）。
func (s *FrpcService) ListInstanceStates() ([]instance.Snapshot, error) {
	return s.engine.AllSnapshots(), nil
}

// GetProjectLogs 拉取项目实例最近日志（lastN <= 0 返回全部缓冲）。
func (s *FrpcService) GetProjectLogs(id string, lastN int) ([]string, error) {
	logs, err := s.engine.Logs(strings.TrimSpace(id), lastN)
	if err != nil {
		return nil, err
	}
	if logs == nil {
		return []string{}, nil
	}
	return logs, nil
}

// Shutdown 销毁实例引擎，终止所有正在运行的 frpc 子进程
func (s *FrpcService) Shutdown() {
	if s.engine != nil {
		s.engine.Shutdown()
	}
}
