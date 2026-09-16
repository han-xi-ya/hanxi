// Package frpc 提供 frp 内网穿透项目、多实例与版本管理能力。
// 它与其他工具模块平等注册、启停和释放资源。
// 已落地 M4.1 版本管理引擎；M4.2~M4.5 的 TOML 生成、多实例进程管理、日志流持续推进。
package frpc

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "frpc"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *FrpcService
}

// New 在 app 装配期创建模块（构造无 IO；实例进程均由 service 方法按需拉起）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewFrpcService(plat)}
}

// Info 返回模块元信息。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "frpc 联调",
		Version:     "0.1.0",
		Description: "管理 frp 内网穿透项目、多实例进程与本地版本",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明导航入口：frpc 属核心页（SectionCore，非 /ext 前缀），固定排网络组首位。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "frpc-projects", Title: "frpc 穿透", Route: "/frpc", Icon: "i:zap", Section: extapi.SectionCore, Order: 10, Group: extapi.GroupNetwork},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// OnDestroy 经 svc.Shutdown 收敛全部 frpc 子进程/日志流 goroutine，停用后不得有残留。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

func (e *Module) OnInit(ctx context.Context) error {
	return nil
}

func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

// IsInitialized 本模块 OnInit 无副作用，懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}
