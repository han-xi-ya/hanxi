// Package nanazip integrates the official NanaZip stable MSIXBundle.
package nanazip

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/extapi"
	"hubkit/internal/platform"
)

const ID = "nanazip"

type Module struct{ svc *NanaZipService }

func New(plat platform.Platform) extapi.Module { return &Module{svc: NewNanaZipService(plat)} }

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{ID: ID, Name: "NanaZip", Version: "0.1.0", Description: "安装、升级和卸载 NanaZip 官方 stable MSIX 完整版", Author: "HubKit", Level: extapi.LevelBuiltin}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{ID: "nanazip-manager", Title: "NanaZip", Route: "/ext/nanazip", Icon: "NZ", Section: extapi.SectionExt, Order: 76}}
}

func (m *Module) Services() []extapi.Service       { return []extapi.Service{application.NewService(m.svc)} }
func (m *Module) Permissions() []extapi.Permission { return nil }
func (m *Module) Protocol() int                    { return 1 }
func (m *Module) OnInit(context.Context) error     { return nil }
func (m *Module) OnDestroy() error                 { return nil }
func (m *Module) IsInitialized() bool              { return true }
