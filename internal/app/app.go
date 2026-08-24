// Package app 是 Composition Root：装配平台、核心服务与扩展，并暴露为 wails3 应用。
package app

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hubkit/internal/extapi"
	"hubkit/internal/logging"
	"hubkit/internal/modules/fileshare"
	"hubkit/internal/modules/frpc"
	"hubkit/internal/modules/frpc/instance"
	"hubkit/internal/modules/frpc/version"
	"hubkit/internal/modules/lan"
	"hubkit/internal/modules/memo"
	"hubkit/internal/modules/portkill"
	"hubkit/internal/modules/portscan"
	"hubkit/internal/modules/publicip"
	"hubkit/internal/modules/wechat"
	"hubkit/internal/platform/windows"
	"hubkit/internal/settings"
)

const (
	Name    = "HubKit"
	Version = "0.1.0"
)

// mainWindow 主窗口引用，供单实例第二启动回调聚焦使用
var mainWindow *application.WebviewWindow

// RegisterEvents 注册类型化事件（wails3 绑定生成器会据此生成 TS API）。
func RegisterEvents() {
	application.RegisterEvent[extapi.NavEntry]("ext:changed")
	application.RegisterEvent[lan.LanProgress]("lan:progress")
	application.RegisterEvent[portscan.ScanProgress]("portscan:progress")
	application.RegisterEvent[wechat.InboundMessage]("wechat:message-received")
	application.RegisterEvent[map[string]string]("wechat:context-token-updated")
	application.RegisterEvent[version.DownloadProgress]("frpc:version-download")
	application.RegisterEvent[instance.Snapshot]("frpc:instance-state")
	application.RegisterEvent[instance.LogEntry]("frpc:instance-log")
	application.RegisterEvent[fileshare.ServerStatus]("fileshare:status")
	application.RegisterEvent[fileshare.TransferEvent]("fileshare:transfer")
	application.RegisterEvent[fileshare.DropItem]("fileshare:text-dropped")
	application.RegisterEvent[any]("memo:changed")
}

// New 装配应用：配置加载 + 扩展注册 + 服务注册 + 窗口创建。
// assets 由 cmd/hubkit 通过 embed 提供前端产物。
func New(assets application.AssetOptions) (*application.App, func()) {
	// 1. 初始化平台原语
	plat, err := windows.New()
	if err != nil {
		slog.Error("failed to init windows platform", "err", err)
	}

	// 2. 初始化路径与配置存储
	paths := settings.InitPaths()
	store, err := settings.NewStore(paths.ConfigFile())
	if err != nil {
		slog.Error("failed to load settings store", "err", err)
	}

	// 3. 初始化日志脱敏系统
	cfg := settings.DefaultSettings()
	if store != nil {
		cfg = store.Get()
	}
	_, logCleanup, _ := logging.InitLogger(paths.LogsDir(), cfg.LogRetainDays)

	slog.Info("HubKit starting",
		"mode", paths.Mode(),
		"baseDir", paths.BaseDir(),
		"version", Version,
	)

	// 4. 初始化模块注册表并注入持久化 Store
	registry := extapi.NewRegistry(store)

	// 5. 初始化 fileshare 与 memo 模块并建立数据互联
	fileShareModule := fileshare.New(plat)
	memoModule, err := memo.New(paths)
	if err != nil {
		slog.Error("failed to init memo module", "err", err)
	}

	// 建立从 fileshare 自动将投递文本写入 memo 的联动管道
	if mMod, ok := memoModule.(*memo.Module); ok && mMod != nil {
		if fsMod, ok := fileShareModule.(*fileshare.Module); ok && fsMod != nil {
			fsMod.Service().SetMemoHook(mMod.GetService().QuickCreate)
		}
	}

	// 6. 工具箱模块统一注册：frpc 与其余工具完全平等，均可启停
	modulesToRegister := []extapi.Module{
		frpc.New(plat),
		lan.New(plat, store),
		portkill.New(plat),
		portscan.New(),
		publicip.New(plat),
		wechat.New(store),
		fileShareModule,
	}
	if memoModule != nil {
		modulesToRegister = append(modulesToRegister, memoModule)
	}

	if err := registry.Register(modulesToRegister...); err != nil {
		panic(err) // 内建模块注册失败属于编程错误，直接暴露
	}

	services := []application.Service{
		application.NewService(NewAppService(registry, store)),
	}
	services = append(services, registry.EnabledServices()...)

	a := application.New(application.Options{
		Name:        Name,
		Description: "frpc 内网穿透开发客户端：多实例联调、局域网扫描、释放端口",
		Services:    services,
		Assets:      assets,
		// 单实例锁: 重复启动时 Wails 以 ExitCode 静默退出第二实例,
		// 避免旧实例在后台长期驻留内存导致任务管理器出现多个同名进程
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.hubkit.desktop",
			// 第二实例启动时, 聚焦展示已有主窗口
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if mainWindow != nil {
					mainWindow.Show()
					mainWindow.Focus()
				}
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// 将 Wails App 引用注入给需要的服务，以便向前端推送事件
	if fsMod, ok := fileShareModule.(*fileshare.Module); ok && fsMod != nil {
		fsMod.Service().SetWailsApp(a)
	}
	if mMod, ok := memoModule.(*memo.Module); ok && mMod != nil {
		mMod.GetService().SetWailsApp(a)
	}

	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            Name,
		Width:            1200,
		Height:           780,
		BackgroundColour: application.NewRGB(245, 246, 248),
		URL:              "/",
	})
	mainWindow = win

	// 注册窗口关闭拦截钩子：如果开启了“关闭时最小化到托盘”，则隐藏窗口代替退出
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		minimizeToTray := true
		if store != nil {
			minimizeToTray = store.Get().MinimizeToTray
		}
		if minimizeToTray {
			e.Cancel()
			win.Hide()
		}
	})

	// 创建系统托盘 (Systray)
	tray := a.SystemTray.New()
	tray.SetTooltip(Name + " - 内网穿透与网络工具箱")
	trayMenu := a.NewMenu()
	trayMenu.Add("显示 " + Name).OnClick(func(ctx *application.Context) {
		win.Show()
		win.Focus()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
		a.Quit()
	})
	tray.SetMenu(trayMenu)
	tray.OnDoubleClick(func() {
		if win.IsVisible() {
			win.Hide()
		} else {
			win.Show()
			win.Focus()
		}
	})

	cleanup := func() {
		if logCleanup != nil {
			logCleanup()
		}
	}

	return a, cleanup
}
