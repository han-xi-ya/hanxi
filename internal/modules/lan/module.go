// Package lan 内置模块：局域网设备扫描。基于 platform 网卡/邻居表 API 做 ARP 式在线探测，
// 结合 mDNS/主机名识别设备，设备备注经 settings.Store 持久化。只读扫描，不改动网络配置。
package lan

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "lan"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *LanService
}

// New 在 app 装配期创建模块；plat 提供网卡/邻居表能力，store 持久化设备备注。
func New(plat platform.Platform, store *settings.Store) extapi.Module {
	return &Module{
		svc: NewLanService(plat, store),
	}
}

// Info 返回模块元信息。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "局域网扫描",
		Version:     "0.1.0",
		Description: "扫描局域网在线设备、备注设备信息并快速复制 IP",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定网络组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "局域网扫描",
		Route:   "/ext/lan",
		Icon:    "i:radar",
		Section: extapi.SectionExt,
		Order:   30,
		Group:   extapi.GroupNetwork,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// PermLANScan 声明批量扫描权限；OnDestroy 的 Cancel 取消在途扫描 context，
// 保证模块停用后无 goroutine 与 socket 残留。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermLANScan}
}

func (e *Module) Protocol() int { return 1 }

func (e *Module) OnInit(ctx context.Context) error {
	// 懒加载初始化：无需分配重型常驻资源
	return nil
}

func (e *Module) OnDestroy() error {
	// 停用模块时强制取消正在进行的任何扫描操作并释放上下文
	e.svc.Cancel()
	return nil
}

// IsInitialized 本模块 OnInit 无副作用，懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}
