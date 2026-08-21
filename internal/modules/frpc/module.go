// Package frpc 内置模块：frpc 联调（产品核心）。
// 与 lan/portkill/publicip 完全平等的模块——统一注册、统一启停。
// 业务实现（项目 CRUD、多实例进程管理、配置生成、日志流）在 M4 落地，
// 本包目前只提供元信息、导航与未来的服务装配入口。
package frpc

import "hubkit/internal/extapi"

const ID = "frpc"

type Module struct{}

func New() extapi.Module { return &Module{} }

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

func (e *Module) Services() []extapi.Service { return nil }

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }