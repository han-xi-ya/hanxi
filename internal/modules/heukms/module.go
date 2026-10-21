// Package heukms 内置模块：HEU KMS Activator 托管（版本管理 + RAR5 下载解包
// + 本地导入兜底 + JobObject 启停 + 窗口唤起）。
//
// 集成范围决策（机主拍板"纯托管"）：不移植任何上游能力，Hanxi 只做版本管理、
// 落位收纳、启停治理与状态呈现——激活功能的全部操作在上游自己的界面里完成。
//
// 与其他托管模块的两点结构性差异（均为上游形态所致，取证见包内 version/instance
// 注释与 docs/TROUBLESHOOTING.md #108）：
//  1. **解包走 RAR5 而不是 zip**：上游当前发 iso+rar，而 iso 的真实内容在 UDF 侧
//     （go-diskfs 无 udf 实现、系统 bsdtar 同样读不到），故 Hanxi 解 rar，经
//     `github.com/nwaples/rardecode`（纯 Go、BSD-2-Clause、零传递依赖；实测解出
//     产物 SHA256 与上游在包内更新日志逐版公布值一致）。可安装集比 zip 家族窄：
//     只有"有 rar 资产且 GitHub 已挂官方 digest"的版本进远程表——内核
//     `artifact.Fetch` 强制摘要校验，无摘要根本装不了，列进来只会造出点不动的行。
//     「导入本地」仍是兜底通道（挂载 iso 手取 exe、或收装机主已有的主程序）。
//  2. **manifest 强制管理员**（PE 资源 requireAdministrator 实证）：未提权的
//     Hanxi 拉起它直得 errno 740，走 elevateHint + 前端 ElevateRestart 一键通道
//     （bcu/litemonitor 同族三重契约）。
//
// 收录风险提示（如实呈现，不粉饰）：上游后期闭源、主程序无代码签名，Windows
// Defender 一类实时保护大概率拦截删除该文件——前端提示条与异常退出文案都写明。
package heukms

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "heukms"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *HeuKmsService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewHeuKmsService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "HEU KMS Activator",
		Version:     "0.1.0",
		Description: "托管 HEU KMS 激活工具：从 GitHub Releases 下载 RAR5（官方摘要校验 + 纯 Go 解包）、JobObject 启停与窗口唤起，另有本地导入兜底（iso 镜像不解析；程序 manifest 强制管理员权限）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定组内排序；86 为 GroupSystem 空位）。
func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "heukms-manager", Title: "HEU_KMS激活工具", Route: "/ext/heukms", Icon: "app:heukms", Section: extapi.SectionExt, Order: 86, Group: extapi.GroupSystem},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/ccswitch 同策略）。
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

// **托盘候选负裁决（刻意不实现 extapi.TrayCommandsProvider）**
//
// HEU 是"用完即关"的一次性操作工具：打开→点几下→退出，不存在需要随时快速
// 触发的常驻态（与 rammap 同类判据，与 ccswitch/dbx 的"日常反复进入"相反）。
// 挂 launch 命令只会让托盘菜单多一项几乎不点的条目，还带来"从托盘拉起一个
// 要求管理员权限的窗口"这条与 740 指引相互缠绕的差体验路径。
// 本注释即 catalog 负裁决的源码证据源；如后续要接，务必复用页面「启动」按钮
// 同一个 OpenWindow() 入口（勿新造 RPC、勿在闭包里另起一套控制编排）。
//
// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较。本模块的"更新"动作是
// 打开发布页自行取主程序导入，不是 Hanxi 代下载——文案见 version.CheckUpdate。
func (e *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return e.svc.manager.CheckUpdate(ctx)
}
