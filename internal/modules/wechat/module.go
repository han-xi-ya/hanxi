package wechat

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/extapi"
	"hubkit/internal/settings"
)

const ID = "wechat"

type Module struct {
	svc *WechatService
}

func New(store *settings.Store) extapi.Module {
	return &Module{
		svc: NewWechatService(store),
	}
}

func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "微信 ClawBot",
		Version:     "0.1.0",
		Description: "微信 iLink 智能机器人网关，支持扫码登录、会话保持、文字与图片多模态加密推送",
		Author:      "HubKit",
		Level:       extapi.LevelBuiltin,
	}
}

func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "微信机器人",
		Route:   "/ext/wechat",
		Icon:    "💬",
		Section: extapi.SectionExt,
		Order:   35,
	}}
}

func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) Permissions() []extapi.Permission {
	return []extapi.Permission{extapi.PermNetwork}
}

func (m *Module) Protocol() int { return 1 }
