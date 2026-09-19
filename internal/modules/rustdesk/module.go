// Package rustdesk 内置模块：RustDesk 跨公网远程桌面托管
// （版本管理 + JobObject 托管启停 + 窗口唤起），与 subnetdesk 模块配成
// "远程控制"组合：RustDesk 管跨公网 ID/中继接入（默认官方公共信令，
// 可自备自建 rendezvous/relay 服务器），SubnetDesk 管局域网/VPN 直连。
//
// 方案要点与决策记录：
//   - RustDesk 本体（AGPL-3.0）与 LAN fork SubnetDesk **协议互不兼容**
//     （ID+中继认证 vs 子网端点+账密认证）："组合"= Hanxi 导航同组提供两条
//     托管入口，两端需装对应软件——非互操作，页面文案如实说明；
//   - 纯托管模式（不内嵌界面）：远程桌面画面即上游 GUI 本体，内嵌重做不可行；
//   - 便携 exe 为 rust-portable packer 单文件（无 zip）：下载即安装；内层解压至
//     %LOCALAPPDATA%\rustdesk，实例探测/归属判定据此设计（见 instance 包注释）；
//   - 端口 TCP 21116/21117（SubnetDesk 为 21118）、数据目录 %APPDATA%\RustDesk，
//     两模块同机并行运行互不冲突；
//   - 便携版边界：不装 Windows 服务，被控仅在便携进程存活期间可被连接，
//     锁屏/安全桌面场景受限（安装版专属能力），页面如实提示。
package rustdesk

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "rustdesk"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *RustDeskService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewRustDeskService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "RustDesk",
		Version:     "0.1.0",
		Description: "跨公网 ID/中继远程桌面（自托管首选）：版本管理、JobObject 托管启停与窗口唤起，支持自建信号服务器",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "rustdesk-manager", Title: "RustDesk 公网", Route: "/ext/rustdesk", Icon: "i:cast", Section: extapi.SectionExt, Order: 87, Group: extapi.GroupNetwork},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 ccswitch/everything 同策略）
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (e *Module) OnDestroy() error {
	e.svc.shutdown()
	return nil
}

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (e *Module) IsInitialized() bool {
	return true
}

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"打开窗口"完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "打开 RustDesk", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
