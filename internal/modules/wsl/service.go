package wsl

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/modules/wsl/readiness"
	"hanxi/internal/modules/wsl/releases"
	"hanxi/internal/settings"
)

// EventReadiness 流式体检事件名（app.go 中注册载荷类型）。
const EventReadiness = "wsl:readiness"

// urlOpener 打开固定官方地址所需的最小平台能力（与 envcheck 同款解耦）。
type urlOpener interface {
	OpenURL(url string) error
}

// WslService Wails 绑定服务：WSL 就绪体检（同步 + 流式）、官方版本管理、
// 白名单提权操作与正规卸载、发行版实例管理控制台（列表 + 启停/导出/迁移等）。
// 探测/外呼函数一律以字段注入，单测替换后即可离线断言。
type WslService struct {
	opener        urlOpener
	probe         func(context.Context) (readiness.ProbeResult, error)
	netProbe      func(context.Context) (api string, gh string)
	wslVersion    func(context.Context) string
	wslDistros    func(context.Context) []readiness.Distro
	onlineDistros func(context.Context) ([]readiness.DistroOption, error)
	overview      func(localVersion string) (releases.Overview, error)
	elevProc      func(context.Context, string, ...string) (OperationOutcome, error)
	localPS       func(context.Context, string) (string, error)
	emit          func(name string, payload any)
	// runWsl 非提权 wsl.exe 通道（解码已收敛在 readiness.RunWsl）；
	// startTerm 唤终端、openFolder 唤资源管理器外呼。
	// 发行版管理面单测替换这三路即可离线断言。
	runWsl     func(context.Context, ...string) (string, error)
	startTerm  func(context.Context, string) error
	openFolder func(context.Context, string) error
	// runWslIn 带 stdin 的 wsl.exe 通道（wsl.conf 写回等经管道改文件场景）。
	runWslIn func(context.Context, string, ...string) (string, error)

	// lastProbe 缓存最近一次成功探针：卸载取 MSI ProductCode 免重复查询。
	mu        sync.Mutex
	lastProbe *readiness.ProbeResult
	// readinessPublish 串行化轮次切换与事件发布，消除“旧轮已检查、尚未 Emit”窗口。
	readinessPublish sync.Mutex
	// readinessCancel 终止上一次仍在跑的流式体检（重新体检防串扰）。
	readinessCancel context.CancelFunc
	readinessGen    uint64
	// 下载单飞状态与本会话落盘登记（RevealDownload 只认这里的路径）。
	dlBusy  bool
	dlPaths map[string]string
	// 发行版管理面：每发行版单飞闸（小写名 → 操作名）、全局重操作计数
	// （迁移会 --shutdown 打停全部实例，与任何单发行版操作互斥）、
	// 导出工件登记（ListDistroExports/RevealDistroExport 只认这里）。
	distroOps     map[string]string
	heavyOps      int
	exportRecords map[string]ExportRecord
	// 端口转发规则持久化（<DataDir>/wsl-portproxy.json）。
	ppPath    string
	ppPending bool // 规则有增删改但尚未点「应用」挂到系统
	// 安装落位偏好持久化（<DataDir>/wsl-install-pref.json）：
	// 取代前端 localStorage 的「空串粘滞、错路径粘 C、跨包各自为政」三坑，
	// 详见 installdir.go 与 docs/TROUBLESHOOTING.md。
	installPrefPath string
	// 长操作取消通道（均以 mu 守护）：stage 同时是"允许取消"的判据——
	// 克隆/下载全程可停，瘦身只在备份与 fstrim 段可停，
	// 进入数据盘处理/重建段后拒绝取消（半途而废比慢更糟）。
	cloneOps  map[string]longOpHandle // key: 源名小写
	compactOp *longOpHandle
	dlOp      *longOpHandle
}

// longOpHandle 后台长操作的取消句柄与当前阶段。
type longOpHandle struct {
	cancel context.CancelFunc
	stage  string
}

func NewWslService(opener urlOpener, paths *settings.Paths) *WslService {
	rulesPath := ""
	installPref := ""
	if paths != nil {
		rulesPath = filepath.Join(paths.DataDir(), "wsl-portproxy.json")
		installPref = filepath.Join(paths.DataDir(), "wsl-install-pref.json")
	}
	return &WslService{
		opener:          opener,
		ppPath:          rulesPath,
		installPrefPath: installPref,
		probe:           readiness.Probe,
		netProbe:        readiness.ProbeNetwork,
		wslVersion:      readiness.Version,
		wslDistros:      readiness.Distros,
		onlineDistros:   readiness.OnlineDistros,
		overview:        releases.OverviewFor,
		elevProc:        runElevatedProcess,
		localPS:         runLocalPS,
		emit:            emitEvent,
		runWsl:          readiness.RunWsl,
		runWslIn:        readiness.RunWslWithStdin,
		startTerm:       startTerminalSession,
		openFolder:      openDistroFolderExplorer,
		dlPaths:         map[string]string{},
		distroOps:       map[string]string{},
		exportRecords:   map[string]ExportRecord{},
		cloneOps:        map[string]longOpHandle{},
	}
}

// emitEvent 经 Wails 事件总线推送；无 app 实例（单测/装配前）静默跳过。
func emitEvent(name string, payload any) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit(name, payload)
	}
}

// ---- 体检：流式分相 ----
// 曾有过同步整体版 GetReadiness：首屏与操作后复查全走 StartReadiness 流式通道，
// 同步版从未被前端接线，死绑定已摘除（避免绑定面长期挂着无人维护的平行取数路径）。

// StartReadiness 异步启动流式体检并立即返回：
// 三源并发、先到先推（system / wsl / net 阶段逐项落位），全齐后 done 阶段整体收口。
// 重复调用会静默终止上一轮，永不双跑串扰。
func (s *WslService) StartReadiness() error {
	s.readinessPublish.Lock()
	defer s.readinessPublish.Unlock()
	s.mu.Lock()
	if s.readinessCancel != nil {
		s.readinessCancel()
	}
	s.readinessGen++
	gen := s.readinessGen
	ctx, cancel := context.WithCancel(context.Background())
	s.readinessCancel = cancel
	s.mu.Unlock()

	go func() {
		defer cancel()
		s.streamReadiness(ctx, gen)
	}()
	return nil
}

func (s *WslService) streamReadiness(ctx context.Context, gen uint64) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	var (
		p        readiness.ProbeResult
		probeErr error
		api, gh  string
		version  string
		distros  []readiness.Distro
		wg       sync.WaitGroup
	)
	wg.Go(func() {
		p, probeErr = s.probe(ctx)
		if probeErr == nil && s.isCurrentReadiness(gen, ctx) {
			s.setLastProbeIfCurrent(gen, p)
			s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "system", Items: readiness.SystemItems(p)})
		}
	})
	wg.Go(func() {
		version = s.wslVersion(ctx)
		s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "wsl", Items: []readiness.CheckItem{readiness.RuntimeItem(version)}})
		distros = s.wslDistros(ctx)
	})
	wg.Go(func() {
		api, gh = s.netProbe(ctx)
		s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "net", Items: []readiness.CheckItem{readiness.NetworkItem(api, gh)}})
	})
	wg.Wait()

	if !s.isCurrentReadiness(gen, ctx) {
		return
	}
	if probeErr != nil {
		s.emitIfCurrent(gen, ctx, ReadinessUpdate{
			Stage: "error",
			Error: fmt.Sprintf("WSL 就绪探针执行失败（PowerShell 不可用或被安全策略拦截？）: %v", probeErr),
		})
		return
	}
	p.APIGitHub, p.GitHub = api, gh
	report := readiness.Evaluate(p, version, distros, time.Now().Format("2006-01-02 15:04:05"))
	s.emitIfCurrent(gen, ctx, ReadinessUpdate{Stage: "done", Report: &report})
}

func (s *WslService) isCurrentReadiness(gen uint64, ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readinessGen == gen
}

func (s *WslService) emitIfCurrent(gen uint64, ctx context.Context, update ReadinessUpdate) {
	s.readinessPublish.Lock()
	defer s.readinessPublish.Unlock()
	if !s.isCurrentReadiness(gen, ctx) {
		return
	}
	s.emit(EventReadiness, update)
}

func (s *WslService) setLastProbeIfCurrent(gen uint64, p readiness.ProbeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readinessGen == gen {
		s.lastProbe = &p
	}
}

func (s *WslService) setLastProbe(p readiness.ProbeResult) {
	s.mu.Lock()
	s.lastProbe = &p
	s.mu.Unlock()
}

// ListOnlineDistros 返回官方在线可安装发行版清单（需 wsl.exe 已具备 --list --online 能力）。
func (s *WslService) ListOnlineDistros() ([]readiness.DistroOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.onlineDistros(ctx)
}

// GetReleases 拉取 microsoft/WSL 官方发布列表并判定与本机版本的关系。
// API 被拦（如 api.github.com 对部分云出口 IP 区域封锁 403）时自动降级
// Atom 订阅源（github.com 域通常畅通），载荷 fallback 标记供前端如实标注。
func (s *WslService) GetReleases() (releases.Overview, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	local := ""
	if v := s.wslVersion(ctx); v != "" {
		local = v
	}
	return s.overview(local)
}

// ---- 白名单提权操作：参数全部后端固定，前端零拼接面 ----

// InstallWsl 一键开启：只装 WSL 本体与虚拟机平台，绝不自动装发行版
// （--install 捆绑模式会把默认 Ubuntu 一起塞进来，与"用户手动挑系统"的
// 产品语义冲突，故本模块操作面固定走 --no-distribution）。
func (s *WslService) InstallWsl() (OperationOutcome, error) {
	return s.elevateWsl("--install", "--no-distribution")
}

// UpdateWsl 经默认通道更新 WSL 本体。
func (s *WslService) UpdateWsl() (OperationOutcome, error) {
	return s.elevateWsl("--update")
}

// UpdateWslWebDownload 强制 GitHub 直连更新（商店通道不通时）。
func (s *WslService) UpdateWslWebDownload() (OperationOutcome, error) {
	return s.elevateWsl("--update", "--web-download")
}

// SetDefaultVersion2 将新装发行版默认设为 WSL2。
func (s *WslService) SetDefaultVersion2() (OperationOutcome, error) {
	return s.elevateWsl("--set-default-version", "2")
}

func (s *WslService) elevateWsl(args ...string) (OperationOutcome, error) {
	return s.elevProc(context.Background(), "wsl.exe", args...)
}

// InstallDistro 安装指定在线发行版。ID 不接受裸字符串直传命令——
// 先实时重取在线清单做白名单校验，杜绝本模块被当作任意参数执行面。
// 白名单之后、提权之前还有一道虚拟化装载预检：默认版本为 2 的前提下，
// 虚拟机平台未启用或"已启用但欠重启"时 WSL2 根本起不了虚拟机（实机
// wsl --status 证词："WSL2 无法启动，因为此计算机上未启用虚拟化"），
// 此时点击安装只是白白弹出 UAC 再失败——提前拦下并指路，探针自身不可得时放行（失败由退出码通道如实上报）。
func (s *WslService) InstallDistro(id string) (OperationOutcome, error) {
	id = strings.TrimSpace(id)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if gate := s.virtualizationGate(ctx); gate != "" {
		return OperationOutcome{Message: gate}, nil
	}
	list, err := s.onlineDistros(ctx)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("无法校验发行版清单，安装中止: %w", err)
	}
	allowed := false
	for _, o := range list {
		if o.ID == id {
			allowed = true
			break
		}
	}
	if !allowed {
		return OperationOutcome{}, fmt.Errorf("发行版 %q 不在官方在线清单中，已拒绝执行", id)
	}
	return s.elevateWsl("--install", "-d", id)
}

// manageMoveMinVersion 自动落位能力（wsl --manage --move）的最低版本：
// 上游 2.4.4 发布说明引入该子命令（github.com/microsoft/WSL/releases/tag/2.4.4，
// 文档 learn.microsoft.com/windows/wsl/virtual-disk 同口径），更老版本直接报
// "unknown option"（上游 issues/11762 实证）。
var manageMoveMinVersion = []int{2, 4, 4}

// 装完即迁链的分段哨兵退出码：安装段与迁移段分开归因——迁移段失败时发行版
// 已经躺在系统默认位置（通常 C 盘），必须点名指路补救而非笼统报错
// （#36/#37 退出码传播红线同一族纪律）。
const (
	installStageExit = 80
	moveStageExit    = 81
)

// InstallDistroTo 安装在线发行版并按需落位：location 为空走系统默认（现状，
// 通常落 C 盘）；指定位置则在同一提权链里连做 wsl --install -d、wsl --shutdown、
// wsl --manage --move，全程一次 UAC、一个提权窗口看进度——wsl --install 本身
// 不接受目标目录参数，"装完即迁"是唯一正规通道（与 MoveDistro 同款停机+重试节律）。
// 白名单/虚拟化预检两道闸门与 InstallDistro 同源复用；location 按基目录语义
// 使用（末级非发行版名时自动追加同名子目录），目标经 moveTarget 的绝对路径/
// 盘符存在/非法字符/目录空性把关。
// 三道落位防线（根治"说好的 D 盘结果还在 C"观感）：迁移能力版本闸门（本机 WSL
// 过老则事前显式抉择，绝不装完才静默失败）；分段退出码归因（迁移段失败点名
// "已装在 C、无需重装、点🧭迁移补救"）；成功后注册表 BasePath 复验（MoveDistro
// 同款，退出码 0 不等于真落位）。
func (s *WslService) InstallDistroTo(id, location string) (OperationOutcome, error) {
	loc := strings.TrimSpace(location)
	if loc == "" {
		return s.InstallDistro(id)
	}
	id = strings.TrimSpace(id)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if gate := s.virtualizationGate(ctx); gate != "" {
		return OperationOutcome{Message: gate}, nil
	}
	list, err := s.onlineDistros(ctx)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("无法校验发行版清单，安装中止: %w", err)
	}
	if !slices.ContainsFunc(list, func(o readiness.DistroOption) bool { return o.ID == id }) {
		return OperationOutcome{}, fmt.Errorf("发行版 %q 不在官方在线清单中，已拒绝执行", id)
	}
	// 版本闸门前置于一切副作用：过老 WSL 上"装完即迁"注定半路夭折，
	// 与其装到 C 再静默失败，不如让用户事前显式抉择。
	if v := s.wslVersion(ctx); !versionAtLeast(v, manageMoveMinVersion) {
		return OperationOutcome{Message: fmt.Sprintf(
			"自动落位需要 WSL %s+ 的迁移能力（当前 %s）：请到「🧩 本体版本」页升级 WSL 本体后再安装；"+
				"或把「安装基目录」留空并接受系统默认位置（通常在 C 盘）。",
			strings.Join(intsToStr(manageMoveMinVersion), "."), orUnknown(v))}, nil
	}
	// 幂等兜底：发行版已在册（典型为上次"装完即迁"迁移段失败后的重试）则跳过
	// 安装段、只补落位迁移——避免对已注册实例重跑 --install 的非零退出把
	// 整链掐死在第一步，形成"永远搬不动"的死循环。
	registered := false
	if names, err := s.quietNames(ctx); err == nil {
		registered = slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, id) })
	}
	currentBase := ""
	if registered {
		if store, err := s.lxss(ctx); err == nil {
			if e, found := store[strings.ToLower(id)]; found {
				currentBase = e.BasePath
			}
		}
	}
	// location 按"基目录"语义处理：末级不是发行版名则自动追加同名子目录
	//（D:\wsl → D:\wsl\Ubuntu），与 ImportDistro 同源复用 underDir。
	cleanTarget := underDir(loc, id)
	// 已在目标位置＝善后完成而非违规操作，早退在 moveTarget 拒绝"相同位置"之前。
	if registered && currentBase != "" && strings.EqualFold(filepath.Clean(currentBase), filepath.Clean(cleanTarget)) {
		return OperationOutcome{Success: true, Message: fmt.Sprintf("%s 已在目标位置 %s，无需再动", id, filepath.Clean(currentBase))}, nil
	}
	// 未在册时 currentBase 留空：跳过"与当前位置相同/嵌套"两项检查。
	clean, err := moveTarget(cleanTarget, currentBase)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("安装位置不合规: %w", err)
	}
	// 安装与落位共用一条提权 PowerShell：install 非零退出即中止（不留下"装到一半
	// 又搬动"的乱局）；落位段沿用 MoveDistro 的 shutdown+3s+至多 5 次重试节律，
	// 两段各用哨兵退出码分开归因。
	// ctx 用 opTimeout（数十 GB 下载+落位远超 60s 白名单窗口——白名单校验已完成）。
	mctx, mcancel := context.WithTimeout(context.Background(), opTimeout)
	defer mcancel()
	inner := fmt.Sprintf(
		"$tries = 0; while ($true) { $tries++; wsl --manage %s --move %s; "+
			"if ($LASTEXITCODE -eq 0) { exit 0 }; if ($tries -ge 5) { exit %d }; Start-Sleep -Seconds 3 }",
		psQuote(id), psQuote(clean), moveStageExit,
	)
	if !registered {
		inner = fmt.Sprintf(
			"wsl --install -d %s; if ($LASTEXITCODE -ne 0) { exit %d }; wsl --shutdown; Start-Sleep -Seconds 3; %s",
			psQuote(id), installStageExit, inner)
	}
	out, err := s.elevProc(mctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inner)
	if err != nil {
		if exitCode(err) == moveStageExit {
			stage := fmt.Sprintf("安装 %s", id)
			if registered {
				stage = "落位迁移 " + id
			}
			return OperationOutcome{Message: fmt.Sprintf(
				"%s 的数据已就位，但迁移到 %s 失败：%v\n该发行版无需重装——到「本机发行版」列表对它点「🧭 迁移」即可手动完成落位（此刻它通常还在系统默认位置，一般在 C 盘）。",
				stage, clean, err)}, nil
		}
		return OperationOutcome{}, err
	}
	if !out.Success {
		return out, nil // UAC 取消等：如实回执不报错
	}
	// 成功复验（MoveDistro 同款）：注册表 BasePath 真指向目标才敢宣布落位完成。
	vctx, vcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer vcancel()
	if store, serr := s.lxss(vctx); serr == nil {
		if e, found := store[strings.ToLower(id)]; found {
			if !strings.EqualFold(filepath.Clean(e.BasePath), clean) {
				return OperationOutcome{Success: true, Message: fmt.Sprintf(
					"%s 安装链执行完毕，但注册表位置（%s）与目标（%s）不一致——请重新复采核实，未落位就用「🧭 迁移」补一次",
					id, e.BasePath, clean)}, nil
			}
			return OperationOutcome{Success: true, Message: fmt.Sprintf("%s 已安装并落位在 %s", id, clean)}, nil
		}
	}
	return out, nil
}

// virtualizationGate 发行版安装的硬前提预检；返回空串表示放行。
func (s *WslService) virtualizationGate(ctx context.Context) string {
	p, err := s.probe(ctx)
	if err != nil {
		return ""
	}
	switch {
	case !p.FeatureVM:
		return "「虚拟机平台」尚未启用——WSL2 无法承载发行版。请先执行「🚀 一键开启」（或「▶️ 开启虚拟机平台」）并重启，再回来挑选发行版。"
	case p.RebootPending:
		return "「虚拟机平台」已启用但还没重启生效——WSL2 此刻起不了虚拟机（wsl --status 原话：未启用虚拟化），现在装发行版注定失败。重启一次、回来重新体检即可安装。"
	}
	return ""
}
