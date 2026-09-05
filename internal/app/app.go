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
	"hanxi/internal/modules/bili23"
	bili23instance "hanxi/internal/modules/bili23/instance"
	bili23version "hanxi/internal/modules/bili23/version"
	"hanxi/internal/modules/ccswitch"
	ccswitchinstance "hanxi/internal/modules/ccswitch/instance"
	ccswitchversion "hanxi/internal/modules/ccswitch/version"
	"hanxi/internal/modules/ddnsgo"
	ddnsgoinstance "hanxi/internal/modules/ddnsgo/instance"
	ddnsgoversion "hanxi/internal/modules/ddnsgo/version"
	"hanxi/internal/modules/eartrumpet"
	"hanxi/internal/modules/envcheck"
	"hanxi/internal/modules/envcheck/npmtool"
	"hanxi/internal/modules/everything"
	everythinginstance "hanxi/internal/modules/everything/instance"
	"hanxi/internal/modules/fileshare"
	"hanxi/internal/modules/flclash"
	flclashinstance "hanxi/internal/modules/flclash/instance"
	flclashversion "hanxi/internal/modules/flclash/version"
	"hanxi/internal/modules/frpc"
	"hanxi/internal/modules/frpc/instance"
	"hanxi/internal/modules/frpc/version"
	"hanxi/internal/modules/guoheview"
	guoheviewinstance "hanxi/internal/modules/guoheview/instance"
	guoheviewversion "hanxi/internal/modules/guoheview/version"
	"hanxi/internal/modules/keyviz"
	keyvizinstance "hanxi/internal/modules/keyviz/instance"
	keyvizversion "hanxi/internal/modules/keyviz/version"
	"hanxi/internal/modules/lan"
	"hanxi/internal/modules/litemonitor"
	litemonitorinstance "hanxi/internal/modules/litemonitor/instance"
	litemonitorversion "hanxi/internal/modules/litemonitor/version"
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
	"hanxi/internal/modules/piclite"
	picliteinstance "hanxi/internal/modules/piclite/instance"
	picliteversion "hanxi/internal/modules/piclite/version"
	"hanxi/internal/modules/portkill"
	"hanxi/internal/modules/portscan"
	"hanxi/internal/modules/publicip"
	"hanxi/internal/modules/quicklook"
	quicklookinstance "hanxi/internal/modules/quicklook/instance"
	quicklookversion "hanxi/internal/modules/quicklook/version"
	"hanxi/internal/modules/recordly"
	recordlyinstance "hanxi/internal/modules/recordly/instance"
	recordlyversion "hanxi/internal/modules/recordly/version"
	"hanxi/internal/modules/rufus"
	rufusinstance "hanxi/internal/modules/rufus/instance"
	rufusversion "hanxi/internal/modules/rufus/version"
	"hanxi/internal/modules/rustdesk"
	rustdeskinstance "hanxi/internal/modules/rustdesk/instance"
	rustdeskversion "hanxi/internal/modules/rustdesk/version"
	"hanxi/internal/modules/snipaste"
	snipasteinstance "hanxi/internal/modules/snipaste/instance"
	snipasteversion "hanxi/internal/modules/snipaste/version"
	"hanxi/internal/modules/subnetdesk"
	subnetdeskinstance "hanxi/internal/modules/subnetdesk/instance"
	subnetdeskversion "hanxi/internal/modules/subnetdesk/version"
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
	application.RegisterEvent[string]("tray:navigate")
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
	application.RegisterEvent[picliteversion.DownloadProgress]("piclite:version-download")
	application.RegisterEvent[picliteinstance.Snapshot]("piclite:instance-state")
	application.RegisterEvent[keyvizversion.DownloadProgress]("keyviz:version-download")
	application.RegisterEvent[keyvizinstance.Snapshot]("keyviz:instance-state")
	application.RegisterEvent[quicklookversion.DownloadProgress]("quicklook:version-download")
	application.RegisterEvent[quicklookinstance.Snapshot]("quicklook:instance-state")
	application.RegisterEvent[litemonitorversion.DownloadProgress]("litemonitor:version-download")
	application.RegisterEvent[litemonitorinstance.Snapshot]("litemonitor:instance-state")
	application.RegisterEvent[guoheviewversion.DownloadProgress]("guoheview:version-download")
	application.RegisterEvent[guoheviewinstance.Snapshot]("guoheview:instance-state")
	application.RegisterEvent[ddnsgoversion.DownloadProgress]("ddnsgo:version-download")
	application.RegisterEvent[ddnsgoinstance.Snapshot]("ddnsgo:instance-state")
	application.RegisterEvent[ddnsgoinstance.LogEntry]("ddnsgo:instance-log")
	application.RegisterEvent[rufusversion.DownloadProgress]("rufus:version-download")
	application.RegisterEvent[rufusinstance.Snapshot]("rufus:instance-state")
	application.RegisterEvent[rustdeskversion.DownloadProgress]("rustdesk:version-download")
	application.RegisterEvent[rustdeskinstance.Snapshot]("rustdesk:instance-state")
	application.RegisterEvent[subnetdeskversion.DownloadProgress]("subnetdesk:version-download")
	application.RegisterEvent[subnetdeskinstance.Snapshot]("subnetdesk:instance-state")
	application.RegisterEvent[bili23version.DownloadProgress]("bili23:version-download")
	application.RegisterEvent[bili23instance.Snapshot]("bili23:instance-state")
	application.RegisterEvent[nanazip.OperationProgress]("nanazip:operation-progress")
	application.RegisterEvent[nanazip.PackageSnapshot]("nanazip:package-snapshot")
	application.RegisterEvent[npmtool.OperationProgress]("envcheck:npm-tool-operation")
	application.RegisterEvent[npmtool.OperationLog]("envcheck:npm-tool-log")
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
		piclite.New(plat),
		keyviz.New(plat),
		quicklook.New(plat),
		litemonitor.New(plat),
		guoheview.New(plat),
		ddnsgo.New(plat),
		rustdesk.New(plat),
		subnetdesk.New(plat),
		rufus.New(plat),
		bili23.New(plat),
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

	appSvc := NewAppService(registry, store)
	services := []application.Service{
		application.NewService(appSvc),
		application.NewService(notify.NewNotificationService()),
	}
	services = append(services, registry.AllServices()...)

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

	// 深色标题栏桥：前端 useTheme 解析出实际亮/暗后经 SetWindowDarkMode 调到这里，
	// 由平台层 DWM 属性同步原生窗框（重构蓝图铁律 8 的唯一后端例外）。
	appSvc.SetWindowDarkApplier(func(dark bool) error {
		return windows.SetImmersiveDarkMode(uintptr(win.NativeWindow()), dark)
	})

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

	// 创建系统托盘 (Systray)：右键菜单按配置动态装配，保存配置后热重建
	tray := a.SystemTray.New()
	tray.SetTooltip(product.Name + " - " + product.Tagline)
	trayMenuBuilder := newTrayMenuBuilder(a, win, tray, registry, store)
	trayMenuBuilder.Rebuild()
	appSvc.SetTrayRebuilder(trayMenuBuilder.Rebuild)
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
