// Package envcheck 内置模块：开发环境检测
// （探测本机开发工具链 git/node/java/python/npm/pnpm/go 的安装路径与版本）。
// 无状态模块（对齐 wifi）：纯 exec 探测、无 store、无事件、无后台协程，
// 打开页面即实时检测，任何时刻不占用常驻资源。
package envcheck

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
)

const ID = "envcheck"

type Module struct {
	svc *EnvCheckService
}

func New() extapi.Module {
	return &Module{
		svc: NewEnvCheckService(),
	}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "开发环境检测",
		Version:     "0.1.0",
		Description: "检测本机开发工具链（git/node/java/python/npm/pnpm/go）的安装路径与版本",
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
	return nil
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
