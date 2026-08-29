package lan

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const ID = "lan"

type Module struct {
	svc *LanService
}

func New(plat platform.Platform, store *settings.Store) extapi.Module {
	return &Module{
		svc: NewLanService(plat, store),
	}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "局域网扫描",
		Version:     "0.1.0",
		Description: "扫描局域网在线设备、备注设备信息并快速复制 IP",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "局域网扫描",
		Route:   "/ext/lan",
		Icon:    "◉",
		Section: extapi.SectionExt,
		Order:   30,
	}}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermLANScan}
}

func (e *Module) Protocol() int { return 1 }

func (e *Module) OnInit(ctx context.Context) error {
	// 懒加载初始化：无需分配重型常驻资源
	return nil
}

func (e *Module) OnDestroy() error {
	// 停用模块时强制取消正在进行的任何扫描操作并释放上下文
	e.svc.Cancel()
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}
