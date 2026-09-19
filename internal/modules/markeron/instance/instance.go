// Package instance 实现 MarkerOn 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 MarkerOn 领域适配：
//   - 命名互斥体探针（supervisor.Probe 形状适配，探针实现见 prober/probe_windows）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的标注态（Drawing 奇偶）/ExitCode/
//     StoppedAt 拼回既有事件契约（前端与 wails 事件载荷零漂移）；
//   - "切换标注态"信使（短命二次拉起经 WM_COPYDATA 转发，见 messenger.go）：
//     不属进程治理，不经内核 Engine。
//
// MarkerOn 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 MarkerOn
// 及其 WebView2 子进程树，杜绝孤儿标注进程。
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
	StateExternal State = "external" // 外部用户自启的 MarkerOn 主实例（非本引擎托管）
)

// Snapshot 引擎状态快照：事件推送与前端渲染共用同一模型。
type Snapshot struct {
	Version   string    `json:"version"`
	State     State     `json:"state"`
	PID       uint32    `json:"pid"`
	ExitCode  int       `json:"exitCode"`
	Error     string    `json:"error"`
	External  bool      `json:"external"` // state==external 时为 true
	Drawing   bool      `json:"drawing"`  // 仅自有实例可信：我方 toggle 奇偶推算的标注态（起始 false=Hidden）
	StartedAt time.Time `json:"startedAt"`
	StoppedAt time.Time `json:"stoppedAt"`
}

// StartOptions 启动参数：由 service 层解析版本后填充。
type StartOptions struct {
	Version string // 绑定版本 vX.Y.Z
	Exe     string // MarkerOn.exe 绝对路径（版本隔离目录内）
	// Detached 独立运行：解除 JobObject 退出联动（Hanxi 关闭完全不影响工具）。
	Detached bool
}

func (o StartOptions) validate() error {
	if o.Exe == "" {
		return fmt.Errorf("MarkerOn.exe 路径不能为空")
	}
	return nil
}

// Callbacks 引擎事件回调（免框架依赖；service 层接 wails 事件推送）。
type Callbacks struct {
	OnState func(snap Snapshot)
}

// Engine MarkerOn 单实例运行引擎：组合内核 supervisor.Engine，
// 本层持有 MarkerOn 专属推算状态（标注态奇偶、退出码/停止时刻账目）。
type Engine struct {
	mu        sync.Mutex
	drawing   bool      // 自有实例标注态（我方 toggle 奇偶推算；用户直接按快捷键会脱同步）
	exitCode  int       // 自有实例最近一次退出码（Start 时清零）
	stoppedAt time.Time // 自有实例最近一次落终态的时刻

	sup   *sup.Engine
	probe MarkerProbe
	cb    Callbacks

	// specArgs 真机冒烟专用注入缝：生产 MarkerOn 恒无参拉起（nil，无参即开
	// 标注 overlay），测试注入让替身进程（cmd.exe）在精简 stdin 环境下也能
	// 稳定存活的确定性命令参数。
	specArgs []string

	toggleMu       sync.Mutex             // 信使进程拉起互斥（防抖）
	spawnMessenger func(exe string) error // 信使拉起接缝（默认真实 spawn，测试注入）
}

// NewEngine 创建托管运行引擎（初始 stopped，无任何系统副作用）；
// JobAPI/Probe/Callbacks 由 service 层注入，保持本包零框架依赖。
func NewEngine(jobAPI platform.JobAPI, probe MarkerProbe, cb Callbacks) *Engine {
	e := &Engine{
		probe:          probe,
		cb:             cb,
		spawnMessenger: spawnMessenger,
	}
	e.sup = sup.NewEngine(jobAPI, supProbe{probe}, sup.Callbacks{OnState: e.onSupState})
	return e
}

// Start 启动自有实例（委托内核：创建进程 → 绑定 JobObject → running）。
// 本方法不做互斥体探测：冷启动与外部实例竞速的 TOCTOU 交给内核 wait 退出分类兜底
// （ReadyTimeout=0 即内核文档所述 markeron 冷启动语义）。
func (e *Engine) Start(opts StartOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.drawing = false // 新进程初始 overlay 为 Hidden
	e.exitCode = 0
	e.stoppedAt = time.Time{}
	e.mu.Unlock()

	// 刻意不设置 HideWindow 等窗口干预：MarkerOn 是 GUI 子系统程序，既不产生
	// 控制台窗口，且需保留其原版行为（自带托盘，属第一阶段零 fork 承诺）。
	return e.sup.Start(context.Background(), sup.Spec{
		Version:       opts.Version,
		Exe:           opts.Exe,
		Args:          e.specArgs,    // 生产恒 nil；仅真机冒烟注入
		DetachFromJob: opts.Detached, // "不随 Hanxi 关闭"开关 → SetAllowKillOnClose(false)
	})
}

// Stop 停止自有实例（幂等；external 状态不在管辖范围内：内核回 ErrExternal，
// 按 markeron 既有契约映射为无操作成功，指引文案由 service 层给出）。
func (e *Engine) Stop() error {
	if err := e.sup.Stop(0); err != nil {
		if errors.Is(err, sup.ErrExternal) {
			return nil
		}
		return err
	}
	return nil
}

// Toggle 拉起同路径第二个 MarkerOn 实例充当单实例协议的"信使"，
// 经 WM_COPYDATA 触发主实例 toggle_drawing（见 messenger.go 的拉起纪律）；
// 仅对自有 running 实例维护奇偶推算，external/stopped 下无本地标注态可言。
func (e *Engine) Toggle(exe string) error {
	e.toggleMu.Lock()
	defer e.toggleMu.Unlock()

	if err := e.spawnMessenger(exe); err != nil {
		return err
	}

	outer := e.sup.Snapshot()
	var snap Snapshot
	e.mu.Lock()
	if outer.State == sup.StateRunning {
		e.drawing = !e.drawing
	}
	snap = e.snapshotLocked(outer)
	e.mu.Unlock()
	e.emit(snap)
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

// WaitReady 阻塞等待 MarkerOn 主实例就绪（互斥体出现），超时返回 false。
func (e *Engine) WaitReady(timeout time.Duration) bool {
	return e.probe.WaitForMarkerOnReady(timeout)
}

// RunningDuration 自有实例已运行时长。
func (e *Engine) RunningDuration() time.Duration {
	s := e.sup.Snapshot()
	if s.State != sup.StateRunning || s.Since.IsZero() {
		return 0
	}
	return time.Since(s.Since)
}

// ---------- 内核 → markeron 形状映射 ----------

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
	state := mapState(s.State)
	switch state {
	case StateStopped, StateExternal:
		// 进程退出/外部实例：标注态自然消失（沿用原 transition/setStateExternal 规则）
		e.drawing = false
	}
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
		State:     state,
		PID:       s.PID,
		ExitCode:  e.exitCode,
		Error:     mapErrorMessage(s),
		External:  s.State == sup.StateExternal,
		Drawing:   e.drawing,
		StartedAt: s.Since,
		StoppedAt: e.stoppedAt,
	}
}

// mapState 状态词表映射：
//
//	supervisor stopped  → stopped
//	supervisor starting → starting
//	supervisor running  → running
//	supervisor stopping → running（markeron 既有词表无 stopping：终止窗口对前端保持
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

// mapErrorMessage 还原 markeron 既有失败文案：内核异常退出消息改回
// "MarkerOn 异常退出（退出码 N）。请确认已安装 WebView2 Runtime"；
// 其余（手动停止/启动失败等）透传。
func mapErrorMessage(s sup.Snapshot) string {
	if s.State == sup.StateFailed {
		if code, ok := exitCodeFromKernelMessage(s.Error); ok {
			return fmt.Sprintf("MarkerOn 异常退出（退出码 %d）。请确认已安装 WebView2 Runtime", code)
		}
	}
	return s.Error
}

// emit 状态广播（回调在锁外执行，防止回调内重入本引擎造成死锁）。
func (e *Engine) emit(snap Snapshot) {
	if e.cb.OnState != nil {
		e.cb.OnState(snap)
	}
}

// supProbe 把 MarkerProbe（命名互斥体存在性）适配为内核统一探针契约。
// 互斥体探测为瞬时系统调用、天然不可取消（任何失败按"不存在"处理，永不报错），
// 因此不产出 ProcInfo（外部实例归属只认 running 事实，PID 无从取得，沿用原口径）。
type supProbe struct{ p MarkerProbe }

func (s supProbe) Inspect(_ context.Context) (bool, *platform.ProcInfo, error) {
	return s.p.IsMarkerOnRunning(), nil, nil
}
