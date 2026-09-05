// Package envcheck 内置模块：开发环境检测。
// 探测本机开发工具链，只读查询 Git、Go、Node.js、Java、Python 与 .NET 官方版本，
// 并对目录内 npm 全局 CLI 工具（Claude Code、Codex 等）提供一键安装/升级/卸载。
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
		Version:     "0.5.0",
		Description: "检测本机开发工具链，查询官网版本，并对 Claude Code、Codex 等 npm 全局工具一键安装/升级/卸载",
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
	return nil // 探测与 npm 操作均按需触发，无常驻资源需要初始化
}

func (e *Module) OnDestroy() error {
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}
