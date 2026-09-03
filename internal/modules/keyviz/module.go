// Package keyviz 内置模块：Keyviz 按键可视化托管
// （版本管理 + JobObject 托管启停 + 状态探测）。
// 与 frpc/markeron/everything/ccswitch/piclite 完全平等的模块——统一注册、统一启停。
//
// 方案决策记录（侦查阶段实证，详见 instance 包注释）：
//   - 纯托管不内嵌：Keyviz 的设置界面与可视化 overlay 完整且依赖全局键盘钩子，
//     内嵌重做性价比低；控制台只承担版本管理与进程生命周期；
//   - MSI 管理提取路线：上游 v2 正式版线不发便携 zip（唯一 zip 停留在两年前
//     预发布），msiexec /a 管理提取免管理员、免注册表副作用拆出单 exe，
//     v2.1.1 已实测（PFiles\keyviz\keyviz.exe，官方 digest 一致）；
//   - 无"打开设置窗口"按钮：上游单实例回调是空函数，唤窗契约不存在，
//     设置入口只在托盘菜单（左键即弹）——前端以提示条如实指引，不造假按钮；
//   - 退出即强杀：上游退出仅存在于托盘回调（process::exit(0)），无任何外部
//     优雅通道；store.json 修改即节流写盘，强杀不丢历史设置；
//   - 许可合规：上游 GPL-3.0，托管模式仅启动上游官方二进制进程、不链接不
//     分发其代码，无传染性；用户数据（%APPDATA%\org.keyviz）不被 Hanxi 读写。
package keyviz

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "keyviz"

type Module struct {
	svc *KeyvizService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewKeyvizService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Keyviz",
		Version:     "0.1.0",
		Description: "开源按键/鼠标可视化工具 Keyviz：MSI 托管安装、JobObject 启停与运行状态探测",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "keyviz-manager", Title: "Keyviz 键显", Route: "/ext/keyviz", Icon: "⌨️", Section: extapi.SectionExt, Order: 82},
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
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 Keyviz", Run: func(context.Context) error {
			_, err := m.svc.StartKeyviz()
			return err
		}},
	}
}
