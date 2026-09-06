// Package vscode 内置模块：Visual Studio Code 双形态托管（版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything/ccswitch 完全平等的模块——统一注册、统一启停。
// 方案要点（集成决策记录，2026-09 上游侦查实证）：
//   - 上游二进制仅微软官方 CDN 分发（GitHub releases 无二进制），远程列表/直链/
//     官方 sha256 全走 update.code.visualstudio.com；官方哈希仅最新版可得，
//     历史版本降级三层完整性校验；
//   - 便携版（zip + data\ 自包含）为主托管通道：版本隔离安装、多版本并存、
//     无应用内自动更新漂移；
//   - 安装版（User Installer, Inno 免 UAC）经 Hanxi 静默安装/升级：安装位置以
//     注册表为准；与用户日常 VS Code 同实例组（唤窗互达）、应用内自动更新会
//     令托管版本记录漂移——两处已知约束在控制台如实预告（用户拍板接受）；
//   - 编辑器功能一概不内嵌：操作在 VS Code 自有窗口完成（其界面即产品）。
package vscode

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "vscode"

type Module struct {
	svc *VSCodeService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewVSCodeService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "VS Code",
		Version:     "0.1.0",
		Description: "托管 Visual Studio Code：便携版版本管理 + 安装版静默升级，JobObject 启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "vscode-manager", Title: "VS Code", Route: "/ext/vscode", Icon: "💻", Section: extapi.SectionExt, Order: 90},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 ccswitch/everything 同策略）
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
		{ID: "launch", Label: "启动 VS Code", Run: func(context.Context) error {
			_, err := m.svc.OpenPreferredWindow()
			return err
		}},
	}
}
