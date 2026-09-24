// Package rufus 内置模块：Rufus USB 启动盘制作工具托管
// （版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything/ccswitch/litemonitor 等完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 Rufus 代码，从上游 GitHub Releases 下载 Windows x64 便携单文件 exe
// （官方 sha256/字节数校验收口至共享内核 artifact.Fetch，MZ 魔数断言留模块）、
// 版本树 staging + 原子落位安装（artifact.Tree）、supervisor 内核托管 JobObject 生命周期、
// Win32 直操作唤窗（上游第二实例弹模态错误框、无唤窗契约）、
// 预置 rufus.ini 强制便携并关闭上游内置更新检查。
// 启动盘制作的全部操作在上游 Rufus 自有界面完成（纯托管决策：磁盘级写入是
// 数据销毁风险最高的操作，上游完整确认交互链就是产品本体，内嵌重做零性价比）。
// 上游 manifest 强制 requireAdministrator：托管启动要求 Hanxi 本身以管理员运行。
package rufus

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "rufus"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *RufusService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewRufusService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Rufus",
		Version:     "0.1.0",
		Description: "托管 USB 启动盘制作工具 Rufus：版本管理、JobObject 启停与窗口唤起（格式化 U 盘 / 写入 ISO 镜像）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "rufus-manager", Title: "Rufus 启动盘", Route: "/ext/rufus", Icon: "app:rufus", Section: extapi.SectionExt, Order: 88, Group: extapi.GroupSystem},
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

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较，返回语义见
// version.Manager.CheckUpdate 注释。
func (e *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return e.svc.manager.CheckUpdate(ctx)
}
