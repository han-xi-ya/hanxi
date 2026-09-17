// Package webapp 的模块装配契约载体，包语义见 service.go 包注释。
package webapp

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// ID 是模块注册键；纯内置 HTTP 无关模块，仅条目存储与窗口编排。
const ID = "webapp"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *WebAppService
}

// New 在 app 装配期创建模块；条目由 settings.Store 持久化，构造无网络/窗口 IO。
func New(store *settings.Store) extapi.Module {
	return &Module{
		svc: NewWebAppService(store, windows.OpenURL),
	}
}

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "网页应用",
		Version:     "0.1.0",
		Description: "把常用网站当作独立窗口使用：网址条目管理、内嵌窗口打开、轮盘/托盘直达（预置微信文件传输助手）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order 37 取 efficiency 组 36/55 之间的空位）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "网页应用",
		Route:   "/ext/webapp",
		Icon:    "i:monitor",
		Section: extapi.SectionExt,
		Order:   37,
		Group:   extapi.GroupEfficiency,
	}}
}

// Services 暴露 WebAppService 给前端（开窗/收起/条目 CRUD 均经该服务）。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

// OnInit 无后台常驻资源（监听器/钩子均无），恒成功；窗口生命周期由服务层自管。
func (m *Module) OnInit(ctx context.Context) error { return nil }

// OnDestroy 真销毁全部存活网页窗（停用/退出即归还 WebView2 内存）。
func (m *Module) OnDestroy() error {
	m.svc.shutdown()
	return nil
}

// IsInitialized 无失败路径，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool { return true }
