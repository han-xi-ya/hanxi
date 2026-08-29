package publicip

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "publicip"

type Module struct {
	svc *PublicIPService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewPublicIPService(plat),
	}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "IP 查看",
		Version:     "0.1.0",
		Description: "查看公网 IPv4/IPv6、局域网 IP、临时 IPv6、网关与 DNS",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "IP 查看",
		Route:   "/ext/publicip",
		Icon:    "≋",
		Section: extapi.SectionExt,
		Order:   50,
	}}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermNetwork}
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
