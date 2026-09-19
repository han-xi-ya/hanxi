// Package publicip 内置模块：聚合公网 IPv4/IPv6（多源冗余查询 + 短 TTL 缓存）、
// 局域网地址、网关与 DNS 与网卡详情。只读查询，无常驻资源。
package publicip

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "publicip"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *PublicIPService
}

// New 在 app 装配期创建模块；plat 提供网卡枚举，公网查询走标准库 HTTP。
func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewPublicIPService(plat, extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "IP 查看",
		Version:     "0.1.0",
		Description: "查看公网 IPv4/IPv6、局域网 IP、临时 IPv6、网关与 DNS",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定网络组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "IP 查看",
		Route:   "/ext/publicip",
		Icon:    "i:globe",
		Section: extapi.SectionExt,
		Order:   50,
		Group:   extapi.GroupNetwork,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// PermNetwork 覆盖 ipify/3322 等公网 IP 探测源；查询型工具，生命周期无副作用。
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
