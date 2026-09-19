// Package mangodisk 集成 MangoDisk 原版 GUI 的版本管理和 JobObject 生命周期托管。
// 范围固定为纯托管：磁盘扫描、清理、卸载和系统设置均在上游窗口内完成。
package mangodisk

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "mangodisk"

// Module 是 extapi.Module 契约载体：仅持有 service 单例，自身无其他状态。
type Module struct{ svc *MangoDiskService }

// New 在 app 装配期创建模块（构造无 IO，重活在 OnInit 与 service 方法内）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewMangoDiskService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是模块实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID: ID, Name: "MangoDisk", Version: "0.1.0",
		Description: "MangoDisk 原版 GUI：官方版本校验、JobObject 托管启停与单实例窗口唤起",
		Author:      "Hanxi", Level: extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（Order/Group 决定桌面组内排序，详见 extapi.NavEntry 契约）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{ID: "mangodisk-manager", Title: "MangoDisk", Route: "/ext/mangodisk", Icon: "i:hard-drive", Section: extapi.SectionExt, Order: 75, Group: extapi.GroupDesktop}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// 本模块无权限申请；OnInit 启动外部实例嗅探 goroutine；
// OnDestroy 经 svc.shutdown 终止该 goroutine 并按"随 Hanxi 退出"开关收尾实例。
func (m *Module) Services() []extapi.Service   { return []extapi.Service{application.NewService(m.svc)} }
func (m *Module) OnInit(context.Context) error { m.svc.activate(); return nil }
func (m *Module) OnDestroy() error {
	// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)
	m.svc.shutdown()
	return nil
}
func (m *Module) IsInitialized() bool { return true }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 MangoDisk", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
