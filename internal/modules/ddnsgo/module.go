// Package ddnsgo 内置模块：ddns-go 动态域名解析工具托管
// （版本管理 + JobObject 托管启停 + 内嵌 Webview 子窗口控制台）。
// 方案要点：不移植 ddns-go 代码，从上游 GitHub Releases 下载 Windows x64 zip
// （GitHub API digest 官方 sha256 四层校验）、解压隔离安装、JobObject 托管生命周期。
// 管理界面经 Wails 子 Webview 窗口直达上游原生页面（决策记录：上游界面为完整
// Web UI，含全量 DNS 服务商配置与日志页；内嵌选择独立顶层 Webview 窗口而非
// iframe——上游会话 Cookie 无 SameSite 属性按 Lax 处理，跨站 iframe 场景
// 登录态必坏，实测上游页面亦无 frame 豁免设计）。
// 配置无缝接管：恒用上游约定路径 %USERPROFILE%\.ddns_go_config.yaml，
// 与用户自行运行的实例共享同一份配置。
package ddnsgo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "ddnsgo"

type Module struct {
	svc *DdnsGoService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewDdnsGoService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "ddns-go",
		Version:     "0.1.0",
		Description: "托管动态域名解析工具 ddns-go：版本管理、JobObject 启停与内嵌 Web 控制台",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "ddnsgo-manager", Title: "ddns-go 解析", Route: "/ext/ddnsgo", Icon: "🌐", Section: extapi.SectionExt, Order: 85},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 ccswitch 同策略）
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
// 复用与模块页面完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
// DDNS 属长驻后台服务，托盘语义取"确保运行并打开面板"（可立即看到上次更新结果）。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "打开 ddns-go 控制台", Run: func(context.Context) error {
			_, err := m.svc.OpenConsole()
			return err
		}},
	}
}
