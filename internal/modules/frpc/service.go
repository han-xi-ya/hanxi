package frpc

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/domain"
	"hubkit/internal/modules/frpc/docgen"
	"hubkit/internal/modules/frpc/version"
	"hubkit/internal/platform"
	"hubkit/internal/settings"
)

// FrpcService 向前端暴露 frp 版本管理与项目实例管理能力（M4.1 版本管理 + M4.2 项目 CRUD 已落地）。
type FrpcService struct {
	plat    platform.Platform
	manager *version.Manager
	store   *frpcStore

	downloadMu sync.Mutex // 防止同一时间并发触发多个下载
}

func NewFrpcService(plat platform.Platform) *FrpcService {
	paths := settings.GetPaths()
	return &FrpcService{
		plat:    plat,
		manager: version.NewManager(paths.VersionsDir()),
		store:   newFrpcStore(paths.DataDir()),
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

// DeleteProject 删除项目
func (s *FrpcService) DeleteProject(id string) error {
	return s.store.Delete(strings.TrimSpace(id))
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
