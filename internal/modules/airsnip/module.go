// Package airsnip 内置模块：Windows 轻量截图标注工具 AirSnip（轻截）托管
// （版本管理 + JobObject 启停 + 内部命令通道唤出截图/贴图/设置面板）。
// 与 frpc/markeron/everything/ccswitch/litemonitor 等完全平等的模块——
// 统一注册、统一启停。
//
// 方案要点（上游 bd-dxg/airsnip 实证，全档见 instance 包注释）：
//   - 从 GitHub Releases 下载 win-x64 便携 zip（官方 digest 四层校验，内核
//     artifact 链收口），解包进版本隔离目录直启单 exe（1.2 MB，无运行时无 WebView）；
//   - 上游托盘常驻、**无主窗口**，且没有 CLI、没有唤窗回调（它自己实现的
//     "第二实例唤起已有实例"消息在 wndproc 里无人处理）——本模块因此用上游
//     托盘菜单自己的命令 ID（PostMessage WM_COMMAND）驱动「截图 / 从剪贴板贴图 /
//     设置」，退出走上游托盘「退出」项逐字等价的 WM_CLOSE；
//   - 上游 manifest 为 requireAdministrator（覆盖管理员窗口所需，ADR 0003）：
//     未提权的 Hanxi 拉起直得 740，指引与一键提权重启走 house 通道；
//   - 上游内置自动更新会在托管目录里原地覆盖 exe，与 Hanxi 版本库打架：
//     每次托管启动前把 config.toml 的 auto_check_update 压成 false（单键、
//     就地改写，理由与边界见 instance/config.go）。
//
// 截图覆盖层、标注工具、长截图、贴图窗、改键等全部在上游本体完成
// （纯托管决策：热键驱动的全屏冻结形态不可内嵌，内嵌等于重做整个产品）。
//
// 与 snipaste 模块的关系：同为截图标注托管，上游定位不同（AirSnip 1 MB 纯 CPU
// 渲染、无 GPU 依赖，适合核显迷你机/虚拟机/远程桌面；Snipaste 功能面更全）。
// 两者**默认热键同为 F1**，同时运行会互相抢占——页面提示条已如实预告，
// 由用户二选一或在上游设置面板改键，本模块不自动改键（配置属用户意图）。
//
// 托盘候选裁决（2026-11-03 定案，非遗漏）：常驻可唤窗型 → 供 launch（复用页面
// 「打开设置」同一 OpenWindow 入口）；截图是本工具的高频一次性动作且有现成
// 命令通道 → 另供 shot（复用页面「截图」同一 Shot 入口）。刻意**不供** pin
// （"从剪贴板贴图"离开截图语境无独立价值，且剪贴板内容不明易被误按）与退出
// （托盘"退出"类命令在全家族无一供应，退出属页面级动作）。
package airsnip

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "airsnip"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *AirsnipService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewAirsnipService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "AirSnip 轻截",
		Version:     "0.1.0",
		Description: "托管 1 MB 原生截图标注工具 AirSnip：版本管理、JobObject 启停与截图/贴图/设置命令通道（全局热键 F1 截图、F3 贴图，含标注/长截图/贴图/另存；需以管理员运行）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序；Order 96 取当前最大 95 之后空位）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "airsnip-manager", Title: "AirSnip 轻截", Route: "/ext/airsnip", Icon: "app:airsnip", Section: extapi.SectionExt, Order: 96, Group: extapi.GroupDesktop},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/ccswitch/litemonitor 同策略）
func (m *Module) OnInit(ctx context.Context) error {
	m.svc.activate()
	return nil
}

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (m *Module) OnDestroy() error {
	m.svc.shutdown()
	return nil
}

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool { return true }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露
// 启动与截图两条命令，均复用与模块页面按钮完全一致的 service 入口；
// 宿主在触发前已完成模块懒初始化（裁决与不供项见包注释）。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 AirSnip", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
		{ID: "shot", Label: "AirSnip 截图", Run: func(context.Context) error {
			_, err := m.svc.Shot()
			return err
		}},
	}
}

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较，返回语义见
// version.Manager.CheckUpdate 注释。
func (m *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return m.svc.manager.CheckUpdate(ctx)
}
