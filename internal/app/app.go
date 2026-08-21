// Package app 是 Composition Root：装配平台、核心服务与扩展，并暴露为 wails3 应用。
package app

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hubkit/internal/extapi"
	"hubkit/internal/logging"
	"hubkit/internal/modules/frpc"
	"hubkit/internal/modules/frpc/instance"
	"hubkit/internal/modules/frpc/version"
	"hubkit/internal/modules/lan"
	"hubkit/internal/modules/portkill"
	"hubkit/internal/modules/portscan"
	"hubkit/internal/modules/publicip"
	"hubkit/internal/platform/windows"
	"hubkit/internal/settings"
)

const (
	Name    = "HubKit"
	Version = "0.1.0"
)

// RegisterEvents 注册类型化事件（wails3 绑定生成器会据此生成 TS API）。
func RegisterEvents() {
	application.RegisterEvent[extapi.NavEntry]("ext:changed")
	application.RegisterEvent[lan.LanProgress]("lan:progress")
	application.RegisterEvent[portscan.ScanProgress]("portscan:progress")
	application.RegisterEvent[version.DownloadProgress]("frpc:version-download")
	application.RegisterEvent[instance.Snapshot]("frpc:instance-state")
	application.RegisterEvent[instance.LogEntry]("frpc:instance-log")
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

	// 5. 工具箱模块统一注册：frpc 与其余工具完全平等，均可启停
	if err := registry.Register(frpc.New(plat), lan.New(plat, store), portkill.New(plat), portscan.New(), publicip.New(plat)); err != nil {
		panic(err) // 内建模块注册失败属于编程错误，直接暴露
	}

	services := []application.Service{
		application.NewService(NewAppService(registry)),
	}
	services = append(services, registry.EnabledServices()...)

	a := application.New(application.Options{
		Name:        Name,
		Description: "frpc 内网穿透开发客户端：多实例联调、局域网扫描、释放端口",
		Services:    services,
		Assets:      assets,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	a.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            Name,
		Width:            1200,
		Height:           780,
		BackgroundColour: application.NewRGB(245, 246, 248),
		URL:              "/",
	})

	cleanup := func() {
		if logCleanup != nil {
			logCleanup()
		}
	}

	return a, cleanup
}
