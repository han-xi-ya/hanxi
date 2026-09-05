// Package rufus 内置模块：Rufus USB 启动盘制作工具托管
// （版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything/ccswitch/litemonitor 等完全平等的模块——统一注册、统一启停。
// 方案要点：不移植 Rufus 代码，从上游 GitHub Releases 下载 Windows x64 便携单文件 exe
// （GitHub API digest 官方 sha256 + 字节数 + MZ 魔数三重校验）、隔离目录安装、
// JobObject 托管生命周期、Win32 直操作唤窗（上游第二实例弹模态错误框、无唤窗契约）、
// 预置 rufus.ini 强制便携并关闭上游内置更新检查。
// 启动盘制作的全部操作在上游 Rufus 自有界面完成（纯托管决策：磁盘级写入是
// 数据销毁风险最高的操作，上游完整确认交互链就是产品本体，内嵌重做零性价比）。
// 上游 manifest 强制 requireAdministrator：托管启动要求 Hanxi 本身以管理员运行。
package rufus

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "rufus"

type Module struct {
	svc *RufusService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewRufusService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Rufus",
		Version:     "0.1.0",
		Description: "托管 USB 启动盘制作工具 Rufus：版本管理、JobObject 启停与窗口唤起（格式化 U 盘 / 写入 ISO 镜像）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "rufus-manager", Title: "Rufus 启动盘", Route: "/ext/rufus", Icon: "💽", Section: extapi.SectionExt, Order: 88},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/ccswitch 同策略）
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
