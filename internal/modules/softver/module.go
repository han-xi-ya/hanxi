package softver

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "softver"

// navRoute 本页导航路由（与 Nav 注册字面量一致；composition contract 以
// 源码字面量提取路由，故 notify 跳转路径单列此常量）。
const navRoute = "/ext/softver"

// Module 软件版本检测（BACKLOG F5）：微信双口径版本 × 官方最新版 × 目录空间勘察。
type Module struct {
	svc *SoftverService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewSoftverService(plat, extapi.NewLeaseHolder(ID))}
}

// SetGate 实现 extapi.GateAware：装配根注册时注入统一调用门，
// service 业务方法经该门取 operation lease（Wave 3 调用门）。
func (e *Module) SetGate(g extapi.Gate) { e.svc.holder.SetGate(g) }

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "软件版本",
		Version:     "0.1.0",
		Description: "日常装机软件版本跟踪（微信首个目标）：本机版本注册表×PE 双口径、官方最新版对照与安装包直连下载（不托管安装）、安装/数据目录两代探测与占用扫描",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{{
		ID:      "softver-main",
		Title:   "软件版本",
		Route:   "/ext/softver", // composition contract 按字面量提取，勿改用常量
		Icon:    "i:tag",
		Section: extapi.SectionExt,
		Order:   67,
		Group:   extapi.GroupSystem,
	}}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{application.NewService(e.svc)}
}

func (e *Module) OnInit(ctx context.Context) error { return nil } // 探测/扫描均按需触发，无常驻资源

func (e *Module) OnDestroy() error {
	e.svc.cancelAllDirScans()
	e.svc.cancelActiveDownload()
	return nil
}

func (e *Module) IsInitialized() bool { return true }
