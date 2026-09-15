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
		Version:     "0.2.0",
		Description: "Windows Subsystem for Linux：就绪体检、GitHub 通道诊断、官方版本管理、发行版管理控制台（六操作+克隆/导入/取证/wsl.conf/瘦身）与端口转发（面向 WSL2 完整内核工作流）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      "wsl-main",
		Title:   "WSL2",
		Route:   "/ext/wsl",
		Icon:    "🐧",
		Section: extapi.SectionExt,
		Order:   66,
	}}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermNetwork}
}

func (e *Module) Protocol() int { return 1 }

func (e *Module) OnInit(ctx context.Context) error {
	return nil // 探针与提权操作均按需触发，无常驻资源
}

func (e *Module) OnDestroy() error { return nil }

func (e *Module) IsInitialized() bool { return true }
