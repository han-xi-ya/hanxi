package aichat

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// ID 是模块注册键，同时用作事件/租约的 moduleID。
const ID = "aichat"

// Module 是 extapi.Module 契约载体。
type Module struct {
	svc *AiChatService
	// initErr 构造期失败（账本损坏/目录不可建）不 panic、不装死：Info 照常、
	// OnInit 如实上抛，让装配根与调用门的既有报错通道去呈现。
	initErr error
}

// New 装配期构造。需要 paths：会话账本（StateDir）与固定工作目录（DataDir，
// resume 依赖 cwd 派生归档，见 service.go 头注）。
// 构造失败（账本损坏/目录不可建）时 svc 置 nil、OnInit 如实上抛：不给一个
// 调用门形同虚设的半成品服务面挂到 RPC 上。
func New(paths *settings.Paths) extapi.Module {
	holder := extapi.NewLeaseHolder(ID)
	svc, err := NewService(holder, paths.StateDir(), paths.DataDir())
	if err != nil {
		return &Module{initErr: err}
	}
	return &Module{svc: svc}
}

// SetGate 注入统一调用门。
func (m *Module) SetGate(g extapi.Gate) {
	if m.svc != nil {
		m.svc.holder.SetGate(g)
	}
}

// Info 返回模块元信息。Description 只写实测过的能力档位：
// 常驻会话、块级流式、resume 复进都钉在探测原始帧上；不提逐 token 打字机
// 与秒级打断（那是未验证的控制帧领域）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "AI 聊天",
		Version:     "0.1.0",
		Description: "驱动本机 Claude Code 的聊天窗口：一个会话一个常驻进程、上下文连续，回答按块流式上屏（单轮实测 2.5~8 秒）；空闲自动回收、再发自动复进（跨进程记忆实测成立）；内容会离开本机，每个会话首条消息需确认；未装 CLI 的机器上如实报错，其余模块不受影响",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（开发者组，排在 JSON 美化之后）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "AI 聊天",
		Route:   "/ext/aichat",
		Icon:    "i:chat",
		Section: extapi.SectionExt,
		Order:   97,
		Group:   extapi.GroupDeveloper,
	}}
}

// Services 注册 RPC 服务面。
func (m *Module) Services() []extapi.Service {
	if m.svc == nil {
		return nil
	}
	return []extapi.Service{application.NewService(m.svc)}
}

func (m *Module) OnInit(context.Context) error { return m.initErr }

// OnDestroy 拆掉所有活进程（杀树走 taskkill，实测收全祖孙三代）。
func (m *Module) OnDestroy() error {
	if m.svc == nil {
		return nil
	}
	m.svc.mu.Lock()
	ids := make([]string, 0, len(m.svc.runtimes))
	for id := range m.svc.runtimes {
		ids = append(ids, id)
	}
	m.svc.mu.Unlock()
	for _, id := range ids {
		m.svc.teardown(id, "")
	}
	return nil
}

// IsInitialized 构造成功即就绪。
func (m *Module) IsInitialized() bool { return m.initErr == nil }
