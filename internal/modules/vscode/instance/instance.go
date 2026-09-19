// Package instance 实现 VS Code 实例运行引擎（Wave 4 内核委托形态；便携版 / 安装版
// 各持一台 supervisor.Engine，互不干扰）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 VS Code 领域适配：
//   - 分治探针（安装版 OpenMutex("vscode") / 便携版镜像路径前缀枚举）经
//     supProbe 形状适配为内核统一探针契约（见 prober/probe_windows）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 Form/ExitCode/StoppedAt 拼回
//     既有事件契约（前端与 wails 事件载荷零漂移）；
//   - "唤窗信使"（无参二次拉起经 Electron 单实例锁转发唤/开主窗口，见
//     messenger.go）：不属进程治理，不经内核 Engine。
//
// VS Code 上游实例模型（Electron，src/vs/code/electron-main/app.ts 实证）：
//   - 单实例锁 requestSingleInstanceLock 按 user-data 目录分实例组：二次无参拉起
//     即"信使"——转发唤醒既有实例窗口后自退（markeron 先例，信使不 Wait 不进 Job）；
//   - 便携版（data\ 自包含）与安装版（%APPDATA%\Code）user-data 不同 → 实例组
//     天然隔离，两形态可同时运行、信使各唤各的；
//   - 命名互斥体 "vscode" 仅 isInnoSetupInstall()（安装版）时创建——探测通道
//     据此分治：安装版 OpenMutex("vscode")（与用户日常实例同组语义一致），
//     便携版无互斥体可用，走 Code.exe 进程镜像路径前缀匹配托管目录。
//   - 关窗即退（无托盘驻留），因此不设空闲自动退出：用户关窗进程自退，
//     窗口开着说明用户正在编辑，3 分钟强退编辑器是反需求。
//
// VS Code 由引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 Code.exe
// 及其全部 Electron 子进程（GPU/扩展宿主等），杜绝孤儿驻留。
// 托管默认 Detached（全托管统一口径）：不随 Hanxi 关闭，由 store 开关联动。
//
// 本包零框架依赖，便于单元测试。
package instance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	StateExternal State = "external" // 外部自行启动的实例（非本引擎托管）
)

// closeGracePeriod Quit 的 WM_CLOSE 优雅退出宽限：超时则 JobObject 强杀兜底。
// 覆盖"模态对话框挂住窗口流程"的场（未保存提示等由用户处理后自然退出，
// 无人处理宽限后强杀——Electron hot exit 已备份用户缓冲，无数据损失）。
// 包级变量仅为单测可压缩等待时长，生产值保持 5s（编辑器弹确认框的概率高于
// tauri 工具，给真人反应窗口）。
var closeGracePeriod = 5 * time.Second

// manualStopWording 内核手动停止的收口文案；vscode 既有快照口径在 stopped
// 态不带文案（Error 为空），映射时如实还原。
const manualStopWording = "已手动停止"

// Snapshot 引擎状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	Form      string    `json:"form"` // portable / installer（service 层填充归属）
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
	Version string // 绑定版本（1.136.1 或安装版版本号）
	Form    string // portable / installer
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
	Exe      string // Code.exe 绝对路径（便携版在版本隔离目录根；安装版在注册表安装目录）
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("Code.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine VS Code 单形态运行引擎（便携版与安装版各持一台，互不干扰）：
// 组合内核 supervisor.Engine，本层持有 VS Code 专属账目
// （形态归属、退出码/停止时刻）与信使唤窗、WM_CLOSE 优雅退出钩子。
type Engine struct {
	mu        sync.Mutex
	form      string    // 最近一次 Start 声明的形态（service 层广播时权威覆写，此处保持快照形状）
	exitCode  int       // 自有实例最近一次退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe Probe
	cb    Callbacks

	spawnMessenger func(exe string) error // 信使拉起接缝（默认真实 spawn，测试注入）

	// specArgs 真机冒烟专用注入缝：生产 VS Code 恒无参拉起（nil），
	// 测试注入让替身进程（cmd.exe）在精简 stdin 环境下也能稳定存活。
	specArgs []string
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe Probe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	// 优雅退出通道：Quit 的 grace 窗口内向自有 PID 的可见顶层窗口投递 WM_CLOSE
	// （VS Code 关最后一个窗口即退出主进程，未保存内容有 hot exit 备份兜底），
	// 超时由内核 JobObject 强杀兜底（连同全部 Electron 子进程一并终止）。
	e.sup.SetQuitHook(func(context.Context) error {
		if pid := e.sup.Snapshot().PID; pid != 0 {
			postCloseByPID(pid)
		}
		return nil
	})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running；工作目录
// 锁定到 exe 所在目录由内核默认保证——便携版 data\ 按 exe 位置解析依赖此）。
// VS Code 无后台启动 CLI，唯一启动语义即"无参拉起 → 主窗口显示"。
// 本方法不做存活探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类兜底
// （ReadyTimeout=0 即 markeron 同款冷启动语义——我方进程信使化自退时，
// 探测信号仍在即判 external 接管）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.form = opts.Form
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：VS Code 是 GUI 子系统程序，既不产生
	// 控制台窗口，且需保留其原版行为（零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// OpenWindow 完成单实例协议的窗口唤起：拉起同路径第二个 VS Code 实例充当
// "信使"，Electron 单实例锁转发后既有实例唤/开其窗口、信使自退（见
// messenger.go 的拉起纪律）；实例是否存在由 service 层依据快照判定。
func (e *Engine) OpenWindow(exe string) (opened bool, err error) {
	if err := e.spawnMessenger(exe); err != nil {
		return false, err
	}
	e.emit(e.Snapshot())
	return true, nil
}

// Quit 优雅退出自有实例（委托内核 Stop 的 grace 语义）：QuitHook 投递 WM_CLOSE
// → 宽限 closeGracePeriod 内等进程自然收口（关窗即退场，含用户处理确认对话框
// 的场）→ 超时 JobObject 强杀兜底（hot exit 已保证编辑内容可恢复）。
// 幂等；external/stopped 状态按既有契约映射为无操作成功（external 不越权强杀，
// 指引文案由 service 层给出）。
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

// RefreshExternal 探测外部实例校正 external/stopped 状态（委托内核）。
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

// WaitReady 阻塞等待 VS Code 实例就绪（探测信号出现），超时返回 false。
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

// ---------- 内核 → vscode 形状映射 ----------

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
		Form:      e.form,
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
//	supervisor stopping → running（vscode 既有词表无 stopping：终止窗口对前端保持
//	                      运行语义，收口后由 stopped/failed 终态广播纠正）
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

// mapErrorMessage 还原 vscode 既有失败文案：
//   - 内核异常退出消息改回"VS Code 异常退出（退出码 N）。请确认托管文件完整
//     未被杀软隔离，或重启 Hanxi 后重试"；
//   - 内核手动停止的"已手动停止"折回本引擎既有的空文案（stopped 态不带话术）；
//   - 其余（启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("VS Code 异常退出（退出码 %d）。请确认托管文件完整未被杀软隔离，或重启 Hanxi 后重试", code)
		}
	}
	if s.State == sup.StateStopped && s.Error == manualStopWording {
		return ""
	}
	return s.Error
}

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// supProbe 把 vscode 形态探针（安装版互斥体 / 便携版镜像路径前缀枚举的存在性
// 结论）适配为内核统一探针契约。两类探测均为瞬时系统调用、天然不可取消
// （任何失败按"不存在"处理，永不报错），因此不产出 ProcInfo（外部实例归属
// 只认 running 事实，PID 无从取得，沿用原口径）。
type supProbe struct{ p Probe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsRunning(), nil, nil
}
