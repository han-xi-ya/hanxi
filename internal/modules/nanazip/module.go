// Package nanazip integrates the official NanaZip stable MSIXBundle.
package nanazip

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "nanazip"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct{ svc *NanaZipService }

// New 在 app 装配期创建模块（构造无 IO，包查询/操作全部延迟到 service 方法）。
func New(plat platform.Platform) extapi.Module { return &Module{svc: NewNanaZipService(plat)} }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{ID: ID, Name: "NanaZip", Version: "0.1.0", Description: "安装、升级和卸载 NanaZip 官方 stable MSIX 完整版", Author: "Hanxi", Level: extapi.LevelBuiltin}
}

// Nav 声明侧边栏入口（Order/Group 决定桌面组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{ID: "nanazip-manager", Title: "NanaZip", Route: "/ext/nanazip", Icon: "i:archive", Section: extapi.SectionExt, Order: 76, Group: extapi.GroupDesktop}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档；
// 生命周期无重资源：OnInit/OnDestroy 均空操作，包操作由 service 自身的单槽位互斥管理。
func (m *Module) Services() []extapi.Service   { return []extapi.Service{application.NewService(m.svc)} }
func (m *Module) OnInit(context.Context) error { return nil }
func (m *Module) OnDestroy() error             { return nil }
func (m *Module) IsInitialized() bool          { return true }
