// Package frpc 提供 frp 内网穿透项目、多实例与版本管理能力。
// 它与其他工具模块平等注册、启停和释放资源。
// 已落地 M4.1 版本管理引擎；M4.2~M4.5 的 TOML 生成、多实例进程管理、日志流持续推进。
package frpc

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "frpc"

type Module struct {
	svc *FrpcService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewFrpcService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "frpc 联调",
		Version:     "0.1.0",
		Description: "管理 frp 内网穿透项目、多实例进程与本地版本",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "frpc-projects", Title: "frpc 穿透", Route: "/frpc", Icon: "⧉", Section: extapi.SectionCore, Order: 10},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

func (e *Module) OnInit(ctx context.Context) error {
	return nil
}

func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}
