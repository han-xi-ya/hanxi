package wifi

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
)

const ID = "wifi"

type Module struct {
	svc *WifiService
}

func New() extapi.Module {
	return &Module{
		svc: NewWifiService(),
	}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "WiFi 密码",
		Version:     "0.1.0",
		Description: "查看本机已保存的 Wi-Fi 网络明文密码",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "WiFi 密码",
		Route:   "/ext/wifi",
		Icon:    "📶",
		Section: extapi.SectionExt,
		Order:   45,
	}}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission {
	return nil
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
