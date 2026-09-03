// Package piclite 内置模块：PicLite 图轻（本地图片/GIF 压缩工具）托管
// （版本管理 + JobObject 托管启停 + 窗口唤起）。
// 与 frpc/markeron/everything/ccswitch 完全平等的模块——统一注册、统一启停。
//
// 方案决策记录：
//   - 纯托管不内嵌：PicLite 工作台/悬浮结果/剪贴板与文件夹监测/图床上传界面完整，
//     内嵌重做性价比低，且悬浮流依赖上游全局快捷键——所有图片操作在 PicLite 自有窗口完成；
//   - MSI 管理提取路线：上游 46 个版本均无便携 zip，仅 NSIS perMachine setup.exe
//     （需提权、写卸载注册表，否决）与 WiX MSI。msiexec /a 管理提取免管理员、
//     免注册表副作用拆出单 exe，已真机验证（详见 version.extractMSI 注释）；
//   - 退出即强杀：上游关窗语义是"隐藏驻托盘"且 ExitRequested 默认拦截，
//     不存在任何外部优雅退出通道（无 -quit CLI、无命令管道），Quit 直接 JobObject
//     终止；配置前端即时写盘，不丢设置（详见 instance 包注释）。
package piclite

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

const ID = "piclite"

type Module struct {
	svc *PicLiteService
}

func New(plat platform.Platform) extapi.Module {
	return &Module{svc: NewPicLiteService(plat)}
}

func (e *Module) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{
		ID:          ID,
		Name:        "PicLite",
		Version:     "0.1.0",
		Description: "本地优先图片/GIF 压缩工具 PicLite 图轻：MSI 托管安装、JobObject 启停与窗口唤起",
		Author:      "Hanxi",
		Level:       extapi.LevelBuiltin,
	}
}

func (e *Module) Nav() []extapi.NavEntry {
	return []extapi.NavEntry{
		{ID: "piclite-manager", Title: "PicLite 压图", Route: "/ext/piclite", Icon: "🖼️", Section: extapi.SectionExt, Order: 81},
	}
}

func (e *Module) Services() []extapi.Service {
	return []extapi.Service{
		application.NewService(e.svc),
	}
}

func (e *Module) Permissions() []extapi.Permission { return nil }

func (e *Module) Protocol() int { return 1 }

// OnInit 首次激活时启动外部实例感知轮询（懒加载，与 markeron/everything 同策略）
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

// TrayCommands 实现 extapi.TrayCommandsProvider 可选契约：向宿主托盘暴露启动命令，
// 复用与模块页面"启动"按钮完全一致的 service 入口；宿主在触发前已完成模块懒初始化。
func (m *Module) TrayCommands() []extapi.TrayCommand {
	return []extapi.TrayCommand{
		{ID: "launch", Label: "启动 PicLite", Run: func(context.Context) error {
			_, err := m.svc.OpenWindow()
			return err
		}},
	}
}
