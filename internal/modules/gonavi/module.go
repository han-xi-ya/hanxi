// Package gonavi 内置模块：GoNavi 数据库管理工具托管（版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/ccswitch/litemonitor 等完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 GoNavi 代码，从上游 GitHub Releases 下载 Windows x64 便携 zip
// （GitHub API digest 官方 sha256，双源俱缺的 release 宁拒不猜不入列表）、保布局
// 解压隔离安装、JobObject 托管生命周期、进程名 + EnumWindows 直操作唤窗
// （便携态无单实例互斥体，二次拉起即多开，信使路线不存在）。
// 数据库连接与 SQL 操作全部在 GoNavi 自有窗口内完成（纯托管决策：其界面即
// 产品价值，且直连用户数据库，由原版实现最稳妥）。数据恒在 ~/.gonavi，
// 托管不动用户数据、不随版本目录隔离。
package gonavi

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "gonavi"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *GoNaviService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewGoNaviService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "GoNavi",
		Version:     "0.1.0",
		Description: "托管轻量数据库管理工具 GoNavi：版本管理、JobObject 启停与窗口唤起（MySQL/PostgreSQL 等图形客户端；数据在 ~/.gonavi 不随版本隔离）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
// Icon 用矢量 i:database——图标池无 gonavi 真图（红线提取候选另议），如实降级；
// Order 73 为全模块扫描后的 developer 组空位（70=ccswitch，90 起为编辑器/终端簇）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "gonavi-manager", Title: "GoNavi", Route: "/ext/gonavi", Icon: "i:database", Section: extapi.SectionExt, Order: 73, Group: extapi.GroupDeveloper},
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
	// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)
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
		{ID: "launch", Label: "启动 GoNavi", Run: func(context.Context) error {
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
