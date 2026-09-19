// Package markeron 内置模块：MarkerOn 屏幕标注工具托管（版本管理 + 标注开关）。
// 与 frpc/lan/portkill 完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 MarkerOn 代码，从上游 GitHub releases 下载 portable zip、
// 解压隔离安装、JobObject 托管生命周期、经单实例协议二次拉起实现标注开关。
package markeron

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "markeron"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *MarkerOnService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewMarkerOnService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "MarkerOn 标注",
		Version:     "0.1.0",
		Description: "收纳屏幕标注工具 MarkerOn：版本管理、JobObject 托管启停与标注开关",
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
		{ID: "markeron-annotate", Title: "MarkerOn 标注", Route: "/ext/markeron", Icon: "i:pen-line", Section: extapi.SectionExt, Order: 55, Group: extapi.GroupDesktop},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 wechat 等常驻模块不同）
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
