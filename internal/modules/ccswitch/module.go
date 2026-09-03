// Package ccswitch 内置模块：CC Switch 供应商切换工具托管（版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything 完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 CC Switch 代码，从上游 GitHub Releases 下载 Windows 便携 zip
// （GitHub API digest 官方 sha256 四层校验）、解压隔离安装、JobObject 托管生命周期、
// 经 tauri-plugin-single-instance 协议无参二次拉起唤起主窗口。
// 供应商切换操作在 CC Switch 自有窗口内完成（其界面已完整，内嵌重做性价比低）。
package ccswitch

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "ccswitch"

type Module struct {
	svc *CCSwitchService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewCCSwitchService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "CC Switch",
		Version:     "0.1.0",
		Description: "收纳 Claude Code / Codex 多供应商切换工具 CC Switch：版本管理、JobObject 托管启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "ccswitch-manager", Title: "CC Switch", Route: "/ext/ccswitch", Icon: "🔀", Section: extapi.SectionExt, Order: 70},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/everything 同策略）
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 CC Switch", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
