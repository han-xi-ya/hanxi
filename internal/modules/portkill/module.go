// Package portkill 内置模块：按端口定位占用进程并安全结束。
// 查杀前经 platform.ProcessAPI 复核 PID 身份（路径+启动时间），系统红线进程拒杀，
// 权限不足时返回 needElevate 建议而非静默失败。
package portkill

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "portkill"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *PortKillService
}

// New 在 app 装配期创建模块；plat 提供端口表与进程查杀能力。
func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewPortKillService(plat, extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "释放端口",
		Version:     "0.1.0",
		Description: "按端口定位占用进程，复核后安全结束",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定系统组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "释放端口",
		Route:   "/ext/portkill",
		Icon:    "i:x-octagon",
		Section: extapi.SectionExt,
		Order:   40,
		Group:   extapi.GroupSystem,
	}}
}

// Service 暴露服务实例供装配根接线统一历史（SetHistory，照 ocr.Module.Service 先例）。
func (e *Module) Service() *PortKillService { return e.svc }

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档；
// PermKillProcess 声明结束进程权限，查询与查杀均为按需短任务，无常驻资源可清理。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) OnInit(ctx context.Context) error {
	return nil
}

func (e *Module) OnDestroy() error {
	return nil
}

// IsInitialized 本模块 OnInit 无副作用，懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}
