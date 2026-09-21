package extapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// drain 预算：停用/退出的有界等待。存量模块普遍缺取消通道，属专项计划
// 认可的过渡态（"先实现拒绝新操作和有界 drain，逐模块补取消"）；超时后
// 强制收口并以 Error 显式告警，绝不静默——在途调用由模块内部锁兜底，
// JobObject 保证进程侧无孤儿。
const (
	drainBudgetDisable  = 30 * time.Second
	drainBudgetShutdown = 60 * time.Second
)

// 哨兵错误：调用方可用 errors.Is 区分"模块不存在"与"模块已禁用"两类失败。
var (
	// ErrUnknownModule 表示注册表中不存在该 ID 的模块。
	ErrUnknownModule = errors.New("registry: unknown module")
	// ErrModuleDisabled 表示模块存在但当前处于禁用状态，拒绝初始化或调用。
	ErrModuleDisabled = errors.New("registry: module disabled")
	// ErrModuleNotInstalled 表示模块存在且启用，但缺少逻辑安装凭据（receipt），
	// 调用门拒绝执行。Phase 1-2 由模块中心"安装/卸载"操作驱动该维度。
	ErrModuleNotInstalled = errors.New("registry: module not installed")
)

// StateStorage 状态持久化抽象接口
type StateStorage interface {
	IsModuleEnabled(moduleId string, defaultEnabled bool) bool
	SetModuleEnabled(moduleId string, enabled bool) error
}

// ModuleWrapper 包装具体模块与其运行时状态。
// mu 保护启停状态、生命周期转换与命令 operation lease；模块业务回调一律在锁外执行。
type ModuleWrapper struct {
	Module       Module
	Enabled      bool
	initialized  bool
	initializing bool
	stopping     bool
	inFlight     int
	// failed 滞留上次 OnInit 失败事实：投影为 RuntimeFailed，直到重试成功、
	// 重新启用或完成析构收口（Wave 0 契约四维状态的运行维度输入）。
	failed bool
	mu     sync.Mutex
	cond   *sync.Cond
}

// stateOverride 承载不由 wrapper 现场派生的维度事实：
// mandatory（Core 控制平面）与 blocked（安全/撤回阻止）由装配根或后续
// 事务引擎注入；health 预留给 Wave 5+ 的签名目录裁决，内建逻辑模块恒 current。
type stateOverride struct {
	mandatory     bool
	blocked       bool
	health        HealthState
	remoteVersion string // health=update-available 时的上游新版本（仅展示,不裁决）
}

// Registry 管理内建扩展的注册、生命周期与启用状态。
type Registry struct {
	mu        sync.RWMutex
	modules   map[string]*ModuleWrapper
	store     StateStorage
	receipts  ReceiptStorage
	overrides map[string]stateOverride // 值类型：读写均经 editOverride/overrideSnapshot 单通道，指针不出锁域
	// onLifecycles 启停副作用钩子链（托盘重建、热键注销等），在 wrapper 锁外依次调用。
	onLifecycles []func(moduleID string, enabled bool)
}

// NewRegistry 创建注册表。store 可为 nil，此时启用状态不持久化（仅内存生效，重启后恢复默认）。
func NewRegistry(store StateStorage) *Registry {
	return &Registry{
		modules: make(map[string]*ModuleWrapper),
		store:   store,
	}
}

// Register 注册一个或多个扩展（此时仅注册元数据，不分配运行时重资源）。
func (r *Registry) Register(exts ...Module) error {
	gated, err := r.registerLocked(exts)
	if err != nil {
		return err
	}
	// 调用门注入在 Registry 锁外统一执行（模块 SetGate 可能触碰自身状态，
	// 不在写锁内回调业务代码）。
	gate := gateView{registry: r}
	for _, e := range gated {
		e.(GateAware).SetGate(gate)
	}
	return nil
}

func (r *Registry) registerLocked(exts []Module) ([]Module, error) {
	var gated []Module
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range exts {
		info := e.Info()
		id := info.ID
		if id == "" {
			return nil, fmt.Errorf("registry: extension with empty id")
		}
		if _, dup := r.modules[id]; dup {
			return nil, fmt.Errorf("registry: duplicate extension id %q", id)
		}

		// 优先从持久化 store 读取状态，默认 true
		enabled := true
		if r.store != nil {
			enabled = r.store.IsModuleEnabled(id, true)
		}

		wrapper := &ModuleWrapper{
			Module:  e,
			Enabled: enabled,
		}
		wrapper.cond = sync.NewCond(&wrapper.mu)
		r.modules[id] = wrapper

		if _, ok := e.(GateAware); ok {
			gated = append(gated, e)
		}
	}
	return gated, nil
}

func (r *Registry) wrapper(id string) (*ModuleWrapper, bool) {
	r.mu.RLock()
	wrapper, ok := r.modules[id]
	r.mu.RUnlock()
	return wrapper, ok
}

// waitForIdleLocked 有界等待 initializing 与 inFlight 归零（调用方持有 w.mu）。
// 返回是否完全收口；超时后不再等待——停用死锁是比残留更糟的失败模式。
// 超时时在途调用仍持租约，其安全由模块内部锁兜底，投影随后自然收敛。
func (w *ModuleWrapper) waitForIdleLocked(budget time.Duration) bool {
	if !w.initializing && w.inFlight == 0 {
		return true
	}
	stopWake := make(chan struct{})
	defer close(stopWake)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.mu.Lock()
				w.cond.Broadcast()
				w.mu.Unlock()
			case <-stopWake:
				return
			}
		}
	}()
	deadline := time.Now().Add(budget)
	for w.initializing || w.inFlight > 0 {
		if time.Now().After(deadline) {
			return false
		}
		w.cond.Wait()
	}
	return true
}

func (r *Registry) wrappers() []*ModuleWrapper {
	r.mu.RLock()
	out := make([]*ModuleWrapper, 0, len(r.modules))
	for _, wrapper := range r.modules {
		out = append(out, wrapper)
	}
	r.mu.RUnlock()
	return out
}

// EnsureActive 确保指定模块已完成懒初始化。在模块页面进入或业务接口调用前统一调用。
// 生命周期回调在 wrapper.mu 外执行；initializing/stopping + cond 串行转换。
func (r *Registry) EnsureActive(moduleID string) error {
	moduleID = strings.TrimSpace(moduleID)
	wrapper, ok := r.wrapper(moduleID)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownModule, moduleID)
	}
	if err := r.checkInstalled(moduleID); err != nil {
		return err
	}
	return ensureActive(wrapper, moduleID)
}

// checkInstalled 裁决 Delivery 维度（Wave 1 口径：未安装 = 不初始化、不进入口、
// 不允许业务调用）。receipts 未注入时（单测、无头进程）全部按已安装处理。
func (r *Registry) checkInstalled(moduleID string) error {
	r.mu.RLock()
	receipts := r.receipts
	r.mu.RUnlock()
	if receipts != nil && !receipts.IsInstalled(moduleID) {
		return fmt.Errorf("%w %q", ErrModuleNotInstalled, moduleID)
	}
	return nil
}

func ensureActive(wrapper *ModuleWrapper, moduleID string) error {
	wrapper.mu.Lock()
	for wrapper.initializing {
		wrapper.cond.Wait()
		if !wrapper.Enabled || wrapper.stopping {
			wrapper.mu.Unlock()
			return fmt.Errorf("%w %q", ErrModuleDisabled, moduleID)
		}
		if wrapper.initialized {
			wrapper.mu.Unlock()
			return nil
		}
	}
	if !wrapper.Enabled || wrapper.stopping {
		wrapper.mu.Unlock()
		return fmt.Errorf("%w %q", ErrModuleDisabled, moduleID)
	}
	if wrapper.initialized {
		wrapper.mu.Unlock()
		return nil
	}
	wrapper.initializing = true
	wrapper.mu.Unlock()

	err := wrapper.Module.OnInit(context.Background())

	wrapper.mu.Lock()
	wrapper.initializing = false
	if err == nil {
		// OnInit 已成功分配的资源必须入账，即使停用已在等待；这样停用方才能
		// 在唤醒后执行 OnDestroy，而不是因 initialized=false 泄露资源。
		wrapper.initialized = true
		wrapper.failed = false
	} else {
		// 失败事实滞留供四维投影呈现 RuntimeFailed，重试成功或收口时清除。
		wrapper.failed = true
	}
	disabled := !wrapper.Enabled || wrapper.stopping
	wrapper.cond.Broadcast()
	wrapper.mu.Unlock()
	if err != nil {
		return fmt.Errorf("registry: init module %q failed: %w", moduleID, err)
	}
	if disabled {
		return fmt.Errorf("%w %q", ErrModuleDisabled, moduleID)
	}
	return nil
}

// List 返回全部扩展元信息（含启用状态与初始化状态）。
func (r *Registry) List() []ModuleInfo {
	wrappers := r.wrappers()
	out := make([]ModuleInfo, 0, len(wrappers))
	for _, wrapper := range wrappers {
		wrapper.mu.Lock()
		info := wrapper.Module.Info()
		info.Enabled = wrapper.Enabled
		info.Initialized = wrapper.initialized
		wrapper.mu.Unlock()
		info.Installed = r.IsInstalled(info.ID)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// IsInstalled 查询模块是否持有逻辑安装凭据（receipts 未注入时恒为 true）。
func (r *Registry) IsInstalled(moduleID string) bool {
	r.mu.RLock()
	receipts := r.receipts
	r.mu.RUnlock()
	return receipts == nil || receipts.IsInstalled(moduleID)
}

// GetEnabledNavs 返回所有已启用且已安装模块的导航条目，按稳定键全局排序。
// 未安装（缺逻辑 receipt）模块不进导航（Wave 1 可见性口径，ADR-0001 §1.4）。
func (r *Registry) GetEnabledNavs() []NavEntry {
	wrappers := r.wrappers()
	r.mu.RLock()
	receipts := r.receipts
	r.mu.RUnlock()
	var out []NavEntry
	for _, wrapper := range wrappers {
		wrapper.mu.Lock()
		if wrapper.Enabled {
			id := wrapper.Module.Info().ID
			if receipts == nil || receipts.IsInstalled(id) {
				out = append(out, wrapper.Module.Nav()...)
			}
		}
		wrapper.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		if out[i].Route != out[j].Route {
			return out[i].Route < out[j].Route
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// AllServices 返回所有模块的 Wails service，用于启动时静态注册与绑定生成。
// 服务被静态注册不代表对应模块已启用或已完成运行时初始化。
func (r *Registry) AllServices() []Service {
	wrappers := r.wrappers()
	var out []Service
	for _, wrapper := range wrappers {
		out = append(out, wrapper.Module.Services()...)
	}
	return out
}

// IsEnabled 查询扩展启用状态。
func (r *Registry) IsEnabled(id string) bool {
	wrapper, ok := r.wrapper(id)
	if !ok {
		return false
	}
	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()
	return wrapper.Enabled
}

// IsActive 查询模块是否已被懒加载激活。
func (r *Registry) IsActive(id string) bool {
	wrapper, ok := r.wrapper(id)
	if !ok {
		return false
	}
	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()
	return wrapper.initialized
}

// SetEnabled 启停扩展。停用先阻止新命令并等待在飞命令/初始化完成，再于锁外
// 调用 OnDestroy；返回时模块已不可执行且运行时资源已收口。
func (r *Registry) SetEnabled(id string, enabled bool) error {
	wrapper, ok := r.wrapper(id)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownModule, id)
	}
	// 启用门：未安装模块不得直接启用（安装动作属于 SetInstalled 事务通道）。
	// 停用不受安装态限制，保证异常残留时仍可收口。
	if enabled {
		if err := r.checkInstalled(id); err != nil {
			return err
		}
	} else if r.IsMandatory(id) {
		// Core 守卫：mandatory 模块不允许停用，杜绝"停用成功但投影仍显
		// mandatory"的双源分叉（ADR-0001 §1.4）。
		return fmt.Errorf("registry: module %q is mandatory core, cannot be disabled", id)
	}

	var destroy bool
	wrapper.mu.Lock()
	if enabled {
		for wrapper.stopping {
			wrapper.cond.Wait()
		}
		wrapper.Enabled = true
		wrapper.failed = false
		wrapper.mu.Unlock()
	} else {
		// 先关门：command lease 在同一把锁下检查 Enabled/stopping，之后不再有新命令进入。
		wrapper.Enabled = false
		if wrapper.stopping {
			for wrapper.stopping {
				wrapper.cond.Wait()
			}
			wrapper.mu.Unlock()
		} else {
			wrapper.stopping = true
			if !wrapper.waitForIdleLocked(drainBudgetDisable) {
				slog.Error("registry: 停用 drain 超时强制收口，在途调用由模块内部锁兜底",
					"module", id, "budget", drainBudgetDisable.String())
			}
			destroy = wrapper.initialized
			wrapper.mu.Unlock()

			if destroy {
				if err := wrapper.Module.OnDestroy(); err != nil {
					slog.Warn("registry: OnDestroy failed", "module", id, "err", err)
				}
			}

			wrapper.mu.Lock()
			wrapper.initialized = false
			wrapper.stopping = false
			wrapper.failed = false
			wrapper.cond.Broadcast()
			wrapper.mu.Unlock()

			if destroy {
				go func() {
					runtime.GC()
					debug.FreeOSMemory()
				}()
			}
		}
	}

	// 启停副作用（托盘重建、热键注销等）在 wrapper 锁外、持久化前触发；
	// 钩子 panic 不允许吞掉状态变更，持久化失败仍按原语义上抛。
	r.fireLifecycle(id, enabled)

	if r.store != nil {
		return r.store.SetModuleEnabled(id, enabled)
	}
	return nil
}

// fireLifecycle 在全部锁外语境下依次调用启停钩子；钩子由装配根注册，必须自容错。
func (r *Registry) fireLifecycle(id string, enabled bool) {
	r.mu.RLock()
	hooks := make([]func(string, bool), len(r.onLifecycles))
	copy(hooks, r.onLifecycles)
	r.mu.RUnlock()
	for _, hook := range hooks {
		hook(id, enabled)
	}
}

// TrayCommandInfo 托盘命令目录项：设置页候选与托盘装配统一按 Key 引用。
type TrayCommandInfo struct {
	Key        string `json:"key"` // "moduleId/commandId" 稳定引用
	ModuleID   string `json:"moduleId"`
	ModuleName string `json:"moduleName"`
	ID         string `json:"id"`
	Label      string `json:"label"`
}

// ListTrayCommands 聚合所有已启用模块实现的托盘命令（按模块 ID 升序，命令保持声明顺序）。
func (r *Registry) ListTrayCommands() []TrayCommandInfo {
	r.mu.RLock()
	ids := make([]string, 0, len(r.modules))
	wrappers := make(map[string]*ModuleWrapper, len(r.modules))
	for id, wrapper := range r.modules {
		ids = append(ids, id)
		wrappers[id] = wrapper
	}
	r.mu.RUnlock()
	sort.Strings(ids)

	var out []TrayCommandInfo
	for _, id := range ids {
		wrapper := wrappers[id]
		wrapper.mu.Lock()
		if !wrapper.Enabled {
			wrapper.mu.Unlock()
			continue
		}
		if !r.IsInstalled(id) {
			wrapper.mu.Unlock()
			continue
		}
		provider, ok := wrapper.Module.(TrayCommandsProvider)
		if !ok {
			wrapper.mu.Unlock()
			continue
		}
		name := wrapper.Module.Info().Name
		commands := provider.TrayCommands()
		wrapper.mu.Unlock()
		for _, cmd := range commands {
			out = append(out, TrayCommandInfo{
				Key:        id + "/" + cmd.ID,
				ModuleID:   id,
				ModuleName: name,
				ID:         cmd.ID,
				Label:      cmd.Label,
			})
		}
	}
	return out
}

// RunTrayCommand 按 key 执行托盘命令。命令经统一 Acquire 取得 operation lease 后
// 在 wrapper.mu 外执行；停用先关门阻止新 lease，再等待 inFlight 归零后 OnDestroy。
func (r *Registry) RunTrayCommand(ctx context.Context, key string) error {
	moduleID, cmdID, ok := splitTrayKey(key)
	if !ok {
		return fmt.Errorf("registry: invalid tray command key %q", key)
	}
	wrapper, release, err := r.Acquire(moduleID)
	if err != nil {
		return err
	}
	defer release()

	provider, ok := wrapper.Module.(TrayCommandsProvider)
	if !ok {
		return fmt.Errorf("registry: module %q provides no tray commands", moduleID)
	}

	// provider 枚举与命令业务均在锁外执行，但 operation lease 全程覆盖；停用会
	// 等待本次查找/执行结束后才析构模块。
	commands := provider.TrayCommands()
	for _, cmd := range commands {
		if cmd.ID == cmdID && cmd.Run != nil {
			return cmd.Run(ctx)
		}
	}
	return fmt.Errorf("registry: tray command %q not found in module %q", cmdID, moduleID)
}

// splitTrayKey 解析 "moduleId/commandId" 形式的托盘命令引用。
func splitTrayKey(key string) (moduleID, cmdID string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(key), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ---------------------------------------------------------------------------
// Wave 1：四维状态投影与统一调用门（catalog.go 契约的 Registry 侧实现）
// ---------------------------------------------------------------------------

// Install 执行逻辑安装事务（Phase 1 最小形态，ADR-0001 §1.5/§8.4 口径）：
// 登记 receipt → 默认启用（安装即可用）；幂等，重复安装保留首次凭据。
// 内建逻辑模块的"安装"不复制宿主代码，UI 必须如实呈现不释放体积。
func (r *Registry) Install(moduleID string, kind ReceiptKind) error {
	r.mu.RLock()
	receipts := r.receipts
	r.mu.RUnlock()
	if receipts == nil {
		return fmt.Errorf("registry: receipt storage not configured")
	}
	if _, ok := r.wrapper(moduleID); !ok {
		return fmt.Errorf("%w %q", ErrUnknownModule, moduleID)
	}
	if !receipts.IsInstalled(moduleID) {
		if err := receipts.MarkInstalled(moduleID, kind); err != nil {
			return fmt.Errorf("registry: install module %q: %w", moduleID, err)
		}
	}
	return r.SetEnabled(moduleID, true)
}

// Uninstall 执行逻辑卸载事务：先 SetEnabled(false) 完成 drain/析构收口，
// 再移除 receipt；用户数据默认保留（数据策略由调用方在 UI 层单独裁决）。幂等。
func (r *Registry) Uninstall(moduleID string) error {
	r.mu.RLock()
	receipts := r.receipts
	r.mu.RUnlock()
	if receipts == nil {
		return fmt.Errorf("registry: receipt storage not configured")
	}
	if _, ok := r.wrapper(moduleID); !ok {
		return fmt.Errorf("%w %q", ErrUnknownModule, moduleID)
	}
	if r.IsMandatory(moduleID) {
		return fmt.Errorf("registry: module %q is mandatory core, cannot be uninstalled", moduleID)
	}
	if err := r.SetEnabled(moduleID, false); err != nil {
		return err
	}
	if err := receipts.MarkAbsent(moduleID); err != nil {
		return fmt.Errorf("registry: uninstall module %q: %w", moduleID, err)
	}
	slog.Info("registry: 模块已逻辑卸载（宿主内建代码仍存在，未释放主程序体积）", "module", moduleID)
	return nil
}

// SetReceiptStorage 注入逻辑安装凭据存储。未注入时全部内建模块按已安装处理
// （兼容注册表单测与旧装配路径）；注入后 Delivery 维度以 receipt 为权威。
func (r *Registry) SetReceiptStorage(rs ReceiptStorage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receipts = rs
}

// OnLifecycle 追加一个模块启停副作用钩子（托盘重建、热键注销等）。
// 钩子在 wrapper 锁外、Registry 持久化前按注册顺序依次调用；钩子必须自容错。
func (r *Registry) OnLifecycle(fn func(moduleID string, enabled bool)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onLifecycles = append(r.onLifecycles, fn)
}

// SetMandatory 标记/解除 Core 模块（策略维度投影为 mandatory，禁止停用与卸载）。
func (r *Registry) SetMandatory(moduleID string, mandatory bool) {
	r.editOverride(moduleID, func(o *stateOverride) { o.mandatory = mandatory })
}

// IsMandatory 查询模块是否为 Core 强制项。
func (r *Registry) IsMandatory(moduleID string) bool {
	ov, _ := r.overrideSnapshot(moduleID)
	return ov.mandatory
}

// SetBlocked 标记/解除安全阻止（撤回、不兼容、恢复失败等）；blocked 拒绝 Acquire。
func (r *Registry) SetBlocked(moduleID string, blocked bool) {
	r.editOverride(moduleID, func(o *stateOverride) { o.blocked = blocked })
}

// SetHealth 写入健康维度覆盖（Wave 5+ 签名目录裁决用）；传空串恢复 current。
// remoteVersion 仅在 health=update-available 时随记录写入（更新感知链给出的上游
// 新版本号，纯展示输入，不进状态机）；health 为 current 或其他值时一律清空。
// health 与 remoteVersion 在同一把写锁内成对落账——投影读到的永远是同一轮事实。
func (r *Registry) SetHealth(moduleID string, health HealthState, remoteVersion string) {
	r.editOverride(moduleID, func(o *stateOverride) {
		if health == "" {
			health = HealthCurrent
		}
		o.health = health
		if health == HealthUpdateAvailable {
			o.remoteVersion = remoteVersion
		} else {
			o.remoteVersion = ""
		}
	})
}

// editOverride 覆盖态唯一写通道：全程持写锁做"取值→闭包改→回存"，
// 临时指针不出锁域（批 1 竞态修复：原 overrideOf 让内部指针逃逸到锁外被并发读写）。
func (r *Registry) editOverride(moduleID string, edit func(*stateOverride)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.overrides == nil {
		r.overrides = map[string]stateOverride{}
	}
	o := r.overrides[moduleID] // 值拷贝
	edit(&o)
	r.overrides[moduleID] = o
}

// overrideSnapshot 覆盖态唯一读通道：锁内复制值，缺省字段即零值
// （health 空串由投影侧按 current 处理，与原构造默认等义）。
func (r *Registry) overrideSnapshot(moduleID string) (stateOverride, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.overrides == nil {
		return stateOverride{}, false
	}
	o, ok := r.overrides[moduleID]
	return o, ok
}

// runtimeStateLocked 把 wrapper 现场标志折算为运行维度投影。
func (w *ModuleWrapper) runtimeStateLocked() RuntimeState {
	switch {
	case w.stopping:
		return RuntimeStopping
	case w.initializing:
		return RuntimeActivating
	case w.initialized && w.inFlight > 0:
		return RuntimeBusy
	case w.initialized:
		return RuntimeActive
	case w.failed:
		return RuntimeFailed
	default:
		return RuntimeInactive
	}
}

// ListStates 返回全部模块的四维状态投影（含派生主操作与摘要），按 ID 稳定排序。
// Registry 是权威源；任何前端/页面不得据此再推导第二份状态。
func (r *Registry) ListStates() []ModuleState {
	wrappers := r.wrappers()
	out := make([]ModuleState, 0, len(wrappers))
	for _, wrapper := range wrappers {
		wrapper.mu.Lock()
		moduleID := wrapper.Module.Info().ID
		enabled := wrapper.Enabled
		runtime := wrapper.runtimeStateLocked()
		wrapper.mu.Unlock()
		out = append(out, r.projectState(moduleID, enabled, runtime))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleID < out[j].ModuleID })
	return out
}

// projectState 合并 wrapper 现场与 overrides，计算一维完整投影。
func (r *Registry) projectState(moduleID string, enabled bool, runtime RuntimeState) ModuleState {
	r.mu.RLock()
	receipts := r.receipts
	r.mu.RUnlock()
	ov, _ := r.overrideSnapshot(moduleID) // 锁内值拷贝：health/remoteVersion 同源一轮

	policy := PolicyDisabled
	if enabled {
		policy = PolicyEnabled
	}
	health := HealthCurrent
	if ov.mandatory {
		policy = PolicyMandatory
	} else if ov.blocked {
		policy = PolicyBlocked
	}
	if ov.health != "" {
		health = ov.health
	}
	delivery := DeliveryInstalled
	if receipts != nil && !receipts.IsInstalled(moduleID) {
		delivery = DeliveryAbsent
	}
	out := StateInput{
		ModuleID: moduleID,
		Delivery: delivery,
		Policy:   policy,
		Runtime:  runtime,
		Health:   health,
	}.Project()
	// remoteVersion 是纯展示附加：仅当 health=update-available 且感知链留有
	// 版本号时盖进投影；不进 StateInput/Project()，不参与状态机裁决。
	if health == HealthUpdateAvailable {
		out.RemoteVersion = ov.remoteVersion
	}
	return out
}

// Acquire 是统一调用门的入口形态（Wave 3 全入口接线）：检查顺序为
// blocked → receipt 已安装 → 懒激活（Enabled/并发初始化）→ operation lease 入账。
// blocked 先于安装检查：撤回/阻止类安全裁决优先于一切状态呈现（与 Project()
// 状态机优先级一致，ADR-0001 §1.3/§1.4）。
// 返回的 release 必须在业务结束时调用（defer 即可，可重入安全）；
// 停用与退出的 drain 以 lease 归零为门，语义与原托盘命令一致。
func (r *Registry) Acquire(moduleID string) (*ModuleWrapper, func(), error) {
	moduleID = strings.TrimSpace(moduleID)
	wrapper, ok := r.wrapper(moduleID)
	if !ok {
		return nil, nil, fmt.Errorf("%w %q", ErrUnknownModule, moduleID)
	}

	ovSnap, _ := r.overrideSnapshot(moduleID)
	if ovSnap.blocked {
		return nil, nil, fmt.Errorf("%w %q (blocked)", ErrModuleDisabled, moduleID)
	}
	if err := r.checkInstalled(moduleID); err != nil {
		slog.Info("registry: 调用门拒绝未安装模块", "module", moduleID)
		return nil, nil, err
	}

	if err := ensureActive(wrapper, moduleID); err != nil {
		return nil, nil, err
	}

	wrapper.mu.Lock()
	if !wrapper.Enabled || wrapper.stopping || !wrapper.initialized {
		wrapper.mu.Unlock()
		return nil, nil, fmt.Errorf("%w %q", ErrModuleDisabled, moduleID)
	}
	wrapper.inFlight++
	wrapper.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			wrapper.mu.Lock()
			wrapper.inFlight--
			if wrapper.inFlight == 0 {
				wrapper.cond.Broadcast()
			}
			wrapper.mu.Unlock()
		})
	}
	return wrapper, release, nil
}

// ShutdownAll 应用退出时清理所有已初始化的模块。与 SetEnabled(false) 同样先
// 阻止新命令、等待 operation lease 清空，再在锁外析构。
func (r *Registry) ShutdownAll() {
	for _, wrapper := range r.wrappers() {
		wrapper.mu.Lock()
		wrapper.Enabled = false
		for wrapper.stopping {
			wrapper.cond.Wait()
		}
		wrapper.stopping = true
		if !wrapper.waitForIdleLocked(drainBudgetShutdown) {
			slog.Error("registry: 退出 drain 超时强制收口（JobObject 保证无进程孤儿，内存残留随进程释放）",
				"budget", drainBudgetShutdown.String())
		}
		destroy := wrapper.initialized
		wrapper.mu.Unlock()

		if destroy {
			if err := wrapper.Module.OnDestroy(); err != nil {
				slog.Warn("registry: ShutdownAll OnDestroy failed", "err", err)
			}
		}

		wrapper.mu.Lock()
		wrapper.initialized = false
		wrapper.stopping = false
		wrapper.cond.Broadcast()
		wrapper.mu.Unlock()
	}
}
