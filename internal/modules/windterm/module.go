// Package windterm 集成 WindTerm 官方 Windows x64 便携版（托管模式）。
//
// 集成决策留档（integrate-github-tool skill 要求）：纯托管、无内嵌重做——
// SSH/Serial/Shell 会话界面在上游自有窗口完成，内嵌重做无收益；上游为
// "部分开源"工具（README 自述完全免费商用/非商用，开源部分除 thirdparty
// 外 Apache-2.0，仓库根无 LICENSE 文件，API license 字段 null）：Hanxi 仅
// 转链官方 GitHub releases 分发，不搬运不打包二进制（许可备忘留档）。
//
// 关键领域事实（2026-09-23 阶段 0 侦查实证）：多实例 Qt 应用（二进制无
// 单实例机制痕迹、无 CLI 信使）；releases 全量无官方摘要（完整性走 vscode
// 降级三层先例，UI 如实标注）；主程序 asInvoker（无 740 提权契约）；
// 外部实例退出按 N3 终裁 confirm-force 档（会话类"中断有实际损失"）。
package windterm

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "windterm"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *WindTermService
}

// New 在 app 装配期创建模块（构造无 IO；本模块 OnInit 无重活，懒初始化仅为统一生命周期）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewWindTermService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "WindTerm",
		Version:     "0.1.0",
		Description: "托管 WindTerm SSH/Sftp/Shell/Telnet/Serial 终端：版本管理、JobObject 启停与外部实例分档治理",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（开发者组排序接 paseo 之后）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "windterm-manager", Title: "WindTerm 终端", Route: "/ext/windterm", Icon: "app:windterm", Section: extapi.SectionExt, Order: 94, Group: extapi.GroupDeveloper},
	}
}

// Services 暴露 RPC 面。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

func (m *Module) OnInit(context.Context) error { return nil }

// OnDestroy 应用退出/模块停用收口：仅在"随 Hanxi 关闭"联动开启时终止自有
// 实例；外部实例永不受影响（N3 分档治理归 Quit 显式入口）。
func (m *Module) OnDestroy() error {
	m.svc.shutdown()
	return nil
}

// IsInitialized 本模块 OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool { return true }

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较，返回语义见
// version.Manager.CheckUpdate 注释。
func (m *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return m.svc.manager.CheckUpdate(ctx)
}
