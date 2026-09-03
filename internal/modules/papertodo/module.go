// Package papertodo 内置模块：PaperTodo 桌面便签的便携托管
// （GitHub Releases 双变体下载 + 单目录覆盖安装 + JobObject 启停 + show/hide/exit 命令信使）。
//
// 集成决策记录（https://github.com/snownico0722/PaperTodo）：
//   - 许可证：PolyForm Noncommercial 1.0.0 + 个人职业使用附加条款——允许自然人免费
//     使用（含工作场景），但不得销售/商业再分发、普通公司不得统一部署。因此只做
//     "用户机器直接从上游下载官方原版"的托管，绝不内嵌进 Hanxi 分发包；
//     告知义务见 docs/THIRD_PARTY_NOTICES.md（recordly AGPL 同款处置思路）；
//   - 发行形态：绿色单文件 exe（self-contained 内嵌 .NET 10 / no-runtime 需系统运行时），
//     便签数据（data.json、note-assets.lmdb、plugins/）恒在 exe 同目录——
//     因此采用固定单目录覆盖升级（数据永不迁移），卸载只删程序保留数据；
//   - 完整性：实证上游资产无 GitHub digest、未收录 winget、body 无哈希，
//     ccswitch 的 digest 硬过滤照搬会清空版本表；降级链见 version/manager.go 包注释，
//     坑点沉淀于 docs/TROUBLESHOOTING.md；
//   - 单实例契约：WPF 自建协议（互斥体 + 命名管道转发命令行参数），
//     探测用 OpenMutex，唤窗/收拢/退出对应 show/hide/exit 命令信使（源码实证）；
//   - 不做空闲自动退出：桌面便签是常驻环境型工具，与 ccswitch 的"无人用即释放内存"
//     语义相反；不碰 --mcp 便签内容通道（未来需求另立模块评估）。
package papertodo

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "papertodo"

type Module struct {
	svc *PaperTodoService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewPaperTodoService(plat)}
}

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

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "papertodo-manager", Title: "PaperTodo 便签", Route: "/ext/papertodo", Icon: "📄", Section: extapi.SectionExt, Order: 79},
	}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) Permissions() []extapi.Permission { return nil }

func (m *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/ccswitch 同策略）
func (m *Module) OnInit(ctx context.Context) error {
	m.svc.activate()
	return nil
}

func (m *Module) OnDestroy() error {
	m.svc.Shutdown()
	return nil
}

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
