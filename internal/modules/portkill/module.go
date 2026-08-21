// Package portkill 内置扩展：释放端口（MVP 阶段仅注册导航与权限声明，功能在 M3 实现）。
package portkill

import "hubkit/internal/extapi"

const ID = "portkill"

type Module struct{}

func New() extapi.Module { return &Module{} }

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "释放端口",
		Version:     "0.1.0",
		Description: "按端口定位占用进程，复核后安全结束",
		Author:      "HubKit",
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

func (e *Module) Services() []extapi.Service { return nil }

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermKillProcess}
}

func (e *Module) Protocol() int { return 1 }