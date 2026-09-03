// Package snipaste 集成 Snipaste 官方 Windows x64 免安装版。
// Snipaste 为闭源软件，本模块负责官网下载、版本管理，以及当前 Hanxi 会话
// 自有实例的启动/手动退出；模块停用或 Hanxi 退出不联动结束 Snipaste。
package snipaste

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "snipaste"

type Module struct {
	svc *SnipasteService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewSnipasteService(plat)}
}

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Snipaste",
		Version:     "0.1.0",
		Description: "管理并启动 Snipaste 官方 Windows 免安装版，保留原生托盘与快捷键",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "snipaste-manager", Title: "Snipaste", Route: "/ext/snipaste", Icon: "✂", Section: extapi.SectionExt, Order: 75},
	}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

func (m *Module) Permissions() []extapi.Permission { return nil }
func (m *Module) Protocol() int                    { return 1 }
func (m *Module) OnInit(context.Context) error     { return nil }

// OnDestroy 不终止已启动的 Snipaste：页面手动退出是唯一控制入口，
// 模块停用或 Hanxi 退出仍保留原生托盘与快捷键。
func (m *Module) OnDestroy() error    { return nil }
func (m *Module) IsInitialized() bool { return true }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 Snipaste", Run: func(context.Context) error {
			_, err := m.svc.Launch()
			return err
		}},
	}
}
