package terminal

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
)

// ID 是模块注册键，同时用作租约的 moduleID。
const ID = "terminal"

// Module 是 extapi.Module 契约载体。
type Module struct {
	svc *TerminalService
}

// New 装配期构造。终端服务是纯内存态（v0 语义：会话与 hanxi 同生命周期，
// 不落账本——见 service.go 头注），不需要 paths。
func New() extapi.Module {
	return &Module{svc: NewService(extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Info 返回模块元数据。Description 只写竖切里真实存在的能力：三档入口、
// 断线重连、杀会话；"多窗同屏观流""重启续会话"等未实现的话一句不提。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "内嵌终端",
		Version:     "0.1.0",
		Description: "在 hanxi 里开真终端：Windows 伪控制台（ConPTY）直连，命令行/PowerShell/Claude Code 三档入口；输出流走 Wails GoStream 二进制通道直达 xterm 渲染（非画面转译），断线不杀进程、刷新即重连；会话与 hanxi 同生命周期，退出全清不留孤儿",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口。图标 i:terminal 已在 icons.ts 池内核实（#136 纪律：
// Nav 的 Icon 必须先对池再落码）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "内嵌终端",
		Route:   "/ext/terminal",
		Icon:    "i:terminal",
		Section: extapi.SectionExt,
		Order:   98,
		Group:   extapi.GroupDeveloper,
	}}
}

// Services 注册 RPC 服务面。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(m.svc)}
}

// OnInit 注册 GoStream 字节通道——本仓首例（beta.10 内置通道，选型理由见
// service.go 头注；未找到仓内先例可抄是事实，不是遗漏）。
// 模块懒激活：前端经 /ext/terminal 门禁链先 EnsureActive 再建流，注册时序
// 天然先于第一次 Stream()。重复激活时 HandleStream 同名替换是幂等语义
// （wails 文档原文），无副作用。
func (m *Module) OnInit(context.Context) error {
	if app := application.Get(); app != nil {
		app.HandleStream(StreamName, m.svc.HandleStream)
	}
	return nil
}

// OnDestroy 杀掉全部终端会话（应用退出/模块停用统一收口，不留孤儿 conpty
// 子进程树——conpty 的孙进程链与 claude 同构，收口在 conpty.Close 内）。
func (m *Module) OnDestroy() error {
	m.svc.killAll()
	return nil
}

// IsInitialized 无副作用初始化，恒为就绪。
func (m *Module) IsInitialized() bool { return true }
