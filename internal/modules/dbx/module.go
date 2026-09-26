// Package dbx 内置模块：DBX 数据库客户端托管（版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 ccswitch/gonavi/litemonitor 等完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 DBX 代码，从上游 GitHub Releases（t8y2/dbx）下载 Windows x64
// 便携 zip（GitHub API digest 官方 sha256 唯一信任根——上游 minisign .sig
// 不消费，宁拒不猜）、保布局解压隔离安装（portable.dbx 标记随目录保留作
// 数据模式双保险）、JobObject 托管生命周期；托管启动注入 DBX_DATA_DIR 将
// 数据改道 Hanxi 数据根（阶段 0 裁决：跨版本共享、删版本不丢数据）。
// 唤窗经 tauri-plugin-single-instance 协议无参二次拉起（信使 handoff，
// ccswitch 同族形制）；数据库连接与 SQL 操作全部在 DBX 自有窗口内完成
// （纯托管决策：其界面即产品价值，且直连用户数据库，由原版实现最稳妥）。
package dbx

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "dbx"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *DBXService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewDBXService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "DBX",
		Version:     "0.1.0",
		Description: "托管 Tauri 数据库客户端 DBX：版本管理、JobObject 启停与信使唤窗（数据改道 Hanxi 数据根，删版本不丢数据）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
// Icon 用矢量 i:server——图标池无 dbx 真图（assets/apps/ 无此键、提取红线
// 候选另议），且刻意与 gonavi（i:database，同为数据库工具）错开免混淆；
// Order 74 为全模块扫描后的 developer 组空位（70=ccswitch，73=gonavi 并行
// 线在途，74 相邻同簇；DB 工具簇最终排序以主会话 navigation 终调为准）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "dbx-manager", Title: "DBX", Route: "/ext/dbx", Icon: "i:server", Section: extapi.SectionExt, Order: 74, Group: extapi.GroupDeveloper},
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
		{ID: "launch", Label: "启动 DBX", Run: func(context.Context) error {
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
