// Package subnetdesk 内置模块：SubnetDesk 局域网远程桌面托管
// （版本管理 + JobObject 托管启停 + 窗口唤起），与 rustdesk 模块配成
// "远程控制"组合：SubnetDesk 管局域网/VPN 直连（mDNS 发现、IP:21118、
// 用户名/密码 Argon2id），RustDesk 管跨公网 ID/中继接入。
//
// 方案要点与决策记录：
//   - SubnetDesk 是 RustDesk 的独立 LAN fork（AGPL-3.0）：移除了公网设备 ID、
//     rendezvous/中继与云账户路径。两模块**协议互不兼容**（子网端点+账密认证 vs
//     ID+中继认证），"组合"= Hanxi 导航同组提供两条托管入口，各自被控端需装
//     对应软件——非互操作，页面文案如实说明；
//   - 纯托管模式（不内嵌界面）：远程桌面画面即上游 GUI 本体，内嵌重做不可行；
//   - 便携 exe 为 rust-portable packer 单文件（无 zip）：下载即安装；内层解压至
//     %LOCALAPPDATA%\subnetdesk，实例探测/归属判定据此设计（见 instance 包注释）；
//   - 端口 TCP 21118（RustDesk 为 21116/21117）、数据目录 %APPDATA%\SubnetDesk，
//     两模块同机并行运行互不冲突。
package subnetdesk

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "subnetdesk"

type Module struct {
	svc *SubnetDeskService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewSubnetDeskService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "SubnetDesk",
		Version:     "0.1.0",
		Description: "局域网直连远程桌面（RustDesk LAN fork）：版本管理、JobObject 托管启停与窗口唤起，默认端口 TCP 21118",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "subnetdesk-manager", Title: "SubnetDesk 局域网", Route: "/ext/subnetdesk", Icon: "🖥", Section: extapi.SectionExt, Order: 86},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 ccswitch/everything 同策略）
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
// 复用与模块页面"打开窗口"完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "打开 SubnetDesk", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
