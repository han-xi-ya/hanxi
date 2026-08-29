package fileshare

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "fileshare"

// Module 局域网快传模块
type Module struct {
	svc *FileShareService
}

// New 实例化模块
func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewFileShareService(plat),
	}
}

// Service 获取底层业务服务实例
func (m *Module) Service() *FileShareService {
	return m.svc
}

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "局域网文件快传",
		Version:     "0.1.0",
		Description: "零客户端依赖的局域网极速文件/文本分享站，手机电脑扫码即用",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "局域网快传",
		Icon:    "📁",
		Route:   "/ext/fileshare",
		Section: extapi.SectionExt,
		Order:   35,
	}}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermNetwork}
}

func (m *Module) Protocol() int { return 1 }

func (m *Module) OnInit(ctx context.Context) error {
	return nil
}

func (m *Module) OnDestroy() error {
	return m.svc.StopServer()
}

func (m *Module) IsInitialized() bool {
	return true
}
