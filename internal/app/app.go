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
	"hanxi/internal/history"
	"hanxi/internal/hotkey"
	"hanxi/internal/logging"
	"hanxi/internal/mcpwizard"
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
	evversion "hanxi/internal/modules/everything/version"
	"hanxi/internal/modules/fileshare"
	"hanxi/internal/modules/flclash"
	flclashinstance "hanxi/internal/modules/flclash/instance"
	flclashversion "hanxi/internal/modules/flclash/version"
	"hanxi/internal/modules/frpc"
	"hanxi/internal/modules/frpc/instance"
	"hanxi/internal/modules/frpc/version"
	"hanxi/internal/modules/gonavi"
	gonaviinstance "hanxi/internal/modules/gonavi/instance"
	gonaviversion "hanxi/internal/modules/gonavi/version"
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
	"hanxi/internal/modules/msgboard"
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
	"hanxi/internal/modules/rammap"
	rammapinstance "hanxi/internal/modules/rammap/instance"
	rammapversion "hanxi/internal/modules/rammap/version"
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
	"hanxi/internal/modules/softver"
	"hanxi/internal/modules/subnetdesk"
	subnetdeskinstance "hanxi/internal/modules/subnetdesk/instance"
	subnetdeskversion "hanxi/internal/modules/subnetdesk/version"
	"hanxi/internal/modules/sysinfo"
	"hanxi/internal/modules/termora"
	terminstance "hanxi/internal/modules/termora/instance"
	termoraversion "hanxi/internal/modules/termora/version"
	"hanxi/internal/modules/translucenttb"
	ttbinstance "hanxi/internal/modules/translucenttb/instance"
	ttbversion "hanxi/internal/modules/translucenttb/version"
	"hanxi/internal/modules/vscode"
	vscodeinstance "hanxi/internal/modules/vscode/instance"
	vscodeversion "hanxi/internal/modules/vscode/version"
	"hanxi/internal/modules/webapp"
	"hanxi/internal/modules/wechat"
	"hanxi/internal/modules/wifi"
	"hanxi/internal/modules/windterm"
	windterminstance "hanxi/internal/modules/windterm/instance"
	windtermversion "hanxi/internal/modules/windterm/version"
	"hanxi/internal/modules/wsl"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform/windows"
	"hanxi/internal/product"
	"hanxi/internal/settings"
	"hanxi/internal/snapshot"
	"hanxi/internal/updatewatch"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
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
	// webapp:windows-changed 同为无载荷事件（开窗/收起/销毁后前端重拉条目窗态）。
	application.RegisterEvent[application.Void]("webapp:windows-changed")
	// operation:changed 是无载荷事件（在途操作观察面登记/收口类变化推送，
	// 前端消费方经 ListOperations 重拉），必须用 Void 注册。
	application.RegisterEvent[application.Void]("operation:changed")
	// updates:checked 是无载荷事件（updatewatch 一轮可用更新感知收口，
	// 健康维度已写入 Registry 覆盖项），消费方经 ListModuleStates 重拉投影。
	application.RegisterEvent[application.Void]("updates:checked")
	application.RegisterEvent[lan.LanProgress]("lan:progress")
	application.RegisterEvent[portscan.ScanProgress]("portscan:progress")
	application.RegisterEvent[wechat.InboundMessage]("wechat:message-received")
	application.RegisterEvent[map[string]string]("wechat:context-token-updated")
	application.RegisterEvent[version.DownloadProgress]("frpc:version-download")
	application.RegisterEvent[instance.Snapshot]("frpc:instance-state")
	application.RegisterEvent[instance.LogEntry]("frpc:instance-log")
	application.RegisterEvent[gonaviversion.DownloadProgress]("gonavi:version-download")
	application.RegisterEvent[gonaviinstance.Snapshot]("gonavi:instance-state")
	application.RegisterEvent[fileshare.ServerStatus]("fileshare:status")
	application.RegisterEvent[fileshare.TransferEvent]("fileshare:transfer")
	application.RegisterEvent[fileshare.DropItem]("fileshare:text-dropped")
	application.RegisterEvent[application.Void]("memo:changed")
	// memo:quicksheet:opening 是无载荷事件（N16 悬浮速记卡每次唤出广播，
	// 卡视图据此清空残稿并聚焦输入条），必须用 Void 注册。
	application.RegisterEvent[application.Void]("memo:quicksheet:opening")
	// msgboard:changed 是无载荷事件（挂牌/撤牌/改配置推送，模块页与牌体视图各自拉新）。
	application.RegisterEvent[application.Void]("msgboard:changed")
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
	application.RegisterEvent[termoraversion.DownloadProgress]("termora:version-download")
	application.RegisterEvent[rammapversion.DownloadProgress]("rammap:version-download")
	application.RegisterEvent[rammapinstance.Snapshot]("rammap:instance-state")
	application.RegisterEvent[terminstance.Snapshot]("termora:instance-state")
	application.RegisterEvent[windtermversion.DownloadProgress]("windterm:version-download")
	application.RegisterEvent[windterminstance.Snapshot]("windterm:instance-state")
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
	application.RegisterEvent[softver.ScanProgress]("softver:dir-scan")
	application.RegisterEvent[softver.InstallerProgress]("softver:installer-download")
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
	if err := paths.InitError(); err != nil {
		// F6 数据根 fail loud：绑定与同级都不可用 → 弹窗指引（搬家/写 hanxi.bind 绑定）即退，
		// 绝不静默回退用户目录——旧 %APPDATA% 兜底已按裁定废弃。
		settings.ExitWithBindingGuide(err)
	}
	// 历史版本的 config/state 恢复采用「先暂存、下次启动前提交」：必须严格早于
	// settings.Store 与任何模块 store 构造，否则旧进程内存态会把刚恢复的盘面重新覆盖。
	// pending 清单/摘要任一不可信即 fail loud，绝不带旧盘面继续启动制造假成功。
	if applied, err := snapshot.ApplyPendingRestores(paths.DataDir()); err != nil {
		slog.Error("应用待恢复配置/状态失败，拒绝以旧盘面继续启动", "err", err)
		panic(err)
	} else if len(applied) > 0 {
		slog.Info("历史版本待恢复文件已在 store 加载前应用", "files", applied)
	}
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

	// Wave 4 安装事务账本与在途操作观察面（PLAN §8）：journal 目录由消费方
	// 懒建；崩溃恢复先于业务开放——与 MigrateRootStateFiles 同阶段，位于一切
	// 模块构造与 wails 应用启动之前（§8.8）。补偿器按"背书"逐事务清理：只动
	// 该事务在各自版本树内的 .tmp-<txnID> staging 与 .removing-<txnID> 同名
	// 目录，无背书孤儿一律如实 Report、不自动删盘（共享 versions 根防误伤）。
	opStore, opErr := operation.OpenStore(paths.ModulesJournalsDir())
	if opErr != nil {
		slog.Warn("journal 账本打开失败，托管写事务将保持拒绝直至重启（不阻断启动与只读功能）", "err", opErr)
	}
	// journalFault 汇总装配期账本不可信根因（打开失败/恢复扫描失败），
	// 在 SetKernel 之后统一挂降级牌（P0 批 2a fail-closed，审查 §3.4）。
	journalFault := opErr
	// 参与背书清理的托管版本树（目录前缀等领域知识由各模块 version 包持有）；
	// 后续模块接入安装事务时在此登记各自的树。
	versionTrees := map[string]*artifact.Tree{
		markeron.ID: markeronversion.OpenTree(paths.VersionsDir()),
		rufus.ID:    rufusversion.OpenTree(paths.VersionsDir()),
		// 迁移到共享内核的托管模块逐批登记（journal 背书法启动恢复的认领面）。
		ccswitch.ID:      ccswitchversion.OpenTree(paths.VersionsDir()),
		ddnsgo.ID:        ddnsgoversion.OpenTree(paths.VersionsDir()),
		translucenttb.ID: ttbversion.OpenTree(paths.VersionsDir()),
		keyviz.ID:        keyvizversion.OpenTree(paths.VersionsDir()),
		flclash.ID:       flclashversion.OpenTree(paths.VersionsDir()),
		paseo.ID:         paseoversion.OpenTree(paths.VersionsDir()),
		windterm.ID:      windtermversion.OpenTree(paths.VersionsDir()),
		termora.ID:       termoraversion.OpenTree(paths.VersionsDir()),
		rammap.ID:        rammapversion.OpenTree(paths.VersionsDir()),
		mangodisk.ID:     mangodiskversion.OpenTree(paths.VersionsDir()),
		bcu.ID:           bcuversion.OpenTree(paths.VersionsDir()),
		piclite.ID:       picliteversion.OpenTree(paths.VersionsDir()),
		papertodo.ID:     papertodoversion.OpenTree(paths.VersionsDir()),
		everything.ID:    evversion.OpenTree(paths.VersionsDir()),
		litemonitor.ID:   litemonitorversion.OpenTree(paths.VersionsDir()),
		vscode.ID:        vscodeversion.OpenTree(paths.VersionsDir()),
		guoheview.ID:     guoheviewversion.OpenTree(paths.VersionsDir()),
		quicklook.ID:     quicklookversion.OpenTree(paths.VersionsDir()),
		bili23.ID:        bili23version.OpenTree(paths.VersionsDir()),
		gonavi.ID:        gonaviversion.OpenTree(paths.VersionsDir()),
	}
	var opHub *operation.Hub
	if opStore != nil {
		report, rerr := operation.Recover(opStore, func(j operation.Journal) (bool, error) {
			tree, ok := versionTrees[j.ModuleID]
			if !ok {
				return false, nil // 未登记补偿的模块：不猜测、不自动执行（§8.8）
			}
			if err := ops.CleanTxnResidue(tree, j.TransactionID); err != nil {
				return true, err
			}
			// 背书清理后账本 Complete(compensated) 落账：该态仍留在 Pending，
			// 由观察面以 resumable 呈现，最终闭环交用户忽略/后续落账
			return true, opStore.Complete(j.TransactionID, string(operation.TxnCompensated), nil)
		})
		if rerr != nil {
			slog.Warn("journal 启动恢复扫描失败（冻结托管写事务直至重启）", "err", rerr)
			if journalFault == nil {
				journalFault = rerr
			}
		}
		if len(report.Resumed) > 0 || len(report.RolledBack) > 0 {
			slog.Info("journal 启动恢复已收口", "resumed", len(report.Resumed), "rolledBack", len(report.RolledBack))
		}
		for _, j := range report.Orphaned {
			slog.Warn("未收口事务无补偿接管，待用户处置（操作观察面可忽略）",
				"txn", j.TransactionID, "module", j.ModuleID, "operation", j.Operation, "state", j.State)
		}
		for _, j := range report.Quarantined {
			slog.Warn("journal 文件损坏，已改名隔离取证", "entry", j.TransactionID)
		}
		// 无背书事务前缀残骸：只上报不动盘（删除决策永远保留给有账本背书的收口路径）
		if pending, perr := opStore.Pending(); perr == nil {
			backed := make(map[string]bool, len(pending))
			for _, j := range pending {
				backed[j.TransactionID] = true
			}
			for id, tree := range versionTrees {
				for _, name := range ops.ListUnbackedTxnDirs(tree, backed) {
					slog.Warn("托管版本树存在无 journal 背书的事务前缀残骸（不自动清理）", "module", id, "dir", name)
				}
			}
		}
	}
	// 文件面收尸（ADR-0002 §4）：Fetch 强杀遗留的 `<name>.<rand>.part-<hex>`
	// 临时件不在目录背书清理职责内，这里对 installers/、versions/ 两根各扫
	// 一轮——只删超龄普通文件（目录/链接拒删，活跃下载不受扰），与 journal
	// 可用性无关，账本降级模式下照常执行。
	// 残件主要产端是 os.CreateTemp(destPath="%TEMP%\\hanxi-*")，故三根同扫;
	// 形状+超龄+普通文件三闸俱备才删，不碰他程序文件(见 artifact.CleanStaleParts)。
	ops.CleanStaleDownloadParts([]string{os.TempDir(), paths.InstallersDir(), paths.VersionsDir()})
	// 恢复之后构造观察面：resumable 回灌投影反映收口后的现态（§2.3）。
	// opStore 打开失败时得到纯内存 Hub（仍可观察在途操作，只是不落账）。
	opHub = operation.NewHub(opStore)
	// N28：量化进度经 Hub 节流窗口（800ms 合并）汇入 operation:changed，
	// 观察面横幅与模块内进度条不再两面脱节；结构性广播维持即时。
	opHub.SetChangeNotifier(ops.BroadcastChanged)
	ops.SetKernel(opStore, opHub)
	if journalFault != nil {
		ops.MarkJournalDegraded(journalFault) // 装配期账本不可信：新托管写事务一律拒绝（重启恢复）
	}

	// 4. 初始化模块注册表并注入持久化 Store
	registry := extapi.NewRegistry(store)
	// Wave 1 逻辑安装态：注入 receipt 存储。未安装 = 不导航、不初始化、不业务调用
	// （ADR-0001 §1.5）；老用户无损迁移在下方 Register 成功后幂等补建凭据。
	receipts := settings.NewReceiptStore(paths.ModulesReceiptsDir(), paths.ModulesLedgerFile())
	registry.SetReceiptStorage(receipts)

	// 5. 初始化 fileshare 与 memo 模块并建立数据互联
	// quickmenu 需在装配根持有引用：弹窗 route 条目要唤出稍后创建的主窗口。
	quickMenuModule := quickmenu.New(store, registry)
	fileShareModule := fileshare.New(plat)
	ocrModule := ocr.New(plat)                  // 类型断言取服务实例，接主窗文件拖放（组件导入）
	portkillModule := portkill.New(plat)        // 类型断言取服务实例，注入统一历史
	msgboardModule := msgboard.New(plat, paths) // 持有引用：热键注册器随装配根注入（R1 收编）
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
		gonavi.New(plat),
		guoheview.New(plat),
		sysinfo.New(),
		ddnsgo.New(plat),
		rustdesk.New(plat),
		subnetdesk.New(plat),
		rufus.New(plat),
		bili23.New(plat),
		vscode.New(plat),
		translucenttb.New(plat),
		paseo.New(plat),
		windterm.New(plat),
		termora.New(plat),
		rammap.New(plat),
		douzy.New(plat),
		ocrModule,
		lan.New(plat, store),
		portkillModule,
		portscan.New(),
		publicip.New(plat),
		wifi.New(),
		envcheck.New(plat),
		softver.New(plat),
		wsl.New(plat, paths),
		wechat.New(store),
		fileShareModule,
		quickMenuModule,
		webapp.New(store),
		msgboardModule,
	}
	if memoModule != nil {
		modulesToRegister = append(modulesToRegister, memoModule)
	}

	if err := registry.Register(modulesToRegister...); err != nil {
		panic(err) // 内建模块注册失败属于编程错误，直接暴露
	}

	// 一次性安装迁移 + 增量认新（EnsureSeen 名单账本，ADR-0001 §1.5）：首次运行
	// 为全部注册模块补建 builtin-logical 凭据（Enabled=true→installed+enabled、
	// false→installed+disabled，入口与数据零丢失）；用户卸载过的模块**永不复活**；
	// 版本升级新增的模块自动安装。失败仅告警不阻断启动：缺凭据的模块按未安装
	// 呈现，可在模块中心一键安装找回。
	registeredIDs := make([]string, 0, len(modulesToRegister))
	for _, info := range registry.List() {
		registeredIDs = append(registeredIDs, info.ID)
	}
	if _, err := receipts.EnsureSeen(registeredIDs, extapi.ReceiptBuiltinLogical); err != nil {
		slog.Warn("逻辑安装凭据迁移未完成，部分模块可能呈现未安装（可在模块中心安装找回）", "err", err)
	}

	// 统一历史记录：公共 Store 挂 state/history.json，装配根注入首批三处接缝
	//（ocr 识别 / portkill 查询与查杀 / npmtool 装升卸）。记录点与口径见各模块接入提交。
	historyStore := history.NewStore(paths.StateDir())
	historySvc := history.NewHistoryService(historyStore, store)
	if ocrMod, ok := ocrModule.(*ocr.Module); ok && ocrMod != nil {
		ocrMod.Service().SetHistory(historyStore, func() bool { return store.Get().HistoryOcrFullText })
	}
	if pkMod, ok := portkillModule.(*portkill.Module); ok && pkMod != nil {
		pkMod.Service().SetHistory(historyStore)
	}
	npmtool.SetHistory(historyStore)

	appSvc := NewAppService(registry, store)
	// Wave 4-B 操作观察面 RPC：在途/近期事务查询与 resumable 残留忽略
	// （双样本 markeron/rufus 的安装事务由此对前端可见可处置）。
	appSvc.SetOperations(opHub, makeDismissResumable(opStore, opHub, versionTrees))
	// "可用更新"感知链（Wave 4+ 健康维度的真实来源）：从注册模块中收集实现
	// extapi.UpdateChecker 的托管模块交给调度器；Restore 回灌上轮结果供前端
	// 首帧投影，Start 仅挂一次性延迟首检（无常驻 ticker，按需刷新走
	// RefreshUpdates RPC）。调度器直调检查器、不经调用门与懒激活，构造/回灌
	// 零网络 IO，网络比较全部延迟到延迟首检与手动触发。
	updateWatcher := updatewatch.New(registry, collectUpdateCheckers(modulesToRegister), paths.StateDir())
	updateWatcher.Restore()
	appSvc.SetUpdateWatcher(updateWatcher)
	// 历史版本（自动快照平台底座）：非 extapi 模块，服务面与 AppService 同级；
	// 触发接线在主窗创建后（见下方窗口事件钩子），退出补拍挂 OnShutdown 链。
	snapSvc := snapshot.New(paths, store)
	services := []application.Service{
		application.NewService(appSvc),
		application.NewService(notify.NewNotificationService()),
		// 统一历史：公共包型服务直挂（notify 同位置先例），不进 modulesToRegister、无 Nav。
		application.NewService(historySvc),
		application.NewService(snapSvc),
		// MCP 安装向导（「AI 接入」分区后端，F4b）：平台级服务直挂同 snapSvc 先例；
		// 无后台协程无常驻状态，只在设置分区打开时按需读盘分析。
		application.NewService(mcpwizard.NewService(paths)),
		// 红线图标运行期本机提取（N27 尾巴）：平台级服务直挂同 snapSvc 先例，
		// 一枚 IconPNG RPC 打通 rammap/recordly/vscode（许可红线永不入仓，
		// 只在机主本机就地提取，见 runtimeicon_service.go 头注释）。
		application.NewService(NewRuntimeIconService(registry)),
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
			// 退出前同步补最后一发（3s 闸门卡死不拖退出；ShutdownAll 已停写入方，
			// 此时盘上即终态）。
			snapSvc.FlushOnExit()
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
		// 历史版本 → 便签热恢复：memo/<id>.md 回滚走 RestoreFile（原样落盘 +
		// 内存换装 + memo:changed），前端即时可见，无需重启。
		snapSvc.SetMemoRestorer(mMod.GetService().RestoreFile)
		// 历史版本 → 文件清单标题映射：ListFiles 左栏把 memo/<id>.md 显示成
		// 便签标题（N33 §2），映射不到由快照侧回落文件名。
		snapSvc.SetMemoTitleResolver(mMod.GetService().TitleForWhitelistPath)
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

	// 历史版本（自动快照）触发源一：主窗失焦/隐藏/最小化置失活标记——关到托盘
	// 走 Hide() 不销毁、驻托盘后不再有失焦事件，故 Hide/Minimise 等价补位
	// （触发源二 mtime 空闲巡检、三退出补拍在 snapshot 服务与 OnShutdown 内）。
	win.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) { snapSvc.NoteDeactivated() })
	win.OnWindowEvent(events.Common.WindowHide, func(*application.WindowEvent) { snapSvc.NoteDeactivated() })
	win.OnWindowEvent(events.Common.WindowMinimise, func(*application.WindowEvent) { snapSvc.NoteDeactivated() })
	win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) { snapSvc.NoteActivated() })
	win.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) { snapSvc.NoteActivated() })
	win.OnWindowEvent(events.Common.WindowUnMinimise, func(*application.WindowEvent) { snapSvc.NoteActivated() })
	snapSvc.Start()

	// 可用更新感知一次性延迟首检（主窗就绪后挂起，不建常驻 ticker）；
	// 退出经 cleanup 的 Stop 收口未触发的延迟 goroutine。
	updateWatcher.Start()

	// 全仓唯一一张热键注册表（底层即 Wails GlobalShortcut 管理器）：ocr 剪贴板
	// 识图、留言板 toggle 等所有热键槽位共用同一份记账与原子换键语义（F2-③）。
	hk := hotkey.NewRegistry(a.GlobalShortcut)

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
		// 全局热键"剪贴板识图"（默认 Ctrl+Alt+T）：通用注册器 + 命令派发接线。
		setupSnipHotkey(hk, registry, ocrSvc)
	}

	// quickmenu：注入主窗引用供 route 条目唤窗，并随启动常驻激活全局鼠标钩子
	//（右键长按识别与 wechat 入站监听同属常驻监听型能力；设置页关模块即可停用）。
	quickMenuModule.SetMainWindow(win)
	if err := registry.EnsureActive("quickmenu"); err != nil {
		slog.Error("激活 quickmenu 模块失败，全局鼠标钩子不可用（不影响启动）", "err", err)
	}

	// msgboard：全局热键收编入通用注册器（R1）——这里只交接注册表，绑定/解绑
	// 随模块 OnInit/OnDestroy 驱动，须在激活前注入。热键与 quickmenu 钩子同属
	// 常驻监听能力——开机即注册（beta.10 会把 Run 前的注册排入 pending，启动瞬间
	// 完成 OS 绑定），主窗不开也能挂牌。
	msgboardModule.SetHotkeyRegistry(hk)
	if err := registry.EnsureActive("msgboard"); err != nil {
		slog.Error("激活 msgboard 模块失败，留言板热键不可用（不影响启动）", "err", err)
	}

	// memo（N16 悬浮速记卡）：与 msgboard 同款接线——交接注册表后常驻激活，
	// 热键随 OnInit 绑定（开机即待命，主窗不开也能唤卡）；模块在设置页停用
	// 即摘热键并销毁速记卡。memo.New 失败（数据损坏）时 memoModule 为 nil，
	// 自然跳过——不激活、不绑键。
	if mMod, ok := memoModule.(*memo.Module); ok && mMod != nil {
		mMod.SetHotkeyRegistry(hk)
		if err := registry.EnsureActive("memo"); err != nil {
			slog.Error("激活 memo 模块失败，速记热键不可用（不影响启动）", "err", err)
		}
	}

	// wsl：预激活——USB 开机自动共享（F9）走「账本+重放」范式，重放随模块
	// OnInit 调度（总开关关/账本空即秒退，无常驻开销）；不预激活则该页不进
	// 就不重放，"开机自动"名存实亡。
	if err := registry.EnsureActive("wsl"); err != nil {
		slog.Error("预激活 wsl 模块失败，USB 自动共享不会随启动重放（不影响启动）", "err", err)
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
		updateWatcher.Stop()
		snapSvc.Stop()
		if logCleanup != nil {
			logCleanup()
		}
	}

	return a, cleanup
}
