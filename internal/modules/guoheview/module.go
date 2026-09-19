// Package guoheview 内置模块：果核看图（GuoheView，原 MagicView）托管
// （版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything/ccswitch/piclite 完全平等的模块——统一注册、统一启停。
//
// 方案决策记录：
//   - 纯托管不内嵌：上游是原生极速看图器（自研解码内核、分块加载、ICC 色彩管理、
//     RAW 全家桶），内嵌重做毫无性价比，浏览操作全部在 GuoheView 自有窗口完成；
//   - 官方发布接口路线（非 GitHub）：闭源 freeware，发布源 rj.lovestu.com 的
//     JSON 接口每次仅返回当前版本（stable/beta 通道、官方 MD5、便携 zip 资产）——
//     远程列表至多两条、无历史版本，回滚依赖「导入本地」；
//   - 多实例契约（真机实证 3.2.7）：上游无单实例互斥体，二次拉起即新开窗口，
//     关窗即退。"打开窗口"= 聚焦自有实例 / 唤回外部实例 / 另开独立窗口三分支，
//     无信使转发路径；托管实例进 JobObject，独立窗口与外部实例永不接管；
//   - 无空闲自动退出：看图器"进程活着=窗口开着=用户在看图"，空闲退出只会打断
//     浏览（与 piclite 关窗藏托盘的空闲语义本质不同）；
//   - 内置更新器只节流不越权：config.ini 无官方关闭自动更新开关（实测键表仅
//     [update] min_check_interval），不改写上游配置语义，版本管理入口引导回
//     Hanxi，Updater 子进程由 JobObject 继承兜底（详见 instance 包注释）；
//   - 共享内核委托（Wave 4，ADR-0002 §5）：解包/落位/卸载走 artifact
//     （UnpackZip+Tree），进程治理走 supervisor；官方仅 MD5 的"下载+校验"段
//     留模块 bespoke，安装事务经 ops.BeginTxn 全链记账（事务与解包器无关）。
package guoheview

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "guoheview"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *GuoheViewService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewGuoheViewService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "果核看图",
		Version:     "0.1.0",
		Description: "果核看图 GuoheView 极速 RAW 图片查看器：官方发布接口版本管理、JobObject 托管启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "guoheview-manager", Title: "果核看图", Route: "/ext/guoheview", Icon: "i:image", Section: extapi.SectionExt, Order: 85, Group: extapi.GroupDesktop},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/piclite 同策略）
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
// 多实例语义下该命令幂等性以"唤回已有窗口优先"实现（见 OpenWindow 编排）。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动果核看图", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
