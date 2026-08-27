// Package flclash 内置模块：FlClash 代理客户端托管
// （版本管理 + JobObject 托管启停 + 窗口唤起 + 闲置自动退出）。
// 与 frpc/markeron/everything/ccswitch/bcu 完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 FlClash 代码，从上游 GitHub Releases 下载 Windows 便携 zip
// （官方 sha256 四层校验）、解压隔离安装、JobObject 托管生命周期。
// 上游单实例是文件锁且第二实例不唤窗——窗口唤起由本模块 EnumWindows 直接
// 置前台（自有/外部实例通用），不依赖二次启动信使。
// 代理订阅与节点配置在 FlClash 自有窗口内完成（界面完整，无内嵌分叉）。
package flclash

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/extapi"
	"hubkit/internal/platform"
)

const ID = "flclash"

type Module struct {
	svc *FlClashService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewFlClashService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "FlClash 代理",
		Version:     "0.1.0",
		Description: "收纳 Clash 系代理客户端 FlClash：版本管理、JobObject 托管启停与窗口唤起",
		Author:      "HubKit",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "flclash-manager", Title: "FlClash 代理", Route: "/ext/flclash", Icon: "⚡", Section: extapi.SectionExt, Order: 80},
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
