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
		svc: NewWebAppService(store, windows.OpenURL, extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部 RPC 导出版经该门取 operation lease（停用后不得开网页窗，Wave 3）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

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

// TrayCommands 把每条网址动态暴露为"打开网页窗"命令（托盘右键/轮盘候选目录，
// 用户自选挂载；命令 key 形如 webapp/open:<entryID>）。
// 纪律（registry.ListTrayCommands 持 wrapper 锁现调聚合）：只读条目配置，
// 绝不回调 registry（同锁重入死锁），且不依赖 OnInit——该聚合路径不 EnsureActive。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	entries := m.svc.store.GetWebAppEntries()
	cmds := make([]extapi.TrayCommand, 0, len(entries))
	for _, e := range entries {
		entryID := e.ID
		cmds = append(cmds, extapi.TrayCommand{
			ID:    "open:" + entryID,
			Label: e.Name,
			// Run 由 registry 在 Unlock 后执行（其内先 EnsureActive），可放心开窗；
			// 已开条目再点=Show+Focus 置顶，Open 天然幂等。
			Run: func(context.Context) error { return m.svc.Open(entryID) },
		})
	}
	return cmds
}
