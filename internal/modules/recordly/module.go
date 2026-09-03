// Package recordly 内置模块：Recordly 演示录屏工具托管（版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything/ccswitch 完全平等的模块——统一注册、统一启停。
//
// 方案要点（集成决策记录）：
//   - 不移植 Recordly 代码（AGPL-3.0 + 品牌/署名附加条款：只做官方原版二进制
//     的下载托管，绝不内嵌进 Hanxi 分发包；操作全程在 Recordly 自有窗口完成，
//     其录屏依赖原生 helper 进程树，内嵌重做性价比为零——纯托管为拍板方案）；
//   - 上游 Windows 仅提供 NSIS 在线安装器（无官方便携包），"免安装"经
//     `/S /D=<托管目录>` 静默实现；oneClick 语义决定单版本目录 + 外部安装卫兵
//     （详见 version.Manager 注释）；
//   - 托管启动注入 RECORDLY_DISABLE_AUTO_UPDATES=1（上游官方开关），
//     版本升级统一走 Hanxi 版本管理；
//   - 信使语义（Electron requestSingleInstanceLock 实证），外部实例仅感知
//     不接管，Quit 不越权；上游 Windows 版无托盘，快捷方式装后清理防绕过。
package recordly

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "recordly"

type Module struct {
	svc *RecordlyService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewRecordlyService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Recordly",
		Version:     "0.1.0",
		Description: "托管开源录屏工具 Recordly：双通道版本管理、NSIS 静默安装、JobObject 启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "recordly-manager", Title: "Recordly 录屏", Route: "/ext/recordly", Icon: "🎬", Section: extapi.SectionExt, Order: 78},
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
