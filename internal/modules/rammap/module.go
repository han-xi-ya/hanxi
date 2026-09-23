// Package rammap 托管微软 Sysinternals RAMMap（内存观察工具）——N4 排期项，
// 机主 2026-09-23 拍板走"托管本体"路线（方案见 PLAN_N4_RAMMAP）。
//
// 上游事实（侦查实证，勿按 GitHub 绿色包家族想当然）：
//   - 分发为"同址覆盖式最新版"（无版本目录、无历史资产）→ 版本模型取
//     zip 的 Last-Modified 日期为版本令牌（唯一诚实语义，见 version 包）；
//   - 官方不为单工具发布摘要 → 完整性走 WindTerm 同款降级三层
//     （字节数 + CRC 闸门 + 布局自检），UI 如实标注"未经官方哈希校验"；
//   - 载荷 manifest requireAdministrator → 提权三重契约（#17：740 指引文案
//     含"管理员"关键词喂 ElevateRestart 一键通道、UIPI 边界如实降级、
//     stopped 引导行预告），与 rufus/litemonitor/bcu 同族；
//   - 许可：Sysinternals 工具按免费试用件分发、无再分发条款——hanxi 代下载
//     自用边界内，二进制不入库、不打包进发布物（与 GitHub 家族同口径）。
package rammap

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "rammap"

// Module extapi.Module 契约载体。
type Module struct {
	svc *RAMMapService
}

// New 在 app 装配期创建模块（构造无 IO）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewRAMMapService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "RAMMap",
		Version:     "0.1.0",
		Description: "托管微软 Sysinternals RAMMap 内存观察工具：官方直链版本管理与 JobObject 启停（需管理员运行）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 注入统一调用门。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Nav 侧边栏入口（系统组，排序接 litemonitor 之后）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "rammap-manager", Title: "RAMMap 内存", Route: "/ext/rammap", Icon: "i:gauge", Section: extapi.SectionExt, Order: 85, Group: extapi.GroupSystem},
	}
}

// Services RPC 面。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

func (m *Module) OnInit(context.Context) error { return nil }

// OnDestroy 应用退出/模块停用收口：仅联动开启时终止自有实例。
func (m *Module) OnDestroy() error {
	m.svc.shutdown()
	return nil
}

// IsInitialized 无失败路径。
func (m *Module) IsInitialized() bool { return true }

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：比较"上游当前日期版 vs
// 本机已装日期版"（语义与限制见 version.Manager.CheckUpdate 注释）。
func (m *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return m.svc.manager.CheckUpdate(ctx)
}
