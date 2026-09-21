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

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "litemonitor"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *LiteMonitorService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewLiteMonitorService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "LiteMonitor",
		Version:     "0.1.0",
		Description: "托管桌面硬件监控 LiteMonitor：版本管理、JobObject 启停与窗口唤起（CPU/GPU/内存/磁盘/网速横条与任务栏显示；上游另具内存清理、FPS 计数与插件扩展）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "litemonitor-manager", Title: "LiteMonitor", Route: "/ext/litemonitor", Icon: "i:activity", Section: extapi.SectionExt, Order: 84, Group: extapi.GroupSystem},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/ccswitch 同策略）
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (e *Module) OnDestroy() error {
	e.svc.shutdown()
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
		{ID: "launch", Label: "启动 LiteMonitor", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较，返回语义见
// version.Manager.CheckUpdate 注释。
func (e *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return e.svc.manager.CheckUpdate(ctx)
}
