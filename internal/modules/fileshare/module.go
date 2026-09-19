// Package fileshare 内置模块：局域网 HTTP 文件/文本快传站。
// 后端在 ShareConfig 指定端口起 http.Server（server.go），前端页面为嵌入的静态资产（fileshare/web），
// 手机电脑同网段扫码互传，零客户端依赖。OnDestroy 必须停服释放监听端口。
package fileshare

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "fileshare"

// Module 局域网快传模块
type Module struct {
	svc *FileShareService
}

// New 实例化模块
func New(plat platform.Platform) extapi.Module {
	return &Module{
		svc: NewFileShareService(plat, extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service RPC 导出版经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Service 获取底层业务服务实例
func (m *Module) Service() *FileShareService {
	return m.svc
}

// Info 返回模块元信息。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "局域网文件快传",
		Version:     "0.1.0",
		Description: "零客户端依赖的局域网极速文件/文本分享站，手机电脑扫码即用",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定网络组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "局域网快传",
		Icon:    "i:share-2",
		Route:   "/ext/fileshare",
		Section: extapi.SectionExt,
		Order:   35,
		Group:   extapi.GroupNetwork,
	}}
}

// 以下方法实现 extapi.Module 契约，逐项语义见接口文档。
// PermNetwork 覆盖局域网监听与广播；HTTP 服务本体由页面手动启停，
// OnDestroy 兜底 StopServer，防止模块停用时端口仍被 Hanxi 占用。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) OnInit(ctx context.Context) error {
	return nil
}

func (m *Module) OnDestroy() error {
	// 停用兜底停服走内部无门版：此时模块已 stopping，经门的 StopServer 会被拒导致端口不释放。
	return m.svc.stopServer()
}

// IsInitialized 本模块 OnInit 无副作用，懒初始化后恒为已就绪。
func (m *Module) IsInitialized() bool {
	return true
}
