package instance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"hanxi/internal/platform"
	sup "hanxi/packages/go/supervisor"
)

// State 引擎状态机：stopped → starting → running → (stopped | failed | external)
type State string

const (
	StateStopped  State = "stopped"  // 未运行 / 手动停止 / 外部实例也已退出
	StateStarting State = "starting" // 自有进程创建中（极短窗口）
	StateRunning  State = "running"  // 自有 Job 托管主实例运行中
	StateFailed   State = "failed"   // 启动失败 / 异常退出
	StateExternal State = "external" // 外部用户自启的 TranslucentTB 主实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"首启欢迎窗口挂起/XAML 线程关闭缓慢"等场景。
// 包级变量仅为单测可压缩等待时长，生产值保持 2s。
var closeGracePeriod = 2 * time.Second

// manualStopWording 内核手动停止的收口文案；translucenttb 既有快照口径在
// stopped 态不带文案（Error 为空），映射时如实还原。
const manualStopWording = "已手动停止"

// Snapshot 引擎状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	Version   string    `json:"version"`
	State     State     `json:"state"`
	PID       uint32    `json:"pid"`
	ExitCode  int       `json:"exitCode"`
	Error     string    `json:"error"`
	External  bool      `json:"external"` // state==external 时为 true
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析版本后填充。
type StartOptions struct {
	Version string // 绑定版本 YYYY.N
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // TranslucentTB.exe 绝对路径（版本隔离目录内）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("TranslucentTB.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine TranslucentTB 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 TranslucentTB 专属账目（退出码/停止时刻）与状态信使、WM_CLOSE 优雅退出钩子。
type Engine struct {
	mu        sync.Mutex
	exitCode  int       // 自有实例最近一次退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe TBProbe
	cb    Callbacks

	spawnMessenger func(exe string) error // 状态信使拉起接缝（默认真实 spawn，测试注入）

	// specArgs 真机冒烟专用注入缝：生产 TranslucentTB 恒无参拉起（nil），
	// 测试注入让替身进程（cmd.exe）在精简 stdin 环境下也能稳定存活。
	specArgs []string
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe TBProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向托盘消息窗口投递 WM_CLOSE
	// （上游处理器 Exit()：保存 settings.json + PostQuitMessage 自退），
	// 超时由内核 JobObject 强杀兜底（settings.json 临时文件 + ReplaceFile
	// 原子落盘，进程级终止不会写坏配置）。
	e.sup.SetQuitHook(func(context.Context) error {
		postClose()
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；
// 工作目录锁定到 exe 所在目录由内核默认保证——settings.json 按 exe 同目录
// 解析依赖此）。TranslucentTB 无后台启动 CLI，唯一启动语义即无参拉起（首启弹
// 欢迎授权窗口，常规态直接驻托盘）。互斥体先于一切 UI 创建（main.cpp 实证），
// 就绪判定留在 service 层经 WaitReady 完成。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类兜底
// （ReadyTimeout=0 即 markeron 同款冷启动语义——我方进程信使化自退时，
// 互斥体仍被外部主实例持有即判 external 接管）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// TranslucentTB 是 GUI 子系统程序，无需隐藏控制台窗口，也不做任何窗口干预
	// （零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// ResetState 经单实例协议重设运行实例的任务栏动态状态：拉起同路径"信使"进程，
// 上游检测互斥体被持有 → 向主实例消息窗口投递 TTB_NewInstanceStarted → 主实例
// 执行 ResetState(true) 并弹"已在运行"气泡。这就是上游托盘菜单 "Reset dynamic
// state" 的对外 IPC 等价物（TranslucentTB 无设置窗口可唤，别按 ccswitch 的
// OpenWindow 语义理解本方法——二次拉起不产生"窗口"，只重放任务栏配置）。
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine，见 messenger.go）。
func (e *Engine) ResetState(exe string) error {
	if err := e.spawnMessenger(exe); err != nil {
		return err
	}
	e.emit(e.Snapshot())
	return nil
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 投递 WM_CLOSE
// → 宽限 closeGracePeriod 内等进程自然收口（上游 Exit() 保存 settings.json 后
// PostQuitMessage，主循环收尾自退）→ 超时 JobObject 强杀兜底（首启欢迎窗挂起等
// 边缘态）。幂等；external 态按既有契约映射为无操作成功。
func (e *Engine) Quit() error {
	return e.stopWithGrace(closeGracePeriod)
}

// Stop 立即强杀自有实例（幂等）。与 Quit 的差别：不投 WM_CLOSE、不耗宽限，
// 直接 JobObject 终止（应用退出时 Shutdown 通道用，无需等待动画）。
func (e *Engine) Stop() error {
	return e.stopWithGrace(0)
}

func (e *Engine) stopWithGrace(grace time.Duration) error {
	if err := e.sup.Stop(grace); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil // external 状态不在管辖范围内：指引文案由 service 层给出
		}
		return err
	}
	return nil
}

// RefreshExternal 探测命名互斥体校正 external/stopped 状态（委托内核）。
// 仅对静止态生效：running/starting/stopping 时探测到的正是自己，会误导状态机。
func (e *Engine) RefreshExternal() {
	e.sup.RefreshExternal()
}

// Snapshot 返回当前状态快照。
func (e *Engine) Snapshot() Snapshot {
	outer := e.sup.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked(outer)
}

// Exe 返回当前自有实例的可执行路径（非 running/starting 时为空串）。
func (e *Engine) Exe() string {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning && s.State != sup.StateStarting {
		return ""
	}
	return s.Exe
}

// WaitReady 阻塞等待 TranslucentTB 实例就绪（单实例互斥体出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForReady(timeout)
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ---------- 内核 → translucenttb 形状映射 ----------

// onSupState 内核状态广播 → 映射为本包 Snapshot 后转发（回调在内核锁外执行）。
func (e *Engine) onSupState(s sup.Snapshot) {
	var snap Snapshot
	e.mu.Lock()
	snap = e.snapshotLocked(s)
	e.mu.Unlock()
	e.emit(snap)
}

// snapshotLocked 前置条件：已持 e.mu。
func (e *Engine) snapshotLocked(s sup.Snapshot) Snapshot {
	switch s.State {
	case sup.StateStopped, sup.StateFailed:
		if e.stoppedAt.IsZero() {
			e.stoppedAt = time.Now()
		}
	}
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			e.exitCode = code
		}
	}
	return Snapshot{
		Version:   s.Version,
		State:     mapState(s.State),
		PID:       s.PID,
		ExitCode:  e.exitCode,
		Error:     mapErrorMessage(s),
		External:  s.State == sup.StateExternal,
		StartedAt: s.Since,
		StoppedAt: e.stoppedAt,
	}
}

// mapState 状态词表映射：
//
//	supervisor stopped  → stopped
//	supervisor starting → starting
//	supervisor running  → running
//	supervisor stopping → running（translucenttb 既有词表无 stopping：终止窗口对前端
//	                      保持运行语义，收口后由 stopped/failed 终态广播纠正）
//	supervisor external → external
//	supervisor failed   → failed
func mapState(s sup.State) State {
	switch s {
	case sup.StateStarting:
		return StateStarting
	case sup.StateRunning, sup.StateStopping:
		return StateRunning
	case sup.StateExternal:
		return StateExternal
	case sup.StateFailed:
		return StateFailed
	default:
		return StateStopped
	}
}

// kernelAbnormalExitRe 匹配内核异常退出文案中的退出码。措辞耦合自
// supervisor.wait 的分类消息"托管进程异常退出（退出码 %d）"——内核文案变更时
// 本处回退为透传 Error（ExitCode 保持账目值），不会崩溃，仅少一层改写。
var kernelAbnormalExitRe = regexp.MustCompile(`托管进程异常退出（退出码 (-?\d+)）`)

// exitCodeFromKernelMessage 从内核异常退出文案中提取退出码。
func exitCodeFromKernelMessage(msg string) (int, bool) {
	g := kernelAbnormalExitRe.FindStringSubmatch(msg)
	if g == nil {
		return 0, false
	}
	var code int
	if _, err := fmt.Sscanf(g[1], "%d", &code); err != nil {
		return 0, false
	}
	return code, true
}

// mapErrorMessage 还原 translucenttb 既有失败文案：
//   - 内核异常退出消息改回本引擎既有话术（按退出码分档，见 abnormalExitWording）；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return abnormalExitWording(code, s.Version)
		}
	}
	if s.State == sup.StateStopped && s.Error == manualStopWording {
		return ""
	}
	return s.Error
}

// abnormalExitWording 异常退出码 → 用户话术（分档）。
//
// 2026-09-22 真机教训：旧话术对一切退出码统一预告"欢迎窗/框架包"两成因，
// 而实测 0xC0000005（访问违例）两者皆非——拒绝许可走退码 0，缺框架包另弹
// 「缺少依赖」——统一话术把机主引向错误排查方向。现按码分档：
//   - 0xC0000005：明说访问违例并给"重启→降级"两步鉴别法（首起断言过
//     "2026.2 上游回归"，机主证词"同版本此前正常"削弱之——同字节、同机器、
//     态变了，explorer 里旧注入 DLL 与重装落盘的新 DLL 混态亦可致此，
//     故话术给可判别的动作序列而非归罪单一成因）；
//   - 其他码：保留既有"欢迎窗/框架包"预告（两者仍是真高发起因）。
func abnormalExitWording(code int, version string) string {
	const avCode = 0xC0000005
	if uint32(code) == avCode {
		guide := "先重启一次电脑再启动（排除资源管理器里旧组件残留）；仍崩则到版本列表安装 2026.1 或 2025.1 并设为使用版本鉴别：旧版能跑=本版本构建问题（等上游修，留好 %LOCALAPPDATA%\\CrashDumps 转储可报 issue）；多版全崩而商店版正常=本机环境与未打包路径冲突——首查虚拟显示器（ToDesk/向日葵/GameViewer 等远程工具注入的虚拟显卡会产生\"默认监视器\"空壳，实测崩在此处，见踩坑 #85），次查近期新装的任务栏/外壳类软件"
		if strings.HasPrefix(version, "2026.2") {
			return fmt.Sprintf("TranslucentTB 异常退出（退出码 %d / 0x%08X），访问违例，非许可/依赖问题。%s", uint32(code), uint32(code), guide)
		}
		return fmt.Sprintf("TranslucentTB 异常退出（退出码 %d / 0x%08X），原生访问违例——不是关闭欢迎窗的退出路径（那属退码 0 正常退出），缺框架包也会另弹明确提示。%s", uint32(code), uint32(code), guide)
	}
	return fmt.Sprintf("TranslucentTB 异常退出（退出码 %d）。若刚关闭了首次启动的欢迎授权窗口，属上游正常退出路径（未同意许可），重新启动即可再次进入欢迎流程；否则便携版要求 Windows 11 且依赖系统已装的 WinUI 2.8 / VCLibs 框架包——弹过「缺少依赖」提示时请先补装框架包或改用 Store 版", code)
}

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// supProbe 把 TBProbe（命名互斥体存在性）适配为内核统一探针契约。
// 互斥体探测为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错），
// 因此不产出 ProcInfo（外部实例归属只认 running 事实，PID 无从取得，沿用原口径）。
type supProbe struct{ p TBProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsRunning(), nil, nil
}
