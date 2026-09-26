// Package piik 内置模块：piik（上游 TNTcraftHIM/Piik，headless 屏幕分享服务器）
// 托管编排层——版本管理 + JobObject 托管启停 + 状态/机读信号投影 + 浏览器界面直达。
//
// **服务型骨架（headless service host）**：piik 无自有窗口、无托盘、无 WebView2
// 依赖，产品 UI 就是系统浏览器里的 http://127.0.0.1:<port>/ 页面。它是托管家族
// 第一个服务型样本，窗口托管模板整族不适用，本层据此做如下负裁决（裁决依据
// docs/plans/PLAN_PIIK_HOSTING.md，此处只落结论，防后人回头照抄窗口先例）：
//
//   - 无"唤窗"：族名 **OpenWindow** 保留，语义重定义为"用系统浏览器打开本机
//     界面"（plat.OpenURL）——没有窗口可唤，也没有信使二次拉起、EnumWindows、
//     WM_CLOSE 可投；界面地址单点由 A 线 instance.URLForPort 组装，本层只决定
//     何时把它交给浏览器；两动词严格分工：Start 起服务、OpenWindow 只开页面，
//     **OpenWindow 绝不冷启动**（起服务在局域网敞开分享端口，须机主明示）；
//   - 引擎恒注入 PIIK_CLIENT_GATE_NO_BROWSER=true（它同时是机读模式开关与浏览
//     器闸：开了机读字段解析，也就闸掉了上游"自动拉浏览器"）——上游在机读模式
//     下不再替用户开页面，所以"打开界面"这条路的价值被放大：快照 noBrowser 位
//     （A 线对 GATE_NO_BROWSER 的命中账）配合 GetStatus.consoleUrl 构成兜底
//     链接，绝不让用户面对"启动了但什么都没发生"；
//   - 优雅停走上游官方通道（stdin 写入即优雅退出）+ 宽限 + JobObject 兜底，
//     孙进程 cloudflared / piik-capture 同笼回收——通道理在 A 线 instance 包；
//   - **空闲自动退出不接线**（followOnExit 开关保留）：服务在跑就有会话价值，
//     闲置 ≠ 可杀——公网邀请链接随时可能有人正连着，空闲杀服务是无告知的断服
//     （rustdesk/subnetdesk 同族裁决理由）；
//   - 无提权通道：上游 manifest asInvoker，#17 三重契约（740/elevateHint/
//     ElevateRestart）一律不接线；
//   - 启动失败文案绝不指向 WebView2（上游无此依赖），如实指向"进程异常退出 /
//     端口占用"——照抄 ccswitch 措辞即错；
//   - 公网隧道（--link 类能力）由 piik 页面侧发起，hanxi 不代管：本层不提供
//     "一键开公网"，不注入隧道参数，只承担 JobObject 回收义务与状态如实。
//
// 对 PLAN 档案"数据不改道"负裁决的**推翻性回填**（档案③末行、⑤步骤 7、遗留
// 声明第 1 条均按侦查期"未见改道接口"下结论）：阶段 0 后续实锤上游有官方改道
// 开关——CLI `--config <路径>` 与 `--log-dir <目录>`（默认落 %APPDATA%\Piik\
// client.json），A 线引擎组参面即其投射。故本层托管启动**恒注入** Hanxi 数据根
// 下的 <数据根>/piik/client.json 与 <数据根>/piik/logs：照 DBX 裁决口径，路径
// 与 activeVersion 无关、跨版本共享，删版本不删数据，绝不让配置与日志随版本
// 目录走或外溢到 %APPDATA%。前端卸载确认框文案（C 线）需按此改口。
//
// 所有业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）：未安装/停用/
// 阻止模块的任何方法调用被拒，且调用在途期间停用会等待 drain。
package piik

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/notify"
	"hanxi/internal/ops"
	"hanxi/internal/platform"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"

	piikinstance "hanxi/internal/modules/piik/instance"
	piikversion "hanxi/internal/modules/piik/version"
)

const (
	// moduleID 是本模块的注册键（通知/事件/账本 module 字段）。装配骨架 module.go
	// 另有导出常量 ID（值必须与此一致）——本层刻意不叫 ID，避免与在途的 module.go
	// 线在同一包内重复声明，联编收口时如有二义以 module.go 为准。
	moduleID = "piik"

	readyTimeout  = 20 * time.Second // 冷启动就绪上限（端口监听起来 = 服务就绪）
	watchInterval = 5 * time.Second  // 外部实例感知轮询间隔

	// defaultPort 上游默认监听端口——值取 A 线 instance.DefaultPort 单点（端口
	// 定值由本层裁决后随 Options.Port 下传，两处常量绝不允许各写一份）；
	// 单实例互斥由"8787 能否绑上"这一事实达成（上游无命名互斥体、无
	// second-instance 协议）。
	defaultPort = piikinstance.DefaultPort
	// portScanLimit 端口分配器候选上限：8787 起被占则 +1，最多扫到 8796。
	// 10 个候选是"够用即止"的有界红线——绝不无限扫（无限扫等于把端口冲突
	// 藏起来，且在最坏情况下拖住 RPC 不放）。A 线包注释明示"8787 起、被占 +1
	// 扫描 ≤10 的分配纪律归 service 层执行，instance 只收定值"，本常量即那条纪律。
	portScanLimit = 10

	// 托管数据目录（Hanxi 数据根下 `piik` 子目录，paths.DataDir() 家族惯例）：
	// client.json（上游配置）与 logs（上游日志）统一落此处，跨版本共享、
	// 删版本不删数据。
	dataDirChildName = "piik"
	configFileName   = "client.json"
	logDirName       = "logs"

	loopbackHost = "127.0.0.1"
)

// versionEngine 版本引擎依赖面（冻结契约的 piikversion 公开面投影）：生产装配
// piikversion.NewManager，单测注入假 Manager 以隔离磁盘与网络、确定性驱动
// 下载时序/卸载清账/漂移投影路径。签名以 A 线在盘实面为准，本接口即联编收口点。
type versionEngine interface {
	ListRemote() ([]Release, error)
	ListInstalled() ([]VersionInfo, error)
	DownloadContext(ctx context.Context, txnID, targetVersion string, progress func(DownloadProgress)) error
	ImportLocal(srcDir string) (VersionInfo, error)
	Remove(targetVersion string) error
	ResolveExe(targetVersion string) (string, error)
	VerifyLedger(targetVersion string) (drifted bool, note string)
	CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error)
}

// instanceEngine 实例引擎依赖面（A 线 piikinstance 公开面的本包投影）：服务型
// 骨架对引擎的要求比窗口托管更窄——没有唤窗/信使/主窗可见性，只有起停与状态。
// 启动参数携带本层分配的端口与托管数据目录落点，引擎把它变成
// `--local --port <p> --config <路径> --log-dir <目录>` 组参 +
// PIIK_CLIENT_GATE_NO_BROWSER=true env（A 线 buildArgs/buildEnv 实面）。
//
// 两面账目都在引擎、本层零副本：
//   - 端口账：Snapshot.Port = 本代/最近一代分配端口（分配纪律在 service，
//     记账在 instance，A 线包注释同口径）；
//   - 机读账：Snapshot 的 localAccessOpen/passwordSet/lanInvitation/
//     publicInvitation/noBrowser 由 stdout 逐行解析入账——口令明文在解析处
//     即弃，A 线类型里根本没有承载它的字段，"口令值绝不上前端"是类型层事实；
//   - 状态词表五态（stopped/starting/running/failed/external）：优雅停收口相
//     在引擎内部（stopping）对前端折并入 running，**无 quitting 档**。
type instanceEngine interface {
	Start(opts piikinstance.Options) error
	// Quit 优雅停（上游官方 stdin 通道 + 宽限，超时 JobObject 兜底）。
	Quit() error
	// Stop 立即强杀自有实例（Shutdown 通道用，不耗宽限）。
	Stop() error
	Snapshot() Snapshot
	// RefreshExternal 外部实例感知校正（静止态判据为进程名单因子——外部实例
	// 端口不可知，无从拨测；实现在 A 线）。
	RefreshExternal()
	WaitReady(timeout time.Duration) bool
}

// 契约钉：A 线具体实现必须满足上面两面（A 线已落盘，本两行是编译期事实核对，
// 后续任何一侧方法面漂移都在此处当场报错，不留到调用栈深处才炸）。
var (
	_ versionEngine  = (*piikversion.Manager)(nil)
	_ instanceEngine = (*piikinstance.Engine)(nil)
)

// PiikService 向前端暴露 piik 版本管理、托管启停、界面直达与安全面如实披露。
// piik 的产品功能（开播、邀请、观众入会）全部在系统浏览器里的上游页面完成——
// 纯托管决策：其界面即产品价值，且分享行为涉及实时音视频流，内嵌重做既不现实
// 也不安全。hanxi 一侧只有四件事：版本、起停、状态灯、打开浏览器。
type PiikService struct {
	plat    platform.Platform
	manager versionEngine
	store   *piikStore
	engine  instanceEngine
	holder  *extapi.LeaseHolder

	// 托管数据目录三账（构造期定格，与 activeVersion 无关）。
	dataDir    string
	configPath string
	logDir     string

	downloadMu sync.Mutex
	downloads  map[string]struct{}
	watchMu    sync.Mutex
	watching   bool
	watchStop  chan struct{}

	// 单测 seam（生产装配恒不注入，零成本）：
	//   - downloadDriver/downloadProbe：同 ccswitch/dbx 纪律，隔离网络并锁死
	//     "done 成功回执送达时首装落账已完成"的时序（termora 2ac9b3b 同型病灶）；
	//   - bindProbe：端口试绑（生产 net.ListenTCP 即绑即放）——单测不真占端口；
	//   - connectProbe：端口连通（external 实例的界面定位，生产 TCP 拨测）；
	//   - browserOpen：界面打开（生产 = plat.OpenURL，测试注入记录器，绝不让
	//     单测真的唤起浏览器）。
	// 端口与机读字段刻意**没有** seam：它们来自 A 线快照（唯一事实源），
	// 由 fakeEngine 摆 Snapshot 直接造景，比再造一层假投影更接近真实形状。
	downloadDriver func(ctx context.Context, txnID, targetVersion string, emit func(DownloadProgress)) error
	downloadProbe  func(p DownloadProgress)
	bindProbe      func(port int) bool
	connectProbe   func(port int) bool
	browserOpen    func(url string) error
}

// NewPiikService 装配版本管理器、持久化 store 与实例引擎（引擎状态回调指回本
// service，二者生命周期一致）；构造无 IO。
func NewPiikService(plat platform.Platform, holder *extapi.LeaseHolder) *PiikService {
	paths := settings.GetPaths()
	dataDir := filepath.Join(paths.DataDir(), dataDirChildName)
	svc := &PiikService{
		plat:       plat,
		manager:    piikversion.NewManager(paths.VersionsDir()),
		store:      newPiikStore(paths.StateDir()),
		downloads:  make(map[string]struct{}),
		holder:     holder,
		dataDir:    dataDir,
		configPath: filepath.Join(dataDir, configFileName),
		logDir:     filepath.Join(dataDir, logDirName),
	}
	svc.bindProbe = portBindable
	svc.connectProbe = portConnectable
	svc.browserOpen = svc.openInBrowser
	svc.engine = piikinstance.NewEngine(plat.Job(), piikinstance.NewProbe(), piikinstance.Callbacks{
		OnState: svc.onEngineState,
	})
	return svc
}

// ---------- 实例事件与后台感知 ----------

// onEngineState A 线引擎状态迁移回调（装配布线：构造期注册的 Go 直调路径，
// 不经运行态）：本层只做广播与告警转呈，不改写快照——端口/机读账都在引擎手里
// （Snapshot.Port 与机读字段即唯一事实源），本层另存一份必漂移。
func (s *PiikService) onEngineState(snap Snapshot) {
	s.emitInstanceState(snap)
}

// emitInstanceState 引擎状态迁移 → 事件 piik:instance-state（载荷为 A 线快照，
// 由装配根 application.RegisterEvent 钉死；秘密值不得进该类型，见 PiikStatus 注释）。
func (s *PiikService) emitInstanceState(snap Snapshot) {
	slog.Debug("piik instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("piik:instance-state", snap)
	}
	if snap.State == piikinstance.StateFailed && snap.Error != "" {
		notify.Error(moduleID, "piik 实例异常", snap.Error, "/ext/piik")
	}
}

// activate 启动后台外部实例感知：5s 轮询"进程名 + 端口监听"双因子校正
// external/stopped。（自有实例的存活由引擎 hold 的进程句柄感知，不需轮询。）
// 本模块**无**空闲退出巡检——服务型骨架裁决，见包注释。
func (s *PiikService) activate() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watching {
		return
	}
	s.watching = true
	stop := make(chan struct{})
	s.watchStop = stop
	go func() {
		t := time.NewTicker(watchInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.engine.RefreshExternal()
			}
		}
	}()
}

// ---------- 版本管理（版本八件套，对照 ccswitch/dbx 形制） ----------

// ListReleases 获取远程可用版本列表（GitHub API digest 官方摘要，10 分钟缓存）。
// 上游日更风暴（22 版/14 天）下本方法只如实列版，追不追由机主在版本管理页手点
// ——不存在自动跟版通道（metaHints 第 1 条的账）。
func (s *PiikService) ListReleases() ([]Release, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListRemote()
}

// ListInstalledVersions 获取本地已安装版本列表（含账外漂移复查明细 HashDrifted/DriftNote）。
func (s *PiikService) ListInstalledVersions() ([]VersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.manager.ListInstalled()
}

// piikInstallSteps 托管资产事务的 journal 步骤词汇（Wave 4）：piik 是裸 zip
// 形态，verify（官方摘要双核）由内核 Fetch 折进 download 步内完成，模块进度
// 词表不单独可见，如实不造幻影步骤。
var piikInstallSteps = []string{"download", "unpack", "place"}

// DownloadVersion 后台下载指定版本：立即返回，全程经事件 piik:version-download
// 推送进度；同时开一笔 journal 托管事务（install 首装 / update 向已托管工具链
// 追加版本，managed-declarative 资产形态）——journal 先落盘再副作用，进度阶段
// 迁移逐步 Advance，收口经观察面 Handle 自动落账并广播 operation:changed
// （与既有模块事件双通道并行，Wave 4-B 接线，markeron/ccswitch/dbx 同构）。
func (s *PiikService) DownloadVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")

	s.downloadMu.Lock()
	if _, ok := s.downloads[targetVersion]; ok {
		s.downloadMu.Unlock()
		return "in-progress", nil
	}

	// 已安装则直接返回，避免重复下载
	installed, err := s.manager.ListInstalled()
	if err == nil {
		for _, v := range installed {
			if strings.EqualFold(strings.TrimPrefix(v.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
				s.downloadMu.Unlock()
				return "already-installed", nil
			}
		}
	}
	opKind := extapi.OpInstall
	if err == nil && len(installed) > 0 {
		opKind = extapi.OpUpdate
	}
	txnID := uuid.NewString()

	s.downloads[targetVersion] = struct{}{}
	s.downloadMu.Unlock()

	go func() {
		defer func() {
			s.downloadMu.Lock()
			delete(s.downloads, targetVersion)
			s.downloadMu.Unlock()
		}()
		lease, lerr := s.holder.EnterBackground(context.Background())
		if lerr != nil {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("piik:version-download", DownloadProgress{Version: targetVersion, Stage: "error", Message: lerr.Error()})
			}
			notify.Error(moduleID, "版本下载失败", fmt.Sprintf("piik %s 后台租约开启失败: %v", targetVersion, lerr), "/ext/piik")
			return
		}
		txn, terr := ops.BeginTxnWithLifecycle(lease.Context(), lease, moduleID, opKind, extapi.DeliveryManagedDeclarative,
			targetVersion, txnID, piikInstallSteps)
		if terr != nil {
			lease.Release()
			// 账本拒绝开启（如未收口事务占位）：下载根本不启动，error 事件
			// 如实送达前端下载卡片收口（既有事件契约兜底），并提示用户
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("piik:version-download", DownloadProgress{Version: targetVersion, Stage: "error", Message: terr.Error()})
			}
			notify.Error(moduleID, "版本下载失败", fmt.Sprintf("piik %s 事务开启失败: %v", targetVersion, terr), "/ext/piik")
			return
		}
		stepIdx := -1
		var stepErr error
		defer func() {
			if txn.JournalFailed() {
				msg := "journal 事务步进失败"
				if stepErr != nil {
					msg = stepErr.Error()
				}
				txn.Fail("journal-degraded", msg)
			} else if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
			}
			txn.Close()
		}()
		emit := func(p DownloadProgress) {
			slog.Debug("piik download progress", "version", p.Version, "stage", p.Stage, "done", p.Done)
			// "首装自动设使用"的落账必须发生在 done 事件发出之前：前端共享 store
			// 收到 done 即复刷 GetActiveVersion，落账晚于事件则瞬时复刷读到空值
			// （termora 2ac9b3b 同型病灶、同型修法）。done 仅在 manager 落位
			// Commit 成功后发出。
			if p.Stage == "done" && s.store.GetActive() == "" {
				_ = s.store.SetActive(targetVersion)
			}
			if s.downloadProbe != nil {
				s.downloadProbe(p)
			}
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("piik:version-download", p)
			}
			// journal 只在阶段迁移处落盘（§8.2 每步迁移即持久化）；
			// 下载分块进度仅进观察面内存投影，不产生 fsync 风暴
			switch p.Stage {
			case "downloading":
				if stepIdx < 0 {
					stepIdx = 0
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
				txn.Progress(p.Done, p.Total)
			case "extract":
				if stepIdx < 1 {
					stepIdx = 1
					stepErr = txn.Step(stepIdx)
					if stepErr != nil {
						txn.Cancel()
						return
					}
				}
			}
			if p.Stage == "done" {
				notify.Success(moduleID, "版本下载成功", fmt.Sprintf("piik %s 已成功安装", p.Version), "/ext/piik")
			}
		}
		drive := s.downloadDriver
		if drive == nil {
			drive = s.manager.DownloadContext
		}
		if err := drive(txn.Context(), txnID, targetVersion, emit); err != nil {
			// N26 用户主动取消：按 2b 纪律如实收口，票面话术不露 ctx 原始错误；
			// 主动动作不发"失败"系统通知（前端票面已呈现「已取消」）。
			if txn.Err() != nil {
				txn.Fail("operation-cancelled", "托管操作已取消")
				emit(DownloadProgress{Version: targetVersion, Stage: "error", Message: "托管下载已取消"})
			} else {
				txn.Fail("asset-install-failed", err.Error())
				emit(DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
				notify.Error(moduleID, "版本下载失败", fmt.Sprintf("piik %s 下载失败: %v", targetVersion, err), "/ext/piik")
			}
			return
		}
		if err := txn.Done(); err != nil {
			emit(DownloadProgress{Version: targetVersion, Stage: "error", Message: err.Error()})
			notify.Error(moduleID, "版本下载失败", fmt.Sprintf("piik %s 事务收口失败: %v", targetVersion, err), "/ext/piik")
			return
		}
		// "首装自动设使用"已在 done 事件发出前的 emit 收口落账（见上），此处不再重复。
	}()

	return "started", nil
}

// RemoveVersion 卸载指定版本（正在运行的版本拒绝卸载）。
// 数据面纪律（照 DBX 裁决口径）：卸载只删版本隔离目录（主 exe 与 runtime
// 兄弟项等条目），**绝不触碰** Hanxi 数据根下的 piik 托管数据目录——配置与
// 日志跨版本共享，删版本不丢数据；本模块不提供任何删数据通道，"是否连数据
// 一起清"的处置权永远留在机主手里（前端卸载确认框的明示文案见 MetaHints）。
// 末版可卸口径（落地第一天就按此执行，不留"最后一版卸不掉"的死锁）：允许
// 卸载到"零版本"——卸载后列表为空、activeVersion 清空、下一次启动给出"先
// 下载或导入"的指引。外部实例正占用文件时由 manager 的 rename 隔离删除如实
// 报错，不谎报成功。
func (s *PiikService) RemoveVersion(targetVersion string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if snap := s.engine.Snapshot(); snap.State == piikinstance.StateRunning &&
		strings.EqualFold(strings.TrimPrefix(snap.Version, "v"), strings.TrimPrefix(targetVersion, "v")) {
		return fmt.Errorf("版本 %s 正在运行，请先退出", targetVersion)
	}
	if err := s.manager.Remove(targetVersion); err != nil {
		return err
	}
	// 卸载的是当前设定版本则清空，下次冷启动自动回退最新已装（末版卸空即指引下载）
	if s.store.GetActive() == targetVersion {
		_ = s.store.SetActive("")
	}
	return nil
}

// SetActiveVersion 设定使用版本（先校验已安装，再持久化）——上游日更风暴下
// 这就是"钉版本"的手，钉住哪个用哪个。
func (s *PiikService) SetActiveVersion(targetVersion string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	targetVersion = "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if _, err := s.manager.ResolveExe(targetVersion); err != nil {
		return "", err
	}
	if err := s.store.SetActive(targetVersion); err != nil {
		return "", err
	}
	return targetVersion, nil
}

// GetActiveVersion 返回当前设定版本（空字符串 = 未指定，冷启动自动用最新已装）。
func (s *PiikService) GetActiveVersion() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return s.store.GetActive(), nil
}

// ImportLocal 导入本地已有的 piik（绿色版目录，须含主 exe 与 runtime 兄弟项）。
// 导入物只有版本隔离目录内的条目；配置与日志不受导入影响（托管启动恒把
// ConfigPath/LogDir 指向 Hanxi 数据根）。运行中的实例拒绝导入：Windows 下
// 运行中的 exe 被独占，拷贝必然失败。
func (s *PiikService) ImportLocal(srcDir string) (VersionInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return VersionInfo{}, gateErr
	}
	defer release()
	if snap := s.engine.Snapshot(); snap.State == piikinstance.StateRunning || snap.State == piikinstance.StateExternal {
		return VersionInfo{}, fmt.Errorf("piik 正在运行，请先退出再导入")
	}
	info, err := s.manager.ImportLocal(strings.TrimSpace(srcDir))
	if err != nil {
		return VersionInfo{}, err
	}
	// N24 契约：没有任何版本时，第一个到手的版本默认=使用版本（导入链与下载链同源）。
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(info.Version)
	}
	return info, nil
}

// ---------- 端口分配与托管数据目录 ----------

// portBindable 生产试绑实现：net.ListenTCP 通配地址即绑即放。
// 绑通配（不指定 IP）而非 127.0.0.1：上游 piik 实际监听 0.0.0.0，试绑面必须
// 与真实监听面一致才可信——"回环能绑"不等于"全网卡能绑"，反之亦然。
// 不开 SO_REUSEADDR 类豁免选项：本函数要的是"这个端口现在真没人用"的诚实答案。
func portBindable(port int) bool {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// portConnectable 生产连通实现：TCP 拨测 400ms 超时（回环握手正常在毫秒级，
// 慢只出现在半开/丢弃场景）。用于 external 实例的界面定位，与试绑是两个
// 不同问题：试绑问"我能占吗"，拨测问"有服务在听吗"。
func portConnectable(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(loopbackHost, fmt.Sprintf("%d", port)), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// allocatePort 端口分配器：自 8787 起逐一试绑，被占则 +1，最多扫 portScanLimit
// 个候选（8787..8796）。首个可绑端口即答案（绑上立刻放开，真正的 bind 由上游
// 进程自己做）。
//
// 为什么要这一层（对档案①#7 的口径补充）：上游把"8787 能否绑上"当单实例互斥，
// 托管侧照单全收就会出现"别人占了 8787，Hanxi 启动 piik 直败"的死结。分配器
// 把互斥面变成可控面：外部有主就让相邻空口，各服务各的端口，互不打扰。
//
// 局限如实声明：试绑成功到上游真正 bind 之间存在 TOCTOU 窗口（恰好被第三者
// 抢走）。本层不在这里做重试循环假装解决——上游 bind 失败会由引擎如实报
// failed，错误文案点名端口，机主重新点一次启动即可（一次点击成本，换零伪装）。
func (s *PiikService) allocatePort() (int, error) {
	probe := s.bindProbe
	if probe == nil {
		probe = portBindable // 装配缺位时退回生产试绑，绝不静默判"全被占"
	}
	for i := 0; i < portScanLimit; i++ {
		if probe(defaultPort + i) {
			return defaultPort + i, nil
		}
	}
	return 0, fmt.Errorf("端口 %d..%d 全部被占，无法为 piik 分配监听端口；请用「端口查杀」页确认并释放 %d 或相邻端口后重试",
		defaultPort, defaultPort+portScanLimit-1, defaultPort)
}

// ensureDataDir 托管启动前保证数据目录与日志目录存在（上游对不存在的
// ConfigPath 父目录/LogDir 目标行为未实证，宁可先建空目录也不赌上游）。
// 幂等：MkdirAll 对已存在目录零副作用。
func (s *PiikService) ensureDataDir() error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return fmt.Errorf("创建 piik 托管数据目录失败: %w", err)
	}
	if err := os.MkdirAll(s.logDir, 0o755); err != nil {
		return fmt.Errorf("创建 piik 托管日志目录失败: %w", err)
	}
	return nil
}

// consoleURLOf 界面地址取用：格式归 A 线 instance.URLForPort 单点组装（引擎给
// URL、浏览器调用归 service 的分工线，见 instance 包注释），本层只转发，绝不
// 第二处拼 http://127.0.0.1:%d/。
func consoleURLOf(port int) string {
	return piikinstance.URLForPort(port)
}

// portOf 快照端口的诚实读法：>0 = 本引擎本代/最近一代分配端口（A 线
// Snapshot.Port 单点记账，0 = 从未启动过）；非正值一律钳为零，绝不用它拼出
// "http://127.0.0.1:0/" 这种坏链接。本层不再另存端口副本。
func portOf(snap Snapshot) int {
	if snap.Port > 0 {
		return snap.Port
	}
	return 0
}

// portCandidates 界面定位候选端口：快照携带的分配端口优先，上游默认 8787 兜底
// （external 实例端口不可知——上游无 mutex 也无从问，只能按默认口拨测），去重保序。
func portCandidates(snap Snapshot) []int {
	seen := make(map[int]struct{}, 2)
	out := make([]int, 0, 2)
	for _, p := range []int{portOf(snap), defaultPort} {
		if p <= 0 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// ---------- 控制操作 ----------

// GetStatus 返回实例快照（含引擎记账的端口与机读字段）+ 界面地址 + 托管数据
// 目录账 + 使用版本账本漂移投影（先做一次静止态外部校正，弥补 5s 轮询间隙的
// 即时性）。
//
// 投影纪律：本层只加 listenPort/consoleUrl/dataDir/configPath/logDir/drifted/
// driftNote 六位，
// 机读字段与端口一律由内嵌快照原样展平（同一条广播的两个出口，绝不在这里
// 抄一份改名别名）；上游口令只以 passwordSet 布尔出场，值绝不上前端——A 线
// 解析位即弃明文，类型层就没有承载它的字段。漂移复查对象：设定版本优先，
// 未定时取快照携带的最近自有版本；无版本可查（未安装）时不复查、留空。
func (s *PiikService) GetStatus() (PiikStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return PiikStatus{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	st := PiikStatus{
		Snapshot:   snap,
		ListenPort: servicePort(snap),
		DataDir:    s.dataDir,
		ConfigPath: s.configPath,
		LogDir:     s.logDir,
	}
	// consoleUrl 只对"本层托管的在世实例"给值（前端 running 话术与 noBrowser
	// 兜底链接消费）；stopped/failed 给空串，external 也不给——那个实例的端口
	// 不归本引擎记账，谎报一个可点链接比不给更糟。
	if snap.State == piikinstance.StateRunning {
		st.ConsoleURL = consoleURLOf(st.ListenPort)
	}

	v := s.store.GetActive()
	if v == "" {
		v = snap.Version
	}
	if v != "" {
		st.Drifted, st.DriftNote = s.manager.VerifyLedger(v)
	}
	return st, nil
}

// servicePort 状态面的端口取用（listenPort 与 consoleUrl 共用这一处判定）：
//   - running：引擎记账的本代分配端口（账为 0 属异常态，退回上游默认口，
//     宁可给一个大概率可点的链接也不给 0）；
//   - external：上游默认口——外部实例的真实端口不可知（A 线只按进程名甄别，
//     无 mutex 无 API 可问），给默认口是"通常占着 8787"这一事实的诚实表达，
//     与前端 external 话术同源；
//   - stopped/failed：最近一代端口（引擎账原位保留），从未启动过则 0。
//
// 端口拨测不在这里做（GetStatus 是高频轮询面，绝不在此挂 400ms 网络等待），
// 定位外部实例的现场拨测留在 OpenWindow。
func servicePort(snap Snapshot) int {
	switch snap.State {
	case piikinstance.StateRunning:
		if p := portOf(snap); p != 0 {
			return p
		}
		return defaultPort
	case piikinstance.StateExternal:
		return defaultPort
	default:
		return portOf(snap)
	}
}

// Start 托管启动 piik 服务（与 OpenWindow 严格分工：本动词只负责"服务在跑"，
// 不代开浏览器——前端「启动」走这里，「打开界面」走 OpenWindow）：
//   - external：不越权接管也不并存另起（同名外部实例在场时另开一个服务端，
//     对机主是"我到底在哪个页面上开播"的困惑源），如实回执并给指引；
//   - running：幂等直返，附当前界面地址；
//   - starting：启动临界区，不二次拉起（引擎侧 errBusy"上一代未收口拒启动"
//     是同一纪律的第二道闸）；
//   - stopped/failed：分配端口 → 保证数据目录 → 引擎托管启动（ConfigPath/LogDir
//     随 Options 下传）→ 等就绪。
func (s *PiikService) Start() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case piikinstance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "piik 正在启动中，请稍候"}, nil
	case piikinstance.StateExternal:
		return ControlOutcome{Action: "external", External: true,
			Message: fmt.Sprintf("检测到外部 piik 实例（自行启动，通常占着 %d），托管启动未执行；可直接「打开界面」访问它，或在对方进程内退出后再由 Hanxi 托管", defaultPort)}, nil
	case piikinstance.StateRunning:
		port := servicePort(snap)
		return ControlOutcome{Action: "already-running", Port: port, URL: consoleURLOf(port),
			Message: fmt.Sprintf("piik %s 已在运行，界面 %s", snap.Version, consoleURLOf(port))}, nil
	default:
		port, url, err := s.startOwned()
		if err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "started", Port: port, URL: url,
			Message: fmt.Sprintf("piik 已启动，监听 %d（邀请与开播在页面内进行）", port)}, nil
	}
}

// startOwned 冷启动编排：解析版本 → 分配端口 → 保证托管数据目录 → 引擎拉起
// （JobObject 罩住 piik 及其 cloudflared/piik-capture 子工）→ 等就绪。
// 端口与数据落点随 Options 下传 A 线引擎（Port/ConfigPath/LogDir 冻结面）。
func (s *PiikService) startOwned() (int, string, error) {
	v, exe, err := s.resolveActiveVersion()
	if err != nil {
		return 0, "", err
	}
	port, err := s.allocatePort()
	if err != nil {
		return 0, "", err
	}
	if err := s.ensureDataDir(); err != nil {
		return 0, "", err
	}
	if err := s.engine.Start(piikinstance.Options{
		Version:    v,
		Exe:        exe,
		Detached:   !s.store.GetFollowOnExit(), // "不随 Hanxi 关闭"开关 → 解除 Job 退出联动
		Port:       port,
		ConfigPath: s.configPath,
		LogDir:     s.logDir,
	}); err != nil {
		return 0, "", fmt.Errorf("启动 piik 失败: %w", err)
	}
	// 端口不再在本层另记一笔：Start 成功即由引擎记进 Snapshot.Port（本代分配
	// 端口），就绪等待与后续所有界面链接都读那一处。
	if !s.engine.WaitReady(readyTimeout) {
		// 文案纪律（档案①#11）：piik 无 WebView2 依赖，绝不用窗口托管模板的
		// "请确认已安装 WebView2"措辞；失败原因如实指向进程退出与端口占用。
		if cur := s.engine.Snapshot(); cur.State == piikinstance.StateFailed && cur.Error != "" {
			return 0, "", fmt.Errorf("%s", cur.Error)
		}
		return 0, "", fmt.Errorf("等待 piik 就绪超时（%d 秒）：端口 %d 上的服务没能起来，多为上游进程启动即退（版本目录内 runtime 兄弟项缺失，或端口在试绑后被抢占）；可用「端口查杀」页复核 %d 的占用情况",
			int(readyTimeout/time.Second), port, port)
	}
	if s.store.GetActive() == "" {
		_ = s.store.SetActive(v) // 首次冷启动将实际采用的版本回写为 activeVersion
	}
	// 本方法只管"服务在跑"，绝不在这里顺手开浏览器：机读模式闸掉了上游自拉，
	// 开页面是机主点「打开界面」（OpenWindow）的动作，两个动词不互相越权。
	// 前端 hint 话术"启动后上游会尽力自动打开浏览器"在机读模式下不成立，
	// 已列入联编收口清单（C 线文案对齐）。
	return port, consoleURLOf(port), nil
}

// OpenWindow "打开界面"——托管族 OpenWindow 之名在无窗服务上的语义重定义：
// 把这个 URL 交给系统浏览器（plat.OpenURL），**不**顺带起服务。
//   - running：补开（页面被关掉/上游自动拉起没成功，再点一次即回）；
//   - external：现场拨测候选端口，命中即开并如实标注"非 Hanxi 托管实例"——
//     本层绝不向外部实例下停止/接管指令；外部实例端口不可知（A 线探针只按
//     进程名甄别，上游无 mutex 可问），故候选 = 引擎账 + 默认 8787 去重拨测；
//   - starting：临界区如实回执，不重复动作（引擎 errBusy 是第二道闸）；
//   - stopped/failed：如实拒绝并指向「启动」。
//
// 负裁决（本方法与 ddnsgo OpenConsole 的唯一分歧，C 线前端同口径锁定）：
// **绝不冷启动**。起服务等于在局域网敞开一个分享端口，这个动作只在机主明
// 确点「启动」（Start RPC）时发生；"界面打不开"绝不构成替机主开后台服务的
// 理由。命名沿用 OpenWindow 是族一致（A 线包注释亦写"界面打开归 service 的
// OpenWindow 通道"），语义已按无窗服务重定义，勿据名找窗口。
func (s *PiikService) OpenWindow() (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	s.engine.RefreshExternal()
	snap := s.engine.Snapshot()

	switch snap.State {
	case piikinstance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "piik 正在启动中，请稍候"}, nil

	case piikinstance.StateExternal:
		dial := s.connectProbe
		if dial == nil {
			dial = portConnectable // 装配缺位时退回真实拨测，绝不因 seam 为 nil 而谎报定位失败
		}
		for _, cand := range portCandidates(snap) {
			if dial(cand) {
				url := consoleURLOf(cand)
				if err := s.openURL(url); err != nil {
					return ControlOutcome{}, err
				}
				return ControlOutcome{Action: "external-opened", External: true, Port: cand, URL: url,
					Message: fmt.Sprintf("已在浏览器打开外部 piik 界面 %s（非 Hanxi 托管实例）", url)}, nil
			}
		}
		return ControlOutcome{}, fmt.Errorf("检测到外部 piik 进程，但候选端口（含默认 %d）拨测无响应——可能运行于其他机器或另有网卡，无法定位界面；如需 Hanxi 托管实例，请先退出该进程", defaultPort)

	case piikinstance.StateRunning:
		port := servicePort(snap)
		url := consoleURLOf(port)
		if err := s.openURL(url); err != nil {
			return ControlOutcome{}, err
		}
		return ControlOutcome{Action: "opened", Port: port, URL: url,
			Message: fmt.Sprintf("已在浏览器打开 piik 界面 %s", url)}, nil

	default:
		return ControlOutcome{}, fmt.Errorf("piik 服务未在运行，界面无从打开；请先点「启动」——起服务会在局域网敞开一个分享端口，这个动作只由您明示触发")
	}
}

// openURL 界面打开单点（走 plat.OpenURL；seam 存在仅为单测不真唤浏览器）。
func (s *PiikService) openURL(url string) error {
	open := s.browserOpen
	if open == nil {
		open = s.openInBrowser
	}
	return open(url)
}

func (s *PiikService) openInBrowser(url string) error {
	if err := s.plat.OpenURL(url); err != nil {
		return fmt.Errorf("唤起系统浏览器失败: %w", err)
	}
	return nil
}

// QuitAdvisory 托管退出预告文案（前端"退出"按钮确认框在执行 Quit 前展示）。
// 文案单点收口在后端（gonavi/dbx 同纪律），避免前端各入口话术漂移；
// 状态相关分支只影响"断播"这一句要不要说，不影响 Quit 的行为本身。
//
// 本模块与窗口托管家族最根本的差别：优雅停（写 stdin）是上游官方通道，
// 但对正在看播的观众而言，优雅与强杀没有区别——**优雅停 = 当场断播**。
// 这句话必须说在前面，不能等用户点完退出再让浏览器里的画面变黑。
func (s *PiikService) QuitAdvisory() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	snap := s.engine.Snapshot()
	if snap.State == piikinstance.StateExternal {
		return "当前是外部自行启动的 piik 实例，Hanxi 不会终止它；如需退出请在启动它的那个窗口里按 Ctrl+C 或直接结束该进程。", nil
	}
	// 引擎无 quitting 档（收口相在内部折并入 running），故在跑的判定就是这两态；
	// 优雅停的 6s 预算窗口内前端仍见 running，这是家族词表的既成口径。
	if snap.State == piikinstance.StateRunning || snap.State == piikinstance.StateStarting {
		return "退出托管会关闭 piik 分享服务：正在观看的观众当场断播，已分发的局域网/公网邀请链接随之失效，捕获子进程与隧道进程一并回收。配置与日志留在 Hanxi 数据根，不受退出影响。", nil
	}
	return "退出托管经上游官方优雅通道（stdin）+ 宽限，超时由 JobObject 兜底强杀，并连带回收 cloudflared / piik-capture 子进程；配置与日志留在 Hanxi 数据根，不随退出清除。", nil
}

// Quit 退出引擎托管的 piik（stdin 优雅停 + 6s 宽限（覆盖上游 5s 停机预算）+
// JobObject 兜底，语义见 QuitAdvisory 预告）。external 状态不越权强杀（外部
// 实例不归本引擎管辖，端口也在别人手里）：仅返回人性化指引。
// 回执端口取退出前的快照账（引擎记账的本代分配端口）——说的是"刚刚释放的是
// 哪个口"，退出后该口即可被下一次分配复用。
func (s *PiikService) Quit() (QuitOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QuitOutcome{}, gateErr
	}
	defer release()
	snap := s.engine.Snapshot()
	if snap.State == piikinstance.StateExternal {
		return QuitOutcome{Stopped: false, External: true,
			Message: fmt.Sprintf("当前是外部自行启动的实例（通常在占着 %d），Hanxi 未越权终止；请在启动它的那一侧退出", defaultPort)}, nil
	}
	port := portOf(snap)
	if err := s.engine.Quit(); err != nil {
		return QuitOutcome{}, err
	}
	return QuitOutcome{Stopped: true, Port: port,
		Message: fmt.Sprintf("piik 已退出（优雅停通道收口%s）", portPhrase(port))}, nil
}

// portPhrase 退出回执的端口从句：无端口账（从未启动过）时不硬凑一句"端口 0
// 已释放"，如实省略。
func portPhrase(port int) string {
	if port <= 0 {
		return "，本会话未曾分配端口"
	}
	return fmt.Sprintf("，界面端口 %d 已释放", port)
}

// MetaHints 如实披露账（前端 meta 区消费，一次拉全）。所有话术单点收口在
// 后端（QuitAdvisory 同纪律），前端不得自造第二套口径。六条按风险登记表
// ④ 逐条对位：日更风暴 / 0.0.0.0 暴露 / 公网隧道 / --link 不代管 /
// capture 子进程归属 / 数据留存与卸载明示。
func (s *PiikService) MetaHints() ([]string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return []string{
		fmt.Sprintf("上游是日更级发布节奏，本托管按「钉版本手动追」：远程表只如实列版，绝不自动跟版、绝不弹红点轰炸；当前使用版本由您设定（%s），非最新上游版属正常状态，徽章如实显示不谎报。", s.activeVersionForHint()),
		fmt.Sprintf("piik 监听 0.0.0.0（不是只回环）：同一局域网内任何设备都能访问本机的分享服务与界面（%s）。LAN 邀请链接含本机内网 IP，原样展示给您——这是产品用法不是泄露，但请您知情。", s.portPhraseForHints()),
		"公网邀请链接由上游在页面内拉起 cloudflared 建立 Cloudflare 临时隧道：链路经第三方边缘、地址公网可达，用完请在 piik 页面内关闭邀请。",
		"隧道/公开链接由 piik Web UI 侧发起，Hanxi 不代管：本模块不提供「一键开公网」按钮，不注入隧道参数；Hanxi 只承担把隧道子进程关进 JobObject 的回收义务与状态如实。",
		"捕获子进程 piik-capture 与隧道进程 cloudflared 都是 piik 带起的子工，托管期间整棵树罩在 JobObject 内：Hanxi 侧退出或崩溃由 KILL_ON_JOB_CLOSE 兜底，不留孤儿占端口；开启「随 Hanxi 一起关闭」则连带终止，关闭则整棵树留在原地继续服务。",
		fmt.Sprintf("配置（client.json）与日志统一落在 Hanxi 数据根 %s：跨版本共享，卸载任何托管版本都不删数据（本模块不提供删数据通道）；卸载确认框请把这句话读到再按确认。", s.dataDir),
	}, nil
}

// activeVersionForHint / portPhraseForHints metaHints 取数小助手：账上是什么
// 就说是什么——未设版本时如实说"未指定，启动时自动取最新已装"，绝不编一个
// 版本号进披露账；未在跑时如实说"默认 8787，被占则自动上移"，绝不编一个
// 端口号（端口账在引擎快照里，本层不另存副本）。
func (s *PiikService) activeVersionForHint() string {
	if v := s.store.GetActive(); v != "" {
		return "已钉 " + v
	}
	return "未指定，启动时自动取最新已装"
}

func (s *PiikService) portPhraseForHints() string {
	if p := portOf(s.engine.Snapshot()); p != 0 {
		return fmt.Sprintf("当前端口 %d", p)
	}
	return fmt.Sprintf("默认端口 %d，被占时自动上移", defaultPort)
}

// OpenDir 在资源管理器中打开指定目录（版本管理页"打开位置"按钮；受控数据目录
// 同由前端持 GetStatus.dataDir 走本方法）。收口至 windows.RevealDir：其
// explorer.exe <dir> 语义即"打开目录"，刻意不走 explorer.exe <file> 的"执行"
// 语义（markeron「打开安装目录」按钮的事故教训）。
func (s *PiikService) OpenDir(dir string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return windows.RevealDir(dir)
}

// OpenDataDir 打开 Hanxi 数据根下的 piik 托管数据目录（配置与日志所在）——
// 纯托管下用户想看"我的数据在哪"的直达入口。只读导航，不改写；目录尚未
// 创建（从未托管启动过）时如实报错不静默。
func (s *PiikService) OpenDataDir() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	if _, err := os.Stat(s.dataDir); err != nil {
		return fmt.Errorf("piik 托管数据目录尚未创建（还未托管启动过）: %s", s.dataDir)
	}
	return windows.RevealDir(s.dataDir)
}

// Shutdown（RPC 导出版，纯 void）：取得调用门租约后转发内部 shutdown()，
// 拒绝即早退——Wave 3 口径：void 方法不改签名。前端当前不调用本方法，
// 但它属绑定面，必须经门收口。
func (s *PiikService) Shutdown() {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return
	}
	defer release()
	s.shutdown()
}

// shutdown 模块停用/应用退出：停后台轮询 + 按开关联动终止自有实例。
// 装配布线:Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)。
// 外部实例不受影响（非我方托管）；自有实例在"随 Hanxi 关闭"开启时走强杀
// 通道（不耗宽限），孙进程 cloudflared/piik-capture 由 JobObject 同笼回收；
// 开关关闭时完全不动进程——服务跨 Hanxi 生命周期继续开播正是该开关的意义。
func (s *PiikService) shutdown() {
	s.watchMu.Lock()
	if s.watching {
		close(s.watchStop)
		s.watching = false
	}
	s.watchMu.Unlock()
	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop() // 联动开启才杀；关闭则完全不影响工具（Job 已解除 kill-on-close）
	}
}

// ---------- 版本解析 ----------

// resolveActiveVersion 解析当前应使用的版本：activeVersion 优先，未设定/已失效回退最新已装。
func (s *PiikService) resolveActiveVersion() (string, string, error) {
	if active := s.store.GetActive(); active != "" {
		if exe, err := s.manager.ResolveExe(active); err == nil {
			return active, exe, nil
		}
		// 已设定的版本被卸载/损坏：清空自愈，回退最新已装
		_ = s.store.SetActive("")
	}
	installed, err := s.manager.ListInstalled()
	if err != nil {
		return "", "", err
	}
	if len(installed) == 0 {
		return "", "", fmt.Errorf("尚未安装任何 piik 版本，请先在版本管理下载或导入")
	}
	sort.Slice(installed, func(i, j int) bool {
		return versionCompare(installed[i].Version, installed[j].Version) > 0
	})
	latest := installed[0]
	return latest.Version, latest.ExePath, nil
}

// versionCompare 比较 vX.Y.Z 版本号（a>b 返回 1；相等 0；a<b 返回 -1）。
// 目录名的字典序对 0.10.0/0.9.0 这类多位数段有误，必须数值分段比较。
func versionCompare(a, b string) int {
	// 数值分段比较实现收口至 versioncmp.Compare（先剥 v 前缀归一再逐段委托）。
	return versioncmp.Compare(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}

// ---------- 联动开关与上游直达 ----------

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *PiikService) GetFollowOnExit() (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.store.GetFollowOnExit(), nil
}

// SetFollowOnExit 设定开关（下次启动生效：Detached 在 spawn 时刻定格进 Job，
// 改开关不影响已在跑的实例——托管家族统一口径）。
func (s *PiikService) SetFollowOnExit(b bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.store.SetFollowOnExit(b)
}

// RepositoryURL 上游 GitHub 仓库官方地址（页面展示与复制）——地址单点在 A 线
// version.RepoURL()（repoOwner/repoName 与其 API 链同源），本层只转发，
// 避免"下载指向的仓库"与"页面展示的仓库"两处各写一份。
func (s *PiikService) RepositoryURL() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	return piikversion.RepoURL(), nil
}

// OpenRepository 用默认浏览器打开上游仓库页面（地址同样取自 version.RepoURL()
// 单点，与 RepositoryURL 不可能指向两处）。
func (s *PiikService) OpenRepository() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.openURL(piikversion.RepoURL())
}
