package jsonfmt

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
)

// ID 是模块注册键，同时用作通知/事件的 moduleID。
const ID = "jsonfmt"

// Module 是 extapi.Module 契约载体：仅持有 service 单例。
type Module struct {
	svc *JsonFmtService
}

// New 在装配期创建模块。无平台依赖：清洗、解析、渲染全是纯计算，不碰注册表、
// 不碰网络、不落盘——所以构造签名比 publicip 还要空。
func New() extapi.Module {
	return &Module{
		svc: NewService(extapi.NewLeaseHolder(ID)),
	}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 的业务方法经该门取 operation lease（Wave 3 调用门）。
func (m *Module) SetGate(g extapi.Gate) { m.svc.holder.SetGate(g) }

// Info 返回模块元信息。Description 只写已实现的能力：
// 日志前缀剔除、嵌套转义展开、大整数保真是做过并测过的；不提"任意日志格式都能剥"
// 这种没证据的话（前缀规则是已知形态集合，见 clean.go 头注释）。
func (m *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "JSON 美化",
		Version:     "0.1.0",
		Description: "粘贴即格式化：从终端输出里抽取多段 JSON 逐段美化、剔除行首日志前缀、展开被字符串化的嵌套 JSON；数字与键序原样保留（大整数不改值）；附键路径频次统计、只读查找与按路径的显式替换；宽松写法与尾部截断自动修复并逐类声明",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

// Nav 声明侧边栏入口（Order/Group 决定开发者组内排序）。
func (m *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      ID,
		Title:   "JSON 美化",
		Route:   "/ext/jsonfmt",
		Icon:    "i:code",
		Section: extapi.SectionExt,
		Order:   96,
		Group:   extapi.GroupDeveloper,
	}}
}

// Services 注册 RPC 服务面。本模块纯计算、无常驻资源，生命周期无副作用。
func (m *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(m.svc),
	}
}

func (m *Module) OnInit(ctx context.Context) error { return nil }

func (m *Module) OnDestroy() error { return nil }

// IsInitialized 本模块 OnInit 无副作用，恒为已就绪。
func (m *Module) IsInitialized() bool { return true }
