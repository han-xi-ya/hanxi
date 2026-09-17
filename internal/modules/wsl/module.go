package wsl

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const ID = "wsl"

type Module struct {
	svc *WslService
}

func New(plat platform.Platform, paths *settings.Paths) extapi.Module {
	return &Module{svc: NewWslService(plat, paths)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "WSL2",
		Version:     "0.3.0",
		Description: "Windows Subsystem for Linux：就绪体检、GitHub 通道诊断、官方版本管理、发行版管理控制台（六操作+克隆/导入/取证/wsl.conf/瘦身）、端口转发与 USB 直通（usbipd-win 集成，面向 WSL2 完整内核工作流）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      "wsl-main",
		Title:   "WSL2",
		Route:   "/ext/wsl",
		Icon:    "i:terminal",
		Section: extapi.SectionExt,
		Order:   66,
		Group:   extapi.GroupDeveloper,
	}}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) OnInit(ctx context.Context) error {
	// 探针与提权操作按需触发；唯一常驻接线是 USB 自动共享的启动重放——
	// 账本空或总开关关时秒退（runUsbReplay 内部门禁），不为无事可做的会话留驻资源。
	e.svc.scheduleUsbStartupReplay()
	return nil
}

func (e *Module) OnDestroy() error {
	e.svc.cancelUsbReplay() // 收回未到点的启动重放/在飞等待（F9 生命周期收口）
	return nil
}

func (e *Module) IsInitialized() bool { return true }
