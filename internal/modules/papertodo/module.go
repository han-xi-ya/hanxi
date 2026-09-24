// Package papertodo 内置模块：PaperTodo 桌面便签的便携托管
// （GitHub Releases 双变体下载 + 单目录覆盖安装 + JobObject 启停 + show/hide/exit 命令信使）。
// Wave 5 起进程治理委托 packages/go/supervisor、受控下载与事务中转委托
// packages/go/artifact（journal 记账同 markeron/rufus/ccswitch）。
//
// 集成决策记录（https://github.com/snownico0722/PaperTodo）：
//   - 许可证：PolyForm Noncommercial 1.0.0 + 个人职业使用附加条款——允许自然人免费
//     使用（含工作场景），但不得销售/商业再分发、普通公司不得统一部署。因此只做
//     "用户机器直接从上游下载官方原版"的托管，绝不内嵌进 Hanxi 分发包；
//     告知义务见 docs/THIRD_PARTY_NOTICES.md（recordly AGPL 同款处置思路）；
//   - 发行形态：绿色单文件 exe（self-contained 内嵌 .NET 10 / no-runtime 需系统运行时），
//     便签数据（data.json、note-assets.lmdb、plugins/）恒在 exe 同目录——
//     因此采用固定单目录覆盖升级（数据永不迁移），卸载只删程序保留数据；
//     此盘上契约与 artifact.Tree 的"版本目录隔离落位"互斥，落位原子性由模块自持
//     （staging 独占 + rename，纪律同内核），登记见 version.Manager 注释；
//   - 完整性：集成实证期上游资产无 GitHub digest（2026-09 已回刷）、未收录 winget、
//     body 无哈希，ccswitch 的 digest 硬过滤照搬会清空历史版本表——digest 在场走
//     内核 Fetch 硬校验，缺失走降级链（见 version/manager.go 包注释），
//     坑点沉淀于 docs/TROUBLESHOOTING.md；
//   - 单实例契约：WPF 自建协议（互斥体 + 命名管道转发命令行参数），
//     探测用 OpenMutex，唤窗/收拢/退出对应 show/hide/exit 命令信使（源码实证），
//     exit 信使经 supervisor.SetQuitHook 收编为内核 grace 优雅退出通道；
//   - 不做空闲自动退出：桌面便签是常驻环境型工具，与 ccswitch 的"无人用即释放内存"
//     语义相反；不碰 --mcp 便签内容通道（未来需求另立模块评估）。
package papertodo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "papertodo"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *PaperTodoService
}

// New 在 app 装配期创建模块（构造无 IO，重活延迟到 OnInit 与 service 方法）。
func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewPaperTodoService(plat, extapi.NewLeaseHolder(ID))}
}

// Info 返回模块元信息（Version 是实现版本，与被管工具版本无关）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "PaperTodo",
		Version:     "0.1.0",
		Description: "托管桌面便签 PaperTodo：双变体版本管理与 JobObject 启停，唤窗/收拢/退出走官方命令信使",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Nav 声明侧边栏入口（Order/Group 决定组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "papertodo-manager", Title: "PaperTodo 便签", Route: "/ext/papertodo", Icon: "app:papertodo", Section: extapi.SectionExt, Order: 79, Group: extapi.GroupEfficiency},
	}
}

// 以下方法实现 extapi.Module 契约，逐项语义见 internal/extapi 接口文档。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/ccswitch 同策略）
func (m *Module) OnInit(ctx context.Context) error {
	m.svc.activate()
	return nil
}

// OnDestroy 交回 service 做资源收尾；错误仅记录，注册表不因此阻断停用流程。
func (m *Module) OnDestroy() error {
	// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)
	m.svc.shutdown()
	return nil
}

// IsInitialized OnInit 无失败路径，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool { return true }

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 PaperTodo", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较，返回语义见
// version.Manager.CheckUpdate 注释。
func (e *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return e.svc.manager.CheckUpdate(ctx)
}
