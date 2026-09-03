// Package quicklook 内置模块：QuickLook 空格预览托管
// （版本管理 + JobObject 托管启停 + 状态探测 + 命名管道优雅退出/重载）。
// 与 keyviz/snipaste/markeron/everything/ccswitch/piclite 完全平等的模块——统一注册、统一启停。
//
// 方案决策记录（侦查阶段上游源码实证，详见 instance 包注释）：
//   - 纯托管不内嵌：QuickLook 的预览窗口、插件查看器体系庞大且依赖其自有 Manager 进程，
//     内嵌重做无意义；控制台只承担版本管理与进程生命周期；
//   - 便携 zip 免安装路线：上游正式版并列发 .7z/.appx/.exe/.msi/.zip，唯有 .zip 是
//     免安装便携包（根含 QuickLook.exe + portable.lock + 原生/插件，实测 v4.5.0），
//     Go 标准库 archive/zip 原生解压；安装器类资产写系统，排除；
//   - 无"打开窗口"按钮：上游为托盘应用，命名管道消息（Toggle/Invoke/Reload/...）全为
//     预览视图相关，无 show-settings 契约；设置唯一入口是托盘左键——前端以提示条如实指引，
//     不造假按钮（同 keyviz 先例）；
//   - 优雅退出优先 + 强杀兜底：向命名管道 "QuickLook.App.Pipe.<SID>" 投递 "Quit" 令上游
//     OnExit 正常收尾，宽限内未退则 JobObject 强杀兜底。抓空格是进程内低级键盘钩子
//     （WH_KEYBOARD_LL，非注入），随进程终止由系统自动摘除，强杀同样零残渣；
//   - 生命周期可选：followOnExit 开关（默认随 Hanxi 退出；关闭则 Detached 独立常驻，
//     贴合 QuickLook "开机常驻" 本性），同 keyviz；
//   - 许可合规：上游 GPL-3.0，托管模式仅启动上游官方二进制、不链接不分发其代码，无传染性；
//     QuickLook 配置在便携目录随 exe，不属 Hanxi 读写范围。
package quicklook

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "quicklook"

type Module struct {
	svc *QuickLookService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewQuickLookService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "QuickLook",
		Version:     "0.1.0",
		Description: "开源空格秒预览工具 QuickLook：便携 zip 托管安装、JobObject 启停、命名管道优雅退出与运行状态探测",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "quicklook-manager", Title: "QuickLook 预览", Route: "/ext/quicklook", Icon: "👁️", Section: extapi.SectionExt, Order: 83},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 keyviz/markeron 同策略）
func (e *Module) OnInit(ctx context.Context) error {
	e.svc.activate()
	return nil
}

func (e *Module) OnDestroy() error {
	e.svc.Shutdown()
	return nil
}

func (e *Module) IsInitialized() bool {
	return true
}
