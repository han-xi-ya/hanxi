package app

import (
	"runtime"

	"hubkit/internal/domain"
	"hubkit/internal/extapi"
)

// AppInfo 前端关于页/首页展示的应用信息。
type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
}

// AppService 是暴露给前端的基础服务：
// 应用信息、模块清单与导航（扩展注入的入口）。
type AppService struct {
	registry *extapi.Registry
}

func NewAppService(registry *extapi.Registry) *AppService {
	return &AppService{registry: registry}
}

func (s *AppService) GetAppInfo() AppInfo {
	return AppInfo{Name: Name, Version: Version, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

// GetNavs 返回前端左侧导航（核心 + 已启用扩展）。
func (s *AppService) GetNavs() []extapi.NavEntry {
	return s.registry.GetEnabledNavs()
}

// ListModules 返回模块清单与启用状态（设置页）。
func (s *AppService) ListModules() []extapi.ModuleInfo {
	return s.registry.List()
}

// SetModuleEnabled 设置页开关模块；启用/禁用成功返回最新元信息。
func (s *AppService) SetModuleEnabled(id string, enabled bool) (*extapi.ModuleInfo, error) {
	if err := s.registry.SetEnabled(id, enabled); err != nil {
		ae := domain.NewAppError(domain.ErrValidation, "无效的扩展 ID")
		ae.Cause = err
		return nil, ae
	}
	info := s.registry.List()
	for i := range info {
		if info[i].ID == id {
			return &info[i], nil
		}
	}
	return nil, nil
}
