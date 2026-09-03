// Package app 是 Composition Root：装配平台、核心服务与扩展，并暴露为 wails3 应用。
package app

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hanxi/internal/extapi"
	"hanxi/internal/logging"
	"hanxi/internal/modules/bcu"
	bcuinstance "hanxi/internal/modules/bcu/instance"
	bcuversion "hanxi/internal/modules/bcu/version"
	"hanxi/internal/modules/ccswitch"
	ccswitchinstance "hanxi/internal/modules/ccswitch/instance"
	ccswitchversion "hanxi/internal/modules/ccswitch/version"
	"hanxi/internal/modules/eartrumpet"
	"hanxi/internal/modules/envcheck"
	"hanxi/internal/modules/everything"
	everythinginstance "hanxi/internal/modules/everything/instance"
	"hanxi/internal/modules/fileshare"
	"hanxi/internal/modules/flclash"
	flclashinstance "hanxi/internal/modules/flclash/instance"
	flclashversion "hanxi/internal/modules/flclash/version"
	"hanxi/internal/modules/frpc"
	"hanxi/internal/modules/frpc/instance"
	"hanxi/internal/modules/frpc/version"
	"hanxi/internal/modules/lan"
	"hanxi/internal/modules/mangodisk"
	mangodiskinstance "hanxi/internal/modules/mangodisk/instance"
	mangodiskversion "hanxi/internal/modules/mangodisk/version"
	"hanxi/internal/modules/markeron"
	markeroninstance "hanxi/internal/modules/markeron/instance"
	markeronversion "hanxi/internal/modules/markeron/version"
	"hanxi/internal/modules/memo"
	"hanxi/internal/modules/nanazip"
	"hanxi/internal/modules/papertodo"
	papertodoinstance "hanxi/internal/modules/papertodo/instance"
	papertodoversion "hanxi/internal/modules/papertodo/version"
	"hanxi/internal/modules/portkill"
	"hanxi/internal/modules/portscan"
	"hanxi/internal/modules/publicip"
	"hanxi/internal/modules/recordly"
	recordlyinstance "hanxi/internal/modules/recordly/instance"
	recordlyversion "hanxi/internal/modules/recordly/version"
	"hanxi/internal/modules/snipaste"
	snipasteinstance "hanxi/internal/modules/snipaste/instance"
	snipasteversion "hanxi/internal/modules/snipaste/version"
	"hanxi/internal/modules/wechat"
	"hanxi/internal/modules/wifi"
	"hanxi/internal/notify"
	"hanxi/internal/platform/windows"
	"hanxi/internal/product"
	"hanxi/internal/settings"
)

// mainWindow 主窗口引用，供单实例第二启动回调聚焦使用
var mainWindow *application.WebviewWindow

// RegisterEvents 注册类型化事件（wails3 绑定生成器会据此生成 TS API）。
func RegisterEvents() {
	// ext:changed / memo:changed 是无载荷事件，必须用 Void 注册：
	// 若注册具体类型而 Emit 不带载荷，会因 Wails 严格类型校验被静默丢弃。
	application.RegisterEvent[application.Void]("ext:changed")
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
	application.RegisterEvent[application.Void]("memo:changed")
	application.RegisterEvent[notify.Notification]("notify:received")
	application.RegisterEvent[markeronversion.DownloadProgress]("markeron:version-download")
	application.RegisterEvent[markeroninstance.Snapshot]("markeron:instance-state")
	application.RegisterEvent[everything.DownloadTicket]("everything:download")
	application.RegisterEvent[everythinginstance.Snapshot]("everything:instance-state")
	application.RegisterEvent[ccswitchversion.DownloadProgress]("ccswitch:version-download")
	application.RegisterEvent[ccswitchinstance.Snapshot]("ccswitch:instance-state")
	application.RegisterEvent[snipasteversion.DownloadProgress]("snipaste:version-download")
	application.RegisterEvent[snipasteinstance.Snapshot]("snipaste:instance-state")
	application.RegisterEvent[mangodiskversion.DownloadProgress]("mangodisk:version-download")
	application.RegisterEvent[mangodiskinstance.Snapshot]("mangodisk:instance-state")
	application.RegisterEvent[bcuversion.DownloadProgress]("bcu:version-download")
	application.RegisterEvent[bcuinstance.Snapshot]("bcu:instance-state")
	application.RegisterEvent[recordlyversion.DownloadProgress]("recordly:version-download")
	application.RegisterEvent[recordlyinstance.Snapshot]("recordly:instance-state")
	application.RegisterEvent[papertodoversion.DownloadProgress]("papertodo:version-download")
	application.RegisterEvent[papertodoinstance.Snapshot]("papertodo:instance-state")
	application.RegisterEvent[flclashversion.DownloadProgress]("flclash:version-download")
	application.RegisterEvent[flclashinstance.Snapshot]("flclash:instance-state")
	application.RegisterEvent[nanazip.OperationProgress]("nanazip:operation-progress")
	application.RegisterEvent[nanazip.PackageSnapshot]("nanazip:package-snapshot")
}

// Options 控制应用启动时行为。
type Options struct {
	StartMinimized bool
}

// New 装配应用：配置加载 + 扩展注册 + 服务注册 + 窗口创建。
// assets 由 cmd/hanxi 通过 embed 提供前端产物。
func New(assets application.AssetOptions, options Options) (*application.App, func()) {
	// 1. 初始化平台原语
	plat, err := windows.New()
	if err != nil {
		slog.Error("failed to init windows platform", "err", err)
		panic(err)
	}

	// 2. 初始化路径与配置存储
	paths := settings.InitPaths()
	store, err := settings.NewStore(paths.ConfigFile())
	if err != nil {
		slog.Error("failed to load settings store", "err", err)
		panic(err)
	}

	// 3. 初始化日志脱敏系统
	cfg := settings.DefaultSettings()
	if store != nil {
		cfg = store.Get()
	}
	_, logCleanup, err := logging.InitLogger(paths.LogsDir(), cfg.LogRetainDays)
	if err != nil {
		slog.Warn("failed to init logger", "err", err)
	}

	slog.Info("Hanxi starting",
		"mode", paths.Mode(),
		"baseDir", paths.BaseDir(),
		"version", product.Version,
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
		markeron.New(plat),
		everything.New(plat),
		ccswitch.New(plat),
		snipaste.New(plat),
		nanazip.New(plat),
		eartrumpet.New(plat),
		mangodisk.New(plat),
		bcu.New(plat),
		flclash.New(plat),
		recordly.New(plat),
		papertodo.New(plat),
		lan.New(plat, store),
		portkill.New(plat),
		portscan.New(),
		publicip.New(plat),
		wifi.New(),
		envcheck.New(plat),
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
		application.NewService(notify.NewNotificationService()),
	}
	services = append(services, registry.EnabledServices()...)

	// 自动预初始化微信等常驻监听型后台模块，确保即便未打开对应前端页面也能实时监听入站消息
	_ = registry.EnsureActive("wechat")

	a := application.New(application.Options{
		Name:        product.Name,
		Description: product.Description,
		// 应用退出统一清理：所有已初始化模块先走 OnDestroy。JobObject 托管工具
		// 会连带退出；Snipaste 等明确脱管的桌面工具由模块契约保留原生托盘与快捷键。
		// OnShutdown 阻塞至返回，保证需要回收的工具不残留孤儿进程。
		OnShutdown: func() {
			registry.ShutdownAll()
		},
		Services: services,
		Assets:   assets,
		// 单实例锁: 重复启动时 Wails 以 ExitCode 静默退出第二实例,
		// 避免旧实例在后台长期驻留内存导致任务管理器出现多个同名进程
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: product.Identifier,
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
		Title:            product.Name,
		Width:            1200,
		Height:           780,
		BackgroundColour: application.NewRGB(245, 246, 248),
		URL:              "/",
		Hidden:           options.StartMinimized,
	})
	mainWindow = win
	notify.GetHub().SetWailsContext(a, win)

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
	tray.SetTooltip(product.Name + " - " + product.Tagline)
	trayMenu := a.NewMenu()
	trayMenu.Add("显示 " + product.Name).OnClick(func(ctx *application.Context) {
		win.Show()
		win.Focus()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
		a.Quit()
	})
	tray.SetMenu(trayMenu)
	// 单击托盘图标切换主窗口显隐：隐藏时显示并聚焦，可见时隐藏到托盘
	tray.OnClick(func() {
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
