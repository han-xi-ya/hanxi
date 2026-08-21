package portscan

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/extapi"
)

const ID = "portscan"

type Module struct {
	svc *PortScanService
}

func New() extapi.Module {
	return &Module{
		svc: NewPortScanService(),
	}
}

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "端口扫描与指纹",
		Version:     "0.1.0",
		Description: "高并发多端口快速扫描，集成 Nmap 深度服务指纹识别",
		Author:      "HubKit",
		Level:       extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "端口扫描",
		Route:   "/ext/portscan",
		Icon:    "🔍",
		Section: extapi.SectionExt,
		Order:   25,
	}}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermNetwork} // 网络扫描基础权限
}

func (m *Module) Protocol() int { return 1 }
