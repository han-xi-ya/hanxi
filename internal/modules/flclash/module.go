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

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "flclash"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *FlClashService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewFlClashService(plat)}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "FlClash 代理",
		Version:     "0.1.0",
		Description: "收纳 Clash 系代理客户端 FlClash：版本管理、JobObject 托管启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "flclash-manager", Title: "FlClash 代理", Route: "/ext/flclash", Icon: "i:shield", Section: extapi.SectionExt, Order: 80, Group: extapi.GroupNetwork},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
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

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 FlClash", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
