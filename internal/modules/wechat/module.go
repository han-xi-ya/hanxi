// Package wechat 内置模块：微信 ClawBot（iLink 网关）多账号机器人。
// 纯 HTTP 实现（扫码登录、长轮询收消息、CDN 密文收发图片/文件），无第三方 SDK 依赖；
// 凭据由 settings.Store 持久化，账号级 Listener goroutine 由 service 统一管理生命周期。
package wechat

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// ID 是模块注册键；微信模块无 exe，直接以 HTTP 客户端对接 iLink 网关。
const ID = "wechat"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *WechatService
}

// New 在 app 装配期创建模块；多账号凭据由 settings.Store 持久化，构造无网络 IO。
func New(store *settings.Store) extapi.Module {
	return &Module{
		svc: NewWechatService(store),
	}
}

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "微信 ClawBot",
		Version:     "0.1.0",
		Description: "微信 iLink 智能机器人网关，支持扫码登录、会话保持、文字与图片多模态加密推送",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定效率组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "微信机器人",
		Route:   "/ext/wechat",
		Icon:    "i:message-circle",
		Section: extapi.SectionExt,
		Order:   35,
		Group:   extapi.GroupEfficiency,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// 声明 PermNetwork 权限（长轮询 + CDN 下载）；OnInit 恢复已登录账号的监听 goroutine，
// OnDestroy 必须经 Destroy 终止全部监听，防止注册表停用后轮询残留。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) OnInit(ctx context.Context) error {
	m.svc.InitOnDemand()
	return nil
}

func (m *Module) OnDestroy() error {
	m.svc.Destroy()
	return nil
}

// IsInitialized InitOnDemand 无失败路径，注册表懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool {
	return true
}
