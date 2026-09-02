// Package envcheck 内置模块：开发环境检测。
// 探测本机开发工具链，并只读查询 Git for Windows 官网近期稳定版本。
package envcheck

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "envcheck"

type Module struct {
	svc *EnvCheckService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewEnvCheckService(plat),
	}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "开发环境检测",
		Version:     "0.2.0",
		Description: "检测本机开发工具链，并查询 Git、Go 与 Node.js 官网版本",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      "envcheck-main",
		Title:   "开发环境检测",
		Route:   "/ext/envcheck",
		Icon:    "🧰",
		Section: extapi.SectionExt,
		Order:   65,
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
	return nil // 全同步探测，无资源需要初始化
}

func (e *Module) OnDestroy() error {
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}
