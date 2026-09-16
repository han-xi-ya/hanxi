// Package ccswitch 内置模块：CC Switch 供应商切换工具托管（版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything 完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 CC Switch 代码，从上游 GitHub Releases 下载 Windows 便携 zip
// （GitHub API digest 官方 sha256 四层校验）、解压隔离安装、JobObject 托管生命周期、
// 经 tauri-plugin-single-instance 协议无参二次拉起唤起主窗口。
// 供应商切换操作在 CC Switch 自有窗口内完成（其界面已完整，内嵌重做性价比低）。
package ccswitch

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "ccswitch"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *CCSwitchService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewCCSwitchService(plat)}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "CC Switch",
		Version:     "0.1.0",
		Description: "收纳 Claude Code / Codex 多供应商切换工具 CC Switch：版本管理、JobObject 托管启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "ccswitch-manager", Title: "CC Switch", Route: "/ext/ccswitch", Icon: "i:shuffle", Section: extapi.SectionExt, Order: 70, Group: extapi.GroupDeveloper},
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

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/everything 同策略）
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
		{ID: "launch", Label: "启动 CC Switch", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
