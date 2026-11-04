// Package omp 内置模块：「Oh My Pi (omp)」官方安装通道。
//
// omp 是 Stencil Labs 的终端 TUI 编码代理（pi-mono 血统，Bun 自包含单文件
// exe + 约 8 万行 Rust 原生核）。本模块做到"一键代跑官方 install.ps1 完成
// 安装/更新（下载、自校验、落位 %LOCALAPPDATA%\omp、PATH 打理、Git Bash
// 接线全部由官方脚本自理），一键卸载（删官方目录+剔 PATH，~/.omp 数据根
// 保留）"——即机主要的那条命令脚本路线，装完新开终端直接敲 omp。
//
// 历史决策留痕：本模块曾自建版本树（Releases 列表/校验下载/导入/激活切换，
// artifact 内核事务落位），机主 2026-11-04 拍板"安装=跑官方脚本"后整体拆除
// ——两套入口并存会把人绕晕，官方件是唯一日常通道。omp 的版本追更感知由
// envcheck 的官方 Releases 对照卡负责（detect/omp.go + ompversion 包），
// 本模块不再重复实现 CheckUpdate（不是遗漏，是分工，勿当缺失补一遍）。
//
// **负裁决两条**：
//  1. 不接管进程运行——omp 是纯 TUI，必须附着控制台终端交互，JobObject
//     静默拉起只会得到秒退的无终端进程；运行入口是用户 PATH 里的 omp、
//     hanxi 嵌入终端或 Paseo 驱动。
//  2. 不挂托盘/轮盘候选（TrayCommands 结构性缺席）——无主窗口可唤，
//     "启动"语义不成立。
//
// 凭据边界：omp 的登录凭据/会话/记忆全部在 ~/.omp（官方脚本装件与裸跑都落
// 这里），本模块不读写该目录内容，仅在状态里展示其路径；卸载保留不删。
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
		Description: "终端 TUI 编码代理 Oh My Pi (omp) 的官方安装通道：一键装/更/卸（代跑官方 install.ps1，含 PATH 打理与卸载回收，~/.omp 数据保留）；不接管进程运行、不自建版本树（追更感知归 envcheck 对照卡），负裁决详见包注释",
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
