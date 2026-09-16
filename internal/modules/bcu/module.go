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

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "bcu"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *BCUService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewBCUService(plat)}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
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

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "bcu-manager", Title: "BC 卸载工具", Route: "/ext/bcu", Icon: "i:trash-2", Section: extapi.SectionExt, Order: 75, Group: extapi.GroupSystem},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知与空闲退出巡检（懒加载，与其他工具模块同策略）
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}
