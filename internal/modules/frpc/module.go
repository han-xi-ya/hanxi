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
	return &Module{svc: NewFrpcService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

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
		{ID: "frpc-projects", Title: "frpc 穿透", Route: "/frpc", Icon: "app:frpc", Section: extapi.SectionCore, Order: 10, Group: extapi.GroupNetwork},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// OnDestroy 经 svc.shutdown 收敛全部 frpc 子进程/日志流 goroutine，停用后不得有残留。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 启动期清扫孤儿运行时配置：此刻必无实例在跑，runDir 里的
// frpc-*.toml 都是上次崩溃/强杀的残留（S0，见 TROUBLESHOOTING #55）。
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.cleanupRuntimeConfigs()
	return nil
}

func (e *Module) OnDestroy() error {
	// 装配布线:Go 直调路径,不得依赖运行态（见 ADR-0001 Wave 3 注记）
	e.svc.shutdown()
	return nil
}

// IsInitialized 本模块 OnInit 仅做幂等的孤儿配置清扫（无失败路径阻断），
// 懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}
