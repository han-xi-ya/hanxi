// Package omp 内置模块：「Oh My Pi (omp)」托管版本管理。
//
// omp 是 Stencil Labs 的终端 TUI 编码代理（pi-mono 血统，Bun 自包含单文件
// exe + 约 8 万行 Rust 原生核）。本模块做到"从官方 GitHub Releases 拉版本
// 列表 → 下载 Windows 单文件 exe（官方 sha256 + 字节上限 + MZ 魔数三层校验，
// 事务化落位 versions/omp_<ver>/）→ 导入本地已有 exe → 激活切换"。
//
// **本模块不做进程托管，负裁决两条**（domain 事实记录在 version 包注释）：
//  1. 不启停/不探测/不唤窗——omp 是纯 TUI，必须附着控制台终端才能交互，
//     JobObject 静默拉起只会得到一个秒退的无终端进程；运行入口是用户
//     PATH 里的 omp、hanxi 嵌入终端或 Paseo 驱动，本模块只保证"版本树里
//     有校验过的最新 exe"。
//  2. 不挂托盘/轮盘候选（TrayCommands 结构性缺席）——无主窗口可唤，
//     "启动"语义不成立；这是刻意裁决，勿当遗漏补一遍。
//
// 凭据边界：omp 的登录凭据/会话/记忆全部在 ~/.omp（数据不随版本树走，
// 也不经 Hanxi），本模块不读写该目录内容，仅在状态里展示其路径。
//
// 上游纪律实测（2026-11-04）：近 30 个 release 全带 omp-windows-x64.exe 且
// GitHub digest 官方 sha256 全量可得，tag 恒为 vX.Y.Z——完整性第一层不缺位。
package omp

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "omp"

type Module struct {
	svc *OmpService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewOmpService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 全部业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "Oh My Pi 代理",
		Version:     "0.1.0",
		Description: "终端 TUI 编码代理 Oh My Pi (omp) 的托管版本树：官方 Releases 列表、sha256 校验下载、本地导入与激活切换（仅版本管理，不接管进程运行，负裁决详见包注释）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "omp-manager", Title: "Oh My Pi 代理", Route: "/ext/omp", Icon: "app:omp", Section: extapi.SectionExt, Order: 99, Group: extapi.GroupDeveloper},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

// OnInit/OnDestroy：本模块无后台协程、无进程托管（包注释负裁决），
// 空实现即可（仿 douzy/portscan），保留装配根生命周期契约的显式表态。
func (e *Module) OnInit(ctx context.Context) error { return nil }

func (e *Module) OnDestroy() error { return nil }

func (e *Module) IsInitialized() bool { return true }

// CheckUpdate 实现 extapi.UpdateChecker 可选契约：宿主更新感知调度器直调
// （不经懒激活与统一调用门），转发版本引擎比较，返回语义见
// version.Manager.CheckUpdate 注释。
func (e *Module) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	return e.svc.manager.CheckUpdate(ctx)
}
