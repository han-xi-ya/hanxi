package portkill

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "portkill"

type Module struct {
	svc *PortKillService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewPortKillService(plat),
	}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "释放端口",
		Version:     "0.1.0",
		Description: "按端口定位占用进程，复核后安全结束",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "释放端口",
		Route:   "/ext/portkill",
		Icon:    "✕",
		Section: extapi.SectionExt,
		Order:   40,
	}}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermKillProcess}
}

func (e *Module) Protocol() int { return 1 }

func (e *Module) OnInit(ctx context.Context) error {
	return nil
}

func (e *Module) OnDestroy() error {
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}
