// Package bcu 内置模块：Bulk Crap Uninstaller 批量卸载工具托管
// （版本管理 + JobObject 托管启停 + 窗口唤起 + 闲置自动退出）。
// 与 frpc/markeron/everything/ccswitch 完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 BCU 代码，从上游 GitHub Releases 下载自包含便携 zip
// （官方 sha256 四层校验）、解压隔离安装、JobObject 托管生命周期、
// 经 Global\BCU-singleinstance 单实例协议无参二次拉起唤起主窗口。
// 卸载操作在 BCU 自有窗口内完成（界面完整，无内嵌分叉）。
package bcu

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "bcu"

type Module struct {
	svc *BCUService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewBCUService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "BC 卸载工具",
		Version:     "0.1.0",
		Description: "收纳批量卸载工具 Bulk Crap Uninstaller：版本管理、JobObject 托管启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "bcu-manager", Title: "BC 卸载工具", Route: "/ext/bcu", Icon: "🧹", Section: extapi.SectionExt, Order: 75},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知与空闲退出巡检（懒加载，与其他工具模块同策略）
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
