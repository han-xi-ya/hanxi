// Package eartrumpet 以 Windows 应用包形态集成 EarTrumpet（每应用音量
// 控制托盘工具），纳管官方直装渠道：检测、启动、退出、卸载、安装更新。
//
// 集成决策记录（为何不套用 markeron/ccswitch 等"便携托管"模板）：
//   - 发行形态：GitHub Releases 停留在 1.3.2.0 且无任何资产，不存在
//     官方便携 zip 与官方 digest，版本管理四层完整性无从谈起；现代 2.x
//     经 Microsoft Store 与上游自托管直装渠道（install.eartrumpet.app，
//     经 winget-pkgs 清单摸到，见 TROUBLESHOOTING #11）双渠道发行；
//   - 无控制通道：上游源码无命令行参数解析、无 URI 协议与
//     AppInstance 重定向，第二实例遇命名 Mutex
//     Local\EarTrumpet-{GUID} 直接退出，无法充当"唤窗信使"；
//   - 生命周期冲突：它是注册 windows.startupTask 的常驻托盘应用，
//     与 Hanxi 托管模式的"空闲自动退出/随 Hanxi 退出/JobObject 强杀"
//     语义相反，激活经 AUMID 由系统进程拉起，也拿不到可托管的直接子进程；
//   - 数据模型：loose（Chocolatey）版配置写 HKCU\Software\EarTrumpet
//     而非 exe 同目录，非真便携，多版本共享同一注册表键会互相踩配置。
//
// 本模块对齐 nanazip 的包管理先例，只纳管直装渠道（用户拍板"商店版
// 不要了"）：解析上游官方 appinstaller 清单（钉死包名/发布者/主机 +
// winget 清单 SHA-256 交叉比对 + Windows 安装期 ACS 签名校验）后
// Add-AppxPackage。两渠道并存会争抢单实例互斥，商店版仅保留注册检测
// 用于并存警告，不提供任何商店操作。不绑定 JobObject，关闭 Hanxi 不影响
// EarTrumpet；运行态经进程枚举（安装目录前缀匹配）探测，"退出"因上游
// 无优雅通道采用 KillVerified 指纹复核后的直接终止（设置落盘为事务性
// 写入，强杀安全；仅影响当前会话，登录自启不受影响）。不提供隐藏托盘
// 等控制。
package eartrumpet

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "eartrumpet"

// Module 实现 extapi.Module，纯无状态入口，无重资源需要懒初始化。
type Module struct {
	svc *EarTrumpetService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewEarTrumpetService(plat)}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "EarTrumpet",
		Version:     "0.1.0",
		Description: "轻量音量控制托盘工具：官方直装渠道的检测、启动、退出、卸载与安装更新（并存商店版仅警告）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{ID: "eartrumpet-manager", Title: "EarTrumpet", Route: "/ext/eartrumpet", Icon: "i:volume-2", Section: extapi.SectionExt, Order: 77, Group: extapi.GroupDesktop}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

func (m *Module) OnInit(context.Context) error { return nil }

// OnDestroy 本模块无常驻资源（查询/安装均为按需短任务，goroutine 随调用结束收敛），无需回收。
func (m *Module) OnDestroy() error { return nil }

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool { return true }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 EarTrumpet", Run: func(context.Context) error {
			err := m.svc.Launch()
			return err
		}},
	}
}
