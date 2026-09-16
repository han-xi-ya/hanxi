// Package wifi 内置模块：读取本机 Windows 已保存的 Wi-Fi 配置与明文密码
// （netsh wlan 输出解析）。查询型工具，无常驻资源。
package wifi

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "wifi"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *WifiService
}

// New 在 app 装配期创建模块（无外部依赖注入）。
func New() extapi.Module {
	return &Module{
		svc: NewWifiService(),
	}
}

// Info 返回模块元信息。明文密码展示受系统权限约束（部分配置需管理员），由上游 netsh 决定。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "WiFi 密码",
		Version:     "0.1.0",
		Description: "查看本机已保存的 Wi-Fi 网络明文密码",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定网络组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "WiFi 密码",
		Route:   "/ext/wifi",
		Icon:    "i:wifi",
		Section: extapi.SectionExt,
		Order:   45,
		Group:   extapi.GroupNetwork,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档；查询型工具，生命周期均无副作用。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission {
	return nil
}

func (e *Module) Protocol() int { return 1 }

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
