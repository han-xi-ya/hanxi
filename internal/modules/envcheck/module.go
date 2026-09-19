// Package envcheck 内置模块：开发环境检测。
// 探测本机开发工具链，只读查询 Git、Go、Node.js、Java、Python 与 .NET 官方版本，
// 并对目录内 npm 全局 CLI 工具（Claude Code、Codex 等）提供一键安装/升级/卸载。
package envcheck

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "envcheck"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *EnvCheckService
}

// New 在 app 装配期创建模块；plat 仅用于"打开官网"类跳转，无其他资源。
func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewEnvCheckService(plat, extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "开发环境检测",
		Version:     "0.5.0",
		Description: "检测本机开发工具链，查询官网版本，并对 Claude Code、Codex 等 npm 全局工具一键安装/升级/卸载",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定系统组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      "envcheck-main",
		Title:   "开发环境检测",
		Route:   "/ext/envcheck",
		Icon:    "i:wrench",
		Section: extapi.SectionExt,
		Order:   65,
		Group:   extapi.GroupSystem,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) OnInit(ctx context.Context) error {
	return nil // 探测与 npm 操作均按需触发，无常驻资源需要初始化
}

func (e *Module) OnDestroy() error {
	return nil
}

// IsInitialized 本模块无常驻资源与失败路径，懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}
