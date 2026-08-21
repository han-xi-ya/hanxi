// Package frpc 内置模块：frpc 联调（产品核心）。
// 与 lan/portkill/publicip 完全平等的模块——统一注册、统一启停。
// 已落地 M4.1 版本管理引擎；M4.2~M4.5 的 TOML 生成、多实例进程管理、日志流持续推进。
package frpc

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/extapi"
	"hubkit/internal/platform"
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
		Description: "核心模块：frp 内网穿透项目、多实例、版本管理",
		Author:      "HubKit",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "frpc-projects", Title: "frpc 项目", Route: "/frpc", Icon: "⧉", Section: extapi.SectionCore, Order: 10},
		{ID: "frpc-versions", Title: "版本管理", Route: "/versions", Icon: "⬇", Section: extapi.SectionCore, Order: 20},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }
