// Package litemonitor 内置模块：LiteMonitor 桌面硬件监控托管
// （版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything/ccswitch 等完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 LiteMonitor 代码，从上游 GitHub Releases 下载 Windows x64 便携 zip
// （GitHub API digest 官方 sha256 四层校验）、嵌套布局解压隔离安装、
// JobObject 托管生命周期、Win32 直操作唤窗（上游第二实例静默退出无唤窗契约）。
// 监控条/任务栏显示与全部配置操作在 LiteMonitor 自有界面完成
// （用户拍板纯托管：其横条/任务栏形态本就是产品价值，内嵌重做无意义）。
package litemonitor

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "litemonitor"

type Module struct {
	svc *LiteMonitorService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewLiteMonitorService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "LiteMonitor",
		Version:     "0.1.0",
		Description: "托管桌面硬件监控 LiteMonitor：版本管理、JobObject 启停与窗口唤起（CPU/GPU/内存/磁盘/网速横条与任务栏显示）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "litemonitor-manager", Title: "LiteMonitor", Route: "/ext/litemonitor", Icon: "📊", Section: extapi.SectionExt, Order: 84},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/ccswitch 同策略）
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

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 LiteMonitor", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
