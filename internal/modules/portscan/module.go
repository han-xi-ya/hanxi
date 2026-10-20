// Package portscan 提供内置端口扫描模块：原生 TCP 并发 connect 探测 + 自研轻量
// 服务指纹（scanner.go 的 lightweightProbe：主动握手与被动 Banner 抓取覆盖
// SSH/FTP/SMTP/MySQL/Redis，另作 HTTP 头部识别），并附带经代理/直连的出网 IP 检测。
// 无外部托管进程，仅依赖标准库与 wails 事件推送。
//
// 诚实边界（2026-10-12 订正）：本模块**不含也不调用 Nmap**——历史上包注释与本模块
// Description 曾写「可选 Nmap 深度指纹」「集成 Nmap 深度服务指纹识别」，仓内无该引擎、
// go.mod 无相关依赖，属文案超前于实现（docs/FEATURES_OVERVIEW.md 早已点名）。Description
// 经 scripts/cataloggen 流入 internal/app/catalog.go、scripts/fixture/module_catalog.json
// 与前端 Info 面板，故一处失真会同时污染对外展示；今日按实能力改述，勿再回写旧口径。
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
		svc: NewPortScanService(extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID: ID,
		// 名称与描述随能力扩展更新：这一页现在管三件事（端口扫描 / 代理连通性 /
		// SSH 连接测试），"端口扫描"已经盖不住。**catalog.go 与 module_catalog.json
		// 两份产物刻意未 regen**——它们正被另一会话在途修改，此处只改源头，
		// 等那轮入库后一笔补齐（Info().Name / Nav().Title 经 cataloggen 流入产物）。
		Name:        "网络测试",
		Version:     "0.1.0",
		Description: "高并发多端口扫描与自研轻量 Banner / HTTP 服务指纹（不含 Nmap），另含代理连通性与 SSH 连接测试",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定网络组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "网络测试",
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

func (m *Module) OnInit(ctx context.Context) error {
	return nil
}

func (m *Module) OnDestroy() error {
	// 停用模块时强制终止所有正在进行的扫描：走未接门的内部版
	// （生命周期收口路径，停用流程中调用门已关，不得依赖运行态）
	m.svc.stopScan("")
	return nil
}

// IsInitialized 本模块 OnInit 无副作用，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool {
	return true
}
