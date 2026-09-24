// Package snipaste 集成 Snipaste 官方 Windows x64 免安装版。
// Snipaste 为闭源软件，本模块负责官网下载、版本管理，以及当前 Hanxi 会话
// 自有实例的启动/手动退出；模块停用或 Hanxi 退出不联动结束 Snipaste。
package snipaste

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "snipaste"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *SnipasteService
}

// New 在 app 装配期创建模块（构造无 IO；本模块 OnInit 亦无重活，懒初始化仅为统一生命周期）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewSnipasteService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Snipaste",
		Version:     "0.1.0",
		Description: "管理并启动 Snipaste 官方 Windows 免安装版，保留原生托盘与快捷键",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（Order/Group 决定效率组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "snipaste-manager", Title: "Snipaste", Route: "/ext/snipaste", Icon: "app:snipaste", Section: extapi.SectionExt, Order: 75, Group: extapi.GroupEfficiency},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档；无权限申请、无初始化副作用。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

func (m *Module) OnInit(context.Context) error { return nil }

// OnDestroy 不终止已启动的 Snipaste：页面手动退出是唯一控制入口，
// 模块停用或 Hanxi 退出仍保留原生托盘与快捷键。
func (m *Module) OnDestroy() error { return nil }

// IsInitialized 本模块 OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool { return true }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 Snipaste", Run: func(context.Context) error {
			_, err := m.svc.Launch()
			return err
		}},
	}
}

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较，返回语义见
// version.Manager.CheckUpdate 注释。
func (e *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return e.svc.manager.CheckUpdate(ctx)
}
