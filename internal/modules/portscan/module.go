// Package portscan 提供内置端口扫描模块：原生 TCP 并发 connect 探测 + 可选 Nmap 深度指纹，
// 并附带经代理/直连的出网 IP 检测。无外部托管进程，仅依赖标准库与 wails 事件推送。
package portscan

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "portscan"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *PortScanService
}

// New 在 app 装配期创建模块（扫描引擎无状态，无需外部依赖注入）。
func New() extapi.Module {
	return &Module{
		svc: NewPortScanService(),
	}
}

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "端口扫描与指纹",
		Version:     "0.1.0",
		Description: "高并发多端口快速扫描，集成 Nmap 深度服务指纹识别",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定网络组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "端口扫描",
		Route:   "/ext/portscan",
		Icon:    "i:search",
		Section: extapi.SectionExt,
		Order:   25,
		Group:   extapi.GroupNetwork,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// StopScan("") 按空任务 ID 约定取消全部在途扫描，模块停用不留孤儿 goroutine。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermNetwork} // 网络扫描基础权限
}

func (m *Module) Protocol() int { return 1 }

func (m *Module) OnInit(ctx context.Context) error {
	return nil
}

func (m *Module) OnDestroy() error {
	// 停用模块时强制终止所有正在进行的扫描
	m.svc.StopScan("")
	return nil
}

// IsInitialized 本模块 OnInit 无副作用，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool {
	return true
}
