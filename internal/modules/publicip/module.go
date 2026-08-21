// Package publicip 内置扩展：公网 IP（MVP 阶段仅注册导航与权限声明，功能在 M2 实现）。
package publicip

import "hubkit/internal/extapi"

const ID = "publicip"

type Module struct{}

func New() extapi.Module { return &Module{} }

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "公网 IP",
		Version:     "0.1.0",
		Description: "查询并展示公网 IPv4/IPv6 地址",
		Author:      "HubKit",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "公网 IP",
		Route:   "/ext/publicip",
		Icon:    "≋",
		Section: extapi.SectionExt,
		Order:   50,
	}}
}

func (e *Module) Services() []extapi.Service { return nil }

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermNetwork}
}

func (e *Module) Protocol() int { return 1 }