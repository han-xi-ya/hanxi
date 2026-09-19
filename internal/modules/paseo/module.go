// Package paseo 内置模块：Paseo coding agent 编排器托管（版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/recordly/vscode 完全平等的模块——统一注册、统一启停。
//
// 方案要点（集成决策记录）：
//   - 纯托管不移植上游代码（Apache-2.0 允许再分发，但合规红线不变：仓库与
//     安装包不捆绑任何第三方二进制，全程用户侧按需下载官方 zip）；
//   - 形态取官方 win zip 便携（electron-builder win.target=[nsis,zip] 双发布，
//     v0.7.0 起每版稳定）而非 NSIS 安装器：避开 HKCU 卸载注册表卫兵与
//     "切换版本=重装"，多版本目录 versions/paseo_X.Y.Z 并存、删除=RemoveAll 自家目录；
//   - 数据模式为**用户目录共享**（拍板方案，与 cc-switch 同构）：Paseo 无
//     data\ 便携激活器，Electron 数据恒在 %APPDATA%\Paseo、daemon 数据恒在
//     ~/.paseo——托管实例与用户自装实例同数据、同单实例锁组（锁按 userData
//     派生），全局至多一个桌面主实例：自有与外部天然互斥，冷启动竞速由
//     wait() 外部接管分类兜底；刻意不注入官方 env 隔离通道
//     （PASEO_HOME/PASEO_ELECTRON_USER_DATA_DIR），零自定义行为、与上游推荐用法一致；
//   - 无自动更新禁用开关可注入（上游无 RECORDLY_DISABLE_AUTO_UPDATES 类 env，
//     源码实证）：electron-updater autoInstallOnAppQuit=false，安装动作只在
//     用户于 Paseo 界面内点击时发生，zip 形态下会装出 %LOCALAPPDATA% 平行副本
//     ——前端提示条如实预告"升级请走 Hanxi 版本管理"，不做静默兜底；
//   - 唤窗不走信使优先（与 recordly 分家）：上游 second-instance 语义是
//     openAdditional 新开窗口而非聚焦（main.ts 实证），优先 Win32 直唤已有
//     可见窗口（litemonitor 家族），无窗时才二次拉起请求开新窗；
//   - 禁用空闲自动退出：Paseo 是 daemon 宿主，无窗运行≠空闲，其上 agent
//     会话可能正在进行（rustdesk 先例：服务型常驻）。
package paseo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "paseo"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *PaseoService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewPaseoService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Paseo",
		Version:     "0.1.0",
		Description: "托管开源 coding agent 编排器 Paseo：双通道版本管理、官方便携 zip 多版本托管、JobObject 启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "paseo-manager", Title: "Paseo 编排器", Route: "/ext/paseo", Icon: "i:paw", Section: extapi.SectionExt, Order: 92, Group: extapi.GroupDeveloper},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 recordly/vscode 同策略）
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
// 复用与模块页面"打开窗口"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "打开 Paseo", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
