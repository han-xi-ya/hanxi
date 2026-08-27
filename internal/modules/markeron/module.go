// Package markeron 内置模块：MarkerOn 屏幕标注工具托管（版本管理 + 标注开关）。
// 与 frpc/lan/portkill 完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 MarkerOn 代码，从上游 GitHub releases 下载 portable zip、
// 解压隔离安装、JobObject 托管生命周期、经单实例协议二次拉起实现标注开关。
package markeron

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/extapi"
	"hubkit/internal/platform"
)

const ID = "markeron"

type Module struct {
	svc *MarkerOnService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewMarkerOnService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "MarkerOn 标注",
		Version:     "0.1.0",
		Description: "收纳屏幕标注工具 MarkerOn：版本管理、JobObject 托管启停与标注开关",
		Author:      "HubKit",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "markeron-annotate", Title: "MarkerOn 标注", Route: "/ext/markeron", Icon: "✎", Section: extapi.SectionExt, Order: 55},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 wechat 等常驻模块不同）
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}
