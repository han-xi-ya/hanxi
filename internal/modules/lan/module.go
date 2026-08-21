// Package lan 内置扩展：局域网扫描（MVP 阶段仅注册导航与权限声明，功能在 M2 实现）。
package lan

import "hubkit/internal/extapi"

const ID = "lan"

type Module struct{}

func New() extapi.Module { return &Module{} }

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "局域网扫描",
		Version:     "0.1.0",
		Description: "扫描局域网在线设备并复制 IP 地址",
		Author:      "HubKit",
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

func (e *Module) Services() []extapi.Service { return nil }

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermLANScan}
}

func (e *Module) Protocol() int { return 1 }