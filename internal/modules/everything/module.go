// Package everything 内置模块：Everything 文件搜索工具托管
// （版本管理 + JobObject 托管启停 + 内嵌搜索 + 本地整套导入）。
// 与 frpc/markeron 完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 Everything 代码，从官网便携 zip 下载隔离安装（官方 sha256 校验），
// 经 -startup/-quit 命令行契约托管生命周期，经 ES.exe 官方 CLI 内嵌索引搜索。
package everything

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "everything"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *EverythingService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewEverythingService(plat)}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Everything 搜索",
		Version:     "0.1.0",
		Description: "收纳文件搜索工具 Everything：版本管理、后台索引托管与内嵌秒级文件搜索",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "everything-search", Title: "Everything 搜索", Route: "/ext/everything", Icon: "i:search-code", Section: extapi.SectionExt, Order: 60, Group: extapi.GroupEfficiency},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron 同策略）
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 Everything", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
