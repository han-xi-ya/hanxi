// Package mangodisk 集成 MangoDisk 原版 GUI 的版本管理和 JobObject 生命周期托管。
// 范围固定为纯托管：磁盘扫描、清理、卸载和系统设置均在上游窗口内完成。
package mangodisk

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "mangodisk"

type Module struct{ svc *MangoDiskService }

func New(plat platform.Platform) extapi.Module { return &Module{svc: NewMangoDiskService(plat)} }

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID: ID, Name: "MangoDisk", Version: "0.1.0",
		Description: "MangoDisk 原版 GUI：官方版本校验、JobObject 托管启停与单实例窗口唤起",
		Author:      "Hanxi", Level: extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{ID: "mangodisk-manager", Title: "MangoDisk", Route: "/ext/mangodisk", Icon: "🥭", Section: extapi.SectionExt, Order: 75}}
}

func (m *Module) Services() []extapi.Service       { return []extapi.Service{application.NewService(m.svc)} }
func (m *Module) Permissions() []extapi.Permission { return nil }
func (m *Module) Protocol() int                    { return 1 }
func (m *Module) OnInit(context.Context) error     { m.svc.activate(); return nil }
func (m *Module) OnDestroy() error                 { m.svc.Shutdown(); return nil }
func (m *Module) IsInitialized() bool              { return true }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 MangoDisk", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
