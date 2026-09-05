// Package bili23 内置模块：Bili23 Downloader（B 站视频下载器）托管（版本管理 +
// JobObject 托管启停 + 窗口唤起/优雅退出编排）。
// 与 frpc/markeron/everything/ccswitch 完全平等的模块——统一注册、统一启停。
//
// 方案要点（纯托管，决策记录）：不移植 Bili23 代码，从上游 GitHub Releases 下载
// Windows 便携 zip（官方 sha256 四层校验）、剥离顶层目录后整目录隔离安装、
// JobObject 托管生命周期、经上游单实例协议（命名互斥体 + QLocalServer 信使）
// 唤起主窗口。解析与下载操作全部在 Bili23 自有窗口内完成——其 GUI 已高度完整
// （扫码登录/批量解析/命名规则引擎/弹幕字幕），内嵌重做性价比极低；上游另有
// 可选 MCP 服务器（默认关闭），作为未来增值路线记录在案，本期不接。
//
// 与 ccswitch 模板的本质差异：上游"关闭窗口"行为用户可配（退出/最小化托盘/询问，
// 默认询问），外部无法保证优雅退出，且下载器强杀有中断在途任务的代价——
// Quit 不做静默强杀兜底，改为三态结果（exited/hidden/windowUp）如实上报，
// 强杀仅保留给用户显式点击的「强制结束」。
package bili23

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "bili23"

type Module struct {
	svc *Service
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Bili23 Downloader",
		Version:     "0.1.0",
		Description: "收纳开源 B 站视频下载器 Bili23 Downloader：版本管理、JobObject 托管启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "bili23-manager", Title: "Bili23 下载", Route: "/ext/bili23", Icon: "📺", Section: extapi.SectionExt, Order: 89},
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
// 复用与模块页面"打开窗口"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 Bili23 下载", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
