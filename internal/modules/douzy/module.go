// Package douzy 内置模块：「抖音下载器」(Douzy) 的版本管理与安装包下载。
//
// 与其它托管模块（ccswitch/rustdesk/everything）的根本区别：**本模块不做进程托管**。
// 只做到"从上游 GitHub Releases 拉版本列表 → 下载 Windows 安装包（Douzy-Setup-*.exe，
// 官方 sha256 + 字节数 + PE 魔数三重校验）→ 交还用户，并可一键拉起上游 NSIS 安装向导"。
//
// 为何止步于此：上游桌面版 Douzy 尚处内测期、Electron 壳源码未公开、Windows 仅有
// NSIS 安装版无便携 zip——三者在"托管启停/探测/唤窗"上都是硬伤。停在"版本+下载"
// 边界既能满足追更下载诉求，又不背内测黑盒产品的运行期风险。详见 docs 集成决策记录。
package douzy

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "douzy"

type Module struct {
	svc *DouzyService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewDouzyService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "抖音下载器",
		Version:     "0.1.0",
		Description: "抖音桌面下载器 Douzy 的内测安装包版本管理：远程列表、官方哈希校验下载与安装向导拉起（仅版本管理+下载，不接管进程运行）",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "douzy-manager", Title: "抖音下载器", Route: "/ext/douzy", Icon: "i:film", Section: extapi.SectionExt, Order: 93, Group: extapi.GroupMedia},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// 无后台协程、无进程托管：OnInit/OnDestroy 空实现（仿 portscan）。
func (e *Module) OnInit(ctx context.Context) error { return nil }

func (e *Module) OnDestroy() error { return nil }

func (e *Module) IsInitialized() bool { return true }
