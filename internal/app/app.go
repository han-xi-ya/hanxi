// Package app 是 Composition Root：装配平台、核心服务与扩展，并暴露为 wails3 应用。
package app

import (
	"fmt"
	"log/slog"
	"os"
	"time"

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
	"hanxi/internal/modules/douzy"
	douzyversion "hanxi/internal/modules/douzy/version"
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
	"hanxi/internal/modules/ocr"
	"hanxi/internal/modules/papertodo"
	papertodoinstance "hanxi/internal/modules/papertodo/instance"
	papertodoversion "hanxi/internal/modules/papertodo/version"
	"hanxi/internal/modules/paseo"
	paseoinstance "hanxi/internal/modules/paseo/instance"
	paseoversion "hanxi/internal/modules/paseo/version"
	"hanxi/internal/modules/piclite"
	picliteinstance "hanxi/internal/modules/piclite/instance"
	picliteversion "hanxi/internal/modules/piclite/version"
	"hanxi/internal/modules/portkill"
	"hanxi/internal/modules/portscan"
	"hanxi/internal/modules/publicip"
	"hanxi/internal/modules/quicklook"
	quicklookinstance "hanxi/internal/modules/quicklook/instance"
	quicklookversion "hanxi/internal/modules/quicklook/version"
	"hanxi/internal/modules/quickmenu"
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
	"hanxi/internal/modules/translucenttb"
	ttbinstance "hanxi/internal/modules/translucenttb/instance"
	ttbversion "hanxi/internal/modules/translucenttb/version"
	"hanxi/internal/modules/vscode"
	vscodeinstance "hanxi/internal/modules/vscode/instance"
	vscodeversion "hanxi/internal/modules/vscode/version"
	"hanxi/internal/modules/webapp"
	"hanxi/internal/modules/wechat"
	"hanxi/internal/modules/wifi"
	"hanxi/internal/modules/wsl"
	"hanxi/internal/notify"
	"hanxi/internal/platform/windows"
	"hanxi/internal/product"
	"hanxi/internal/settings"
)

// 主窗口外观与启动交接参数：集中常量化，避免魔法数字散落装配代码。
const (
	windowWidth  = 1200 // 主窗口默认宽
	windowHeight = 780  // 主窗口默认高
	// 窗口底色与前端浅色主题页面背景同值，消除 WebView 挂载前的白/异色闪。
	windowBgR = 245
	windowBgG = 246
	windowBgB = 248
	// takeoverWaitTimeout 提权重启交接：等待旧实例退出（让出单实例锁）的上限。
	takeoverWaitTimeout = 15 * time.Second
)

// RegisterEvents 注册类型化事件（wails3 绑定生成器会据此生成 TS API）。
func RegisterEvents() {
	// ext:changed / memo:changed 是无载荷事件，必须用 Void 注册：
	// 若注册具体类型而 Emit 不带载荷，会因 Wails 严格类型校验被静默丢弃。
	application.RegisterEvent[application.Void]("ext:changed")
	application.RegisterEvent[string]("tray:navigate")
	// quickmenu:opening 是无载荷事件（弹窗视图收到后重拉条目），必须用 Void 注册。
	application.RegisterEvent[application.Void]("quickmenu:opening")
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
	application.RegisterEvent[vscodeversion.DownloadProgress]("vscode:version-download")
	application.RegisterEvent[vscodeinstance.Snapshot]("vscode:instance-state")
	application.RegisterEvent[ttbversion.DownloadProgress]("translucenttb:version-download")
	application.RegisterEvent[ttbinstance.Snapshot]("translucenttb:instance-state")
	application.RegisterEvent[paseoversion.DownloadProgress]("paseo:version-download")
	application.RegisterEvent[paseoinstance.Snapshot]("paseo:instance-state")
	application.RegisterEvent[douzyversion.DownloadProgress]("douzy:version-download")
	application.RegisterEvent[nanazip.OperationProgress]("nanazip:operation-progress")
	application.RegisterEvent[nanazip.PackageSnapshot]("nanazip:package-snapshot")
	application.RegisterEvent[npmtool.OperationProgress]("envcheck:npm-tool-operation")
	application.RegisterEvent[npmtool.OperationLog]("envcheck:npm-tool-log")
	application.RegisterEvent[wsl.ReadinessUpdate]("wsl:readiness")
	application.RegisterEvent[wsl.DownloadProgress]("wsl:msi-download")
	application.RegisterEvent[wsl.CloneProgress]("wsl:clone")
	application.RegisterEvent[wsl.CompactProgress]("wsl:compact")
	application.RegisterEvent[ocr.ServiceState]("ocr:service-state")
	application.RegisterEvent[ocr.DropResult]("ocr:file-drop-result")
	application.RegisterEvent[ocr.SnipResult]("ocr:snip-result")
}

// Options 控制应用启动时行为。
type Options struct {
	StartMinimized bool
	// TakeoverPID 提权重启交接：旧实例进程 PID。非 0 时新实例在抢占单实例锁
	// 之前等待该进程退出，否则会被 Wails 判为第二实例静默自退、交接失败。
	TakeoverPID uint32
	// InitialRoute 提权重启交接：启动后前端应直达的路由（如 "/ext/bcu"）。
	// 非空时主窗口 URL 带 "#<route>" hash，App.vue 挂载后据此回航原页面。
	// 已由 cmd 入口做格式门卫，此处仅透传。
	InitialRoute string
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
	// config.json 损坏时不再 panic 造成"启动即崩、用户无修复入口"的死循环：
	// 将损坏文件隔离改名（取证副本保留在原地），以出厂默认配置降级启动并显著告警。
	// 隔离改名失败（如权限异常）仍拒绝启动——那时降级也无从落盘，需要人工介入。
	paths := settings.InitPaths()
	store, err := settings.NewStore(paths.ConfigFile())
	if err != nil {
		quarantine := fmt.Sprintf("%s.corrupt-%s", paths.ConfigFile(), time.Now().Format("20060102-150405"))
		if rerr := os.Rename(paths.ConfigFile(), quarantine); rerr != nil {
			slog.Error("failed to load settings store and quarantine rename failed, refusing to start",
				"err", err, "rename_err", rerr)
			panic(err)
		}
		slog.Error("config.json 损坏，已隔离为取证副本，本次以出厂默认配置启动（原配置可从副本手工找回）",
			"err", err, "quarantined_to", quarantine)
		if store, err = settings.NewStore(paths.ConfigFile()); err != nil {
			slog.Error("settings store still unloadable after quarantine", "err", err)
			panic(err)
		}
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
		"takeover", options.TakeoverPID,
	)

	// 提权重启交接（elevate restart handoff）：旧实例经 UAC 拉起本实例后即走
	// 正常退出流程，这里必须等它释放单实例互斥体再走 application.New 抢锁；
	// 超时不致命——真让不出去就按普通第二实例聚焦旧窗并自退（Wails 原语义）。
	if options.TakeoverPID != 0 && options.TakeoverPID != uint32(os.Getpid()) {
		if !windows.WaitProcessGone(options.TakeoverPID, takeoverWaitTimeout) {
			slog.Warn("takeover: old instance did not exit within timeout",
				"pid", options.TakeoverPID, "timeout", takeoverWaitTimeout)
		}
	}

	// 数据根治理：把历史版本平铺在根目录的模块状态 JSON 收拢进 state/。
	// 必须严格早于下方一切模块构造（store 构造即读盘）；InitLogger 已就位，
	// 迁移告警能落日志文件。放在 takeover 等待之后，把 mixed-version 双开
	// 的竞态窗口压到最小（迁移本身幂等、冲突不覆盖，竞态无损）。
	settings.MigrateRootStateFiles(paths)

	// 4. 初始化模块注册表并注入持久化 Store
	registry := extapi.NewRegistry(store)

	// 5. 初始化 fileshare 与 memo 模块并建立数据互联
	// quickmenu 需在装配根持有引用：弹窗 route 条目要唤出稍后创建的主窗口。
	quickMenuModule := quickmenu.New(store, registry)
	fileShareModule := fileshare.New(plat)
	ocrModule := ocr.New(plat) // 类型断言取服务实例，接主窗文件拖放（组件导入）
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
		vscode.New(plat),
		translucenttb.New(plat),
		paseo.New(plat),
		douzy.New(plat),
		ocrModule,
		lan.New(plat, store),
		portkill.New(plat),
		portscan.New(),
		publicip.New(plat),
		wifi.New(),
		envcheck.New(plat),
		wsl.New(plat, paths),
		wechat.New(store),
		fileShareModule,
		quickMenuModule,
		webapp.New(store),
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

	// mainWin 主窗口局部引用：闭包捕获供单实例第二启动回调聚焦，
	// 不再使用包级全局（消除可被任意代码读写的共享状态）。
	var mainWin *application.WebviewWindow

	// 自动预初始化微信等常驻监听型后台模块，确保即便未打开对应前端页面也能实时监听入站消息
	if err := registry.EnsureActive("wechat"); err != nil {
		slog.Error("预激活 wechat 模块失败，入站消息监听不可用（不影响启动）", "err", err)
	}

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
				if mainWin != nil {
					mainWin.Show()
					mainWin.Focus()
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

	// 交接路由以 hash 形态挂进初始 URL（前端无 URL 路由，hash 仅回航提示用；
	// main.ts 的 #quickmenu 分流不受 "/#/xxx" 影响）。
	initialURL := "/"
	if options.InitialRoute != "" {
		initialURL = "/#" + options.InitialRoute
	}
	win := a.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            product.Name,
		Width:            windowWidth,
		Height:           windowHeight,
		BackgroundColour: application.NewRGB(windowBgR, windowBgG, windowBgB),
		URL:              initialURL,
		Hidden:           options.StartMinimized,
		// 原生文件拖放：Windows(WebView2) 下把落放文件的真实磁盘路径交给 Go。
		// 只有落在带 data-file-drop-target 标记元素上的拖放才会回报
		//（文字识别页的组件导入区/图片区），其余区域行为不变。
		EnableFileDrop: true,
	})
	mainWin = win
	notify.GetHub().SetWailsContext(a, win)

	// 文字识别：主窗文件拖放 → OcrService（exe 落放=导入组件，图片落放=选图识别）。
	if ocrMod, ok := ocrModule.(*ocr.Module); ok && ocrMod != nil {
		ocrSvc := ocrMod.Service()
		win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
			details := e.Context().DropTargetDetails()
			if details == nil {
				return
			}
			switch details.ElementID {
			case "ocr-import-target", "ocr-image-dropzone":
				ocrSvc.HandleNativeDrop(e.Context().DroppedFiles())
			}
		})
	}

	// quickmenu：注入主窗引用供 route 条目唤窗，并随启动常驻激活全局鼠标钩子
	//（右键长按识别与 wechat 入站监听同属常驻监听型能力；设置页关模块即可停用）。
	quickMenuModule.SetMainWindow(win)
	if err := registry.EnsureActive("quickmenu"); err != nil {
		slog.Error("激活 quickmenu 模块失败，全局鼠标钩子不可用（不影响启动）", "err", err)
	}

	// 标题栏同步桥：前端 useTheme 解析出实际亮/暗与当前色板后经 SetWindowDarkMode
	// 调到这里，由平台层 DWM 属性同步原生窗框（重构蓝图铁律 8 的唯一后端例外）。
	// 沉浸式深浅打底（Win10+），Win11 再叠加精确配色让标题栏与外壳层
	// （--surface-chrome，与双栏导航共色）按色板同色，不支持的属性静默降级。
	appSvc.SetWindowDarkApplier(func(dark bool, accent string) error {
		hwnd := uintptr(win.NativeWindow())
		if err := windows.SetImmersiveDarkMode(hwnd, dark); err != nil {
			return err
		}
		_ = windows.SetTitleBarPalette(hwnd, dark, accent)
		return nil
	})
	// 启动即按持久化主题预应用（light/dark 立即可判，消除前端挂载前的原生白标题栏
	// 闪现；system 的真实解析仍由前端 useTheme 回调完成）。
	if t := store.Get(); t.Theme == "light" || t.Theme == "dark" {
		_ = appSvc.SetWindowDarkMode(t.Theme == "dark", t.Accent)
	}

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
