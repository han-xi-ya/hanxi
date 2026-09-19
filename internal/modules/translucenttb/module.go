// Package translucenttb 内置模块：TranslucentTB 任务栏透明工具托管（版本管理 + JobObject 托管启停 + 状态信使）。
// 与 frpc/ccswitch/everything 完全平等的模块——统一注册、统一启停。
//
// 方案要点（纯托管决策记录，阶段 0 侦查实证）：
//   - 不移植上游代码（C++/WinRT + 注入 explorer 的 TAP/Hooks，内嵌重做毫无性价比），
//     从 GitHub Releases 下载 TranslucentTB-portable-x64.zip（官方 sha256 四层校验）、
//     解压隔离安装、JobObject 托管生命周期；
//   - 上游全部设置 UI 就在系统托盘 XAML 飞控（无主设置窗口），故本模块没有"唤窗"，
//     取而代之的是"重设任务栏状态"（经单实例互斥体协议拉起状态信使）；
//   - 便携版仅支持 Windows 11 且依赖系统已装 WinUI 2.8 / VCLibs 框架包（上游 README 与
//     dynamicdependency.cpp 双重实证），引导文案如实预告；
//   - 上游自带 hideTray 配置但托盘飞控是其唯一常规 UI——不代开隐藏开关，防用户失去入口。
package translucenttb

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "translucenttb"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *TranslucentTBService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewTranslucentTBService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "TranslucentTB",
		Version:     "0.1.0",
		Description: "收纳任务栏透明工具 TranslucentTB：版本管理、JobObject 托管启停与任务栏状态重设",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "translucenttb-manager", Title: "TranslucentTB 透明栏", Route: "/ext/translucenttb", Icon: "i:layers", Section: extapi.SectionExt, Order: 91, Group: extapi.GroupDesktop},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 ccswitch/everything 同策略）
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
		{ID: "launch", Label: "启动 TranslucentTB", Run: func(context.Context) error {
			_, err := m.svc.Start()
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
