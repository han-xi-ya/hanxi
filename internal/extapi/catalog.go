// catalog.go 是"工作台界面 × 官方模块分发"联合改造的共同契约（Wave 0 冻结产物）。
//
// 三份专项文档（docs/plans/PLAN_WORKBENCH_MODULE_ROADMAP.md 与两份专项计划）约定：
// 界面（首页/模块中心/导航/搜索/托盘/热键/MCP）与后端生命周期共享同一投影，
// 前端不维护第二份模块安装、启用、运行或健康状态。本文件是该契约的 Go 权威源：
//
//   - ModuleCatalogItem：静态身份与能力目录（模块是谁、如何交付、有哪些入口），
//     不承载瞬时进度，由装配根 Catalog 表投影；
//   - ModuleState：四维事实投影（Delivery/Policy/Runtime/Health），
//     Registry/生命周期管理器是权威源，主操作与禁止原因由统一状态机计算；
//   - Operation：长操作与调用租约的载荷契约（引擎实现按 Wave 3/4 落地，
//     本 Wave 只冻结结构与语义）。
//
// 契约版本化：ModuleContractSchema 随投影结构体下发，前端与契约测试以其判定兼容性；
// 破坏性变更必须递增 schema 并在 docs/adr/ 立案，禁止静默改语义。
package extapi

import "slices"

// ModuleContractSchema 是模块状态契约的当前版本号（Wave 0 冻结为 1）。
const ModuleContractSchema = 1

// ---------------------------------------------------------------------------
// 交付形态（静态：模块如何到达本机）
// ---------------------------------------------------------------------------

// DeliveryKind 描述模块的交付形态。Phase 1-2 仅 builtin-logical 落地；
// managed-declarative 与 official-sidecar 分别对应 Wave 5 / Wave 6。
type DeliveryKind string

const (
	// DeliveryBuiltinLogical 内建代码 + 逻辑安装凭据：代码始终随宿主发布，
	// "卸载"仅移除逻辑 receipt 与入口，不释放宿主体积——UI 文案必须如实表达。
	DeliveryBuiltinLogical DeliveryKind = "builtin-logical"
	// DeliveryManagedDeclarative 官方签名声明驱动的共享托管模块（Wave 5+）。
	DeliveryManagedDeclarative DeliveryKind = "managed-declarative"
	// DeliveryOfficialSidecar 官方复杂功能独立交付包（Wave 6+）。
	DeliveryOfficialSidecar DeliveryKind = "official-sidecar"
)

// ---------------------------------------------------------------------------
// 四维状态（动态事实，独立存储、独立展示，再派生摘要）
// ---------------------------------------------------------------------------

// DeliveryState 交付维度：本机是否持有可运行的安装凭据/资产。
type DeliveryState string

const (
	// DeliveryAbsent 逻辑未安装，或本机无可用声明/sidecar/所需资产。
	DeliveryAbsent DeliveryState = "absent"
	// DeliveryInstalling 正在安装逻辑凭据、托管资产或官方包。
	DeliveryInstalling DeliveryState = "installing"
	// DeliveryInstalled 已安装并有完整 receipt。
	DeliveryInstalled DeliveryState = "installed"
	// DeliveryUpdating 更新事务进行中，旧版本尽量保持可用。
	DeliveryUpdating DeliveryState = "updating"
	// DeliveryRemoving 正在停止并移除。
	DeliveryRemoving DeliveryState = "removing"
	// DeliveryRepairing 正在按可信清单修复。
	DeliveryRepairing DeliveryState = "repairing"
	// DeliveryOrphaned 磁盘内容与 receipt/Catalog 不一致。
	DeliveryOrphaned DeliveryState = "orphaned"
)

// PolicyState 策略维度：用户/平台是否允许使用。
type PolicyState string

const (
	// PolicyEnabled 允许进入导航和调用门。
	PolicyEnabled PolicyState = "enabled"
	// PolicyDisabled 已安装但停用。
	PolicyDisabled PolicyState = "disabled"
	// PolicyBlocked 因不兼容、撤回、恢复或安全策略禁止启动。
	PolicyBlocked PolicyState = "blocked"
	// PolicyPendingConsent 新权限或数据迁移等待用户确认。
	PolicyPendingConsent PolicyState = "pending-consent"
	// PolicyMandatory Core 控制平面：不允许卸载或停用。
	PolicyMandatory PolicyState = "mandatory"
)

// RuntimeState 运行维度：当前是否占用运行资源。
// 由 Registry wrapper 事实派生：enabled=false→inactive；initializing→activating；
// stopping→stopping；inFlight>0→busy；initialized→active；init 失败滞留→failed。
type RuntimeState string

const (
	// RuntimeInactive 没有运行资源。
	RuntimeInactive RuntimeState = "inactive"
	// RuntimeActivating 正在初始化、启动或探测。
	RuntimeActivating RuntimeState = "activating"
	// RuntimeActive 可接受调用。
	RuntimeActive RuntimeState = "active"
	// RuntimeBusy 有在途操作。
	RuntimeBusy RuntimeState = "busy"
	// RuntimeStopping 正在 drain/cancel/destroy。
	RuntimeStopping RuntimeState = "stopping"
	// RuntimeCrashed 受管进程异常退出。
	RuntimeCrashed RuntimeState = "crashed"
	// RuntimeFailed 初始化或恢复失败（滞留在 wrapper，待重试）。
	RuntimeFailed RuntimeState = "failed"
)

// HealthState 健康与版本维度：本机内容与官方目录的一致性判断。
// Phase 1-2 内建逻辑模块恒为 current；其余取值服务 Wave 4+ 托管与签名目录。
type HealthState string

const (
	// HealthCurrent 当前版本可用且无已知更新。
	HealthCurrent HealthState = "current"
	// HealthUpdateAvailable 有兼容更新。
	HealthUpdateAvailable HealthState = "update-available"
	// HealthPinned 用户固定版本。
	HealthPinned HealthState = "pinned"
	// HealthIncompatible 与宿主、策略、架构或上游环境不兼容。
	HealthIncompatible HealthState = "incompatible"
	// HealthCorrupt 摘要、布局或 receipt 不一致。
	HealthCorrupt HealthState = "corrupt"
	// HealthRevoked 官方目录撤回当前声明或包。
	HealthRevoked HealthState = "revoked"
	// HealthUnverified 缺少有效官方验证，不可激活。
	HealthUnverified HealthState = "unverified"
	// HealthDegraded 可用但部分探测、来源或能力异常。
	HealthDegraded HealthState = "degraded"
	// HealthOfflineStale 本地目录过期，已有版本按离线策略继续使用。
	HealthOfflineStale HealthState = "offline-stale"
)

// ---------------------------------------------------------------------------
// 入口与主操作词汇（所有产品面共享同一枚举，禁止页面私有状态机）
// ---------------------------------------------------------------------------

// Entrypoint 模块对外暴露的能力入口类型。可见性与调用门按入口统一裁决：
// 未安装或停用模块不得从任一入口旁路执行。
type Entrypoint string

const (
	// EntryRPC Wails 服务方法调用。
	EntryRPC Entrypoint = "rpc"
	// EntryNavigation 侧栏/路由导航。
	EntryNavigation Entrypoint = "navigation"
	// EntrySearch 命令面板与全局搜索。
	EntrySearch Entrypoint = "search"
	// EntryTray 系统托盘菜单与命令。
	EntryTray Entrypoint = "tray"
	// EntryHotkey 全局热键。
	EntryHotkey Entrypoint = "hotkey"
	// EntryMCP MCP 工具暴露。
	EntryMCP Entrypoint = "mcp"
	// EntryWindow 独立窗口（quickmenu/webapp/msgboard 等）。
	EntryWindow Entrypoint = "window"
	// EntryBackground 启动预激活与常驻后台任务。
	EntryBackground Entrypoint = "background"
)

// PrimaryAction 状态机计算出的卡片主操作语义键。前端按词汇表映射文案，
// 不得依据维度自行推断。
type PrimaryAction string

const (
	// ActionInstall 逻辑安装（builtin-logical：仅登记凭据与入口）。
	ActionInstall PrimaryAction = "install"
	// ActionEnable 启用。
	ActionEnable PrimaryAction = "enable"
	// ActionDisable 停用。
	ActionDisable PrimaryAction = "disable"
	// ActionOpen 打开/进入（含懒激活）。
	ActionOpen PrimaryAction = "open"
	// ActionUpdate 更新到新版本。
	ActionUpdate PrimaryAction = "update"
	// ActionRetry 重试初始化/恢复（崩溃或失败后）。
	ActionRetry PrimaryAction = "retry"
	// ActionRepair 按可信清单修复。
	ActionRepair PrimaryAction = "repair"
	// ActionUninstall 卸载。
	ActionUninstall PrimaryAction = "uninstall"
	// ActionNone 无可执行主操作（进行中或被阻止），以 Reason 说明。
	ActionNone PrimaryAction = "none"
)

// SummaryKey 四维状态派生出的稳定摘要语义键，供首页/模块中心/详情共用同一词汇。
type SummaryKey string

const (
	SummaryNotInstalled  SummaryKey = "not-installed"      // 未安装
	SummaryInstalledIdle SummaryKey = "installed-disabled" // 已安装，未启用
	SummaryRunning       SummaryKey = "running"            // 运行中
	SummaryRunningUpdate SummaryKey = "running-update"     // 运行中，有更新
	SummaryBlocked       SummaryKey = "blocked"            // 已阻止运行（撤回/不兼容/策略）
	SummaryFaulted       SummaryKey = "faulted"            // 异常，可重试或诊断
	SummaryBusyOperation SummaryKey = "in-progress"        // 安装/更新/卸载/修复进行中
	SummaryIdleEnabled   SummaryKey = "installed-enabled"  // 已安装，未运行
)

// ---------------------------------------------------------------------------
// 静态目录项
// ---------------------------------------------------------------------------

// Compatibility 声明模块可运行的宿主与平台约束（Phase 1-2 内建模块总是兼容）。
type Compatibility struct {
	HostRange string   `json:"hostRange"` // 宿主版本区间，"*" 表示任意当前宿主
	Platform  []string `json:"platform"`  // 目标平台（windows 等）
}

// ModuleCatalogItem 描述"模块是谁、如何交付、有哪些入口和能力"，
// 不承载瞬时进度；由装配根 Catalog 表（app/catalog.go）投影，
// 基线冻结于 scripts/fixture/module_catalog.json 并由契约测试守护。
type ModuleCatalogItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"` // 展示描述（与 ModuleInfo.Description 同源，模块中心卡片/搜索消费）
	Category    string `json:"category"`    // NavGroup 六组值之一；"core" 保留给控制平面
	// DeliveryKind 是静态交付形态（builtin-logical 等），与 ModuleState.Delivery
	// （安装生命周期维度）语义正交,故键名刻意区分,消费端禁止互相拿错。
	DeliveryKind  DeliveryKind  `json:"deliveryKind"`
	Capabilities  []string      `json:"capabilities"` // 能力标记（如 managed-versions/tray-commands）
	Entrypoints   []Entrypoint  `json:"entrypoints"`  // 该模块暴露的全部入口
	Compatibility Compatibility `json:"compatibility"`
	Permissions   []Permission  `json:"permissions"` // 需要的受控权限（Wave 0 允许为空）
	Owner         string        `json:"owner"`       // 维护 owner（官方清点口径，发布职责不可无主）
}

// IsCore 判定控制平面模块（不可卸载、不可停用）。
func (i ModuleCatalogItem) IsCore() bool { return i.Category == "core" }

// ---------------------------------------------------------------------------
// 四维状态投影
// ---------------------------------------------------------------------------

// ModuleState 是模块在当前设备上的四维事实 + 派生主操作。
// Registry 是唯一权威源；前端只缓存投影，不得持久化第二份真相。
type ModuleState struct {
	Schema        int           `json:"schema"` // ModuleContractSchema
	ModuleID      string        `json:"moduleId"`
	Delivery      DeliveryState `json:"delivery"`
	Policy        PolicyState   `json:"policy"`
	Runtime       RuntimeState  `json:"runtime"`
	Health        HealthState   `json:"health"`
	PrimaryAction PrimaryAction `json:"primaryAction"`
	Reason        string        `json:"reason,omitempty"` // ActionNone/受阻时的人类可读原因
	Summary       SummaryKey    `json:"summary"`          // 派生摘要语义键
}

// StateInput 提供状态机的四个输入维度；Project 计算派生字段并冻结投影。
type StateInput struct {
	ModuleID string
	Delivery DeliveryState
	Policy   PolicyState
	Runtime  RuntimeState
	Health   HealthState
}

// Project 依据 Wave 0 冻结的状态机表计算主操作、禁止原因与摘要键。
// 规则按优先级自上而下命中即停（与 docs/adr/ADR-0001 一一对应）：
//
//  1. 进行中的交付事务（installing/updating/removing/repairing）→ none + 进度语义；
//  2. orphaned/corrupt → repair；
//  3. revoked/incompatible/unverified/blocked/pending-consent → none + 原因；
//  4. absent → install；
//  5. disabled → enable；
//  6. mandatory/enabled + crashed/failed → retry；
//  7. 其余（installed + enabled/mandatory）→ open。
func (in StateInput) Project() ModuleState {
	s := ModuleState{
		Schema:   ModuleContractSchema,
		ModuleID: in.ModuleID,
		Delivery: in.Delivery,
		Policy:   in.Policy,
		Runtime:  in.Runtime,
		Health:   in.Health,
	}
	switch {
	case in.Delivery == DeliveryInstalling:
		s.PrimaryAction, s.Reason = ActionNone, "正在安装"
	case in.Delivery == DeliveryUpdating:
		s.PrimaryAction, s.Reason = ActionNone, "正在准备更新，当前版本仍可用"
	case in.Delivery == DeliveryRemoving:
		s.PrimaryAction, s.Reason = ActionNone, "正在卸载"
	case in.Delivery == DeliveryRepairing:
		s.PrimaryAction, s.Reason = ActionNone, "正在修复"
	case in.Delivery == DeliveryOrphaned:
		s.PrimaryAction, s.Reason = ActionRepair, "本机内容与安装记录不一致，需要修复"
	case in.Health == HealthCorrupt:
		s.PrimaryAction, s.Reason = ActionRepair, "安装内容校验不一致，需要修复"
	case in.Health == HealthRevoked:
		s.PrimaryAction, s.Reason = ActionNone, "版本已被官方撤回"
	case in.Health == HealthIncompatible:
		s.PrimaryAction, s.Reason = ActionNone, "与当前宿主或平台不兼容"
	case in.Health == HealthUnverified:
		s.PrimaryAction, s.Reason = ActionNone, "缺少有效的官方验证，暂不可用"
	case in.Policy == PolicyBlocked:
		s.PrimaryAction, s.Reason = ActionNone, "已被安全策略阻止"
	case in.Policy == PolicyPendingConsent:
		s.PrimaryAction, s.Reason = ActionNone, "等待权限或数据迁移确认"
	case in.Delivery == DeliveryAbsent:
		s.PrimaryAction = ActionInstall
	case in.Policy == PolicyDisabled:
		s.PrimaryAction = ActionEnable
	case in.Runtime == RuntimeCrashed || in.Runtime == RuntimeFailed:
		s.PrimaryAction = ActionRetry
	default:
		s.PrimaryAction = ActionOpen
	}
	s.Summary = deriveSummary(in)
	return s
}

// deriveSummary 按模块分发专项 §6.5 的派生口径输出摘要键。
func deriveSummary(in StateInput) SummaryKey {
	switch {
	case in.Delivery == DeliveryInstalling || in.Delivery == DeliveryUpdating ||
		in.Delivery == DeliveryRemoving || in.Delivery == DeliveryRepairing ||
		in.Delivery == DeliveryOrphaned:
		// updating 特例：旧版本仍可用时按运行中呈现，其余进行中事务统一 in-progress。
		if in.Delivery == DeliveryUpdating && in.Runtime == RuntimeActive {
			return summaryRunningFamily(in.Health)
		}
		return SummaryBusyOperation
	case in.Delivery == DeliveryAbsent:
		return SummaryNotInstalled
	case in.Health == HealthRevoked || in.Health == HealthIncompatible || in.Policy == PolicyBlocked:
		return SummaryBlocked
	case in.Policy == PolicyDisabled:
		return SummaryInstalledIdle
	case in.Runtime == RuntimeActive || in.Runtime == RuntimeBusy || in.Runtime == RuntimeActivating:
		return summaryRunningFamily(in.Health)
	case in.Runtime == RuntimeCrashed || in.Runtime == RuntimeFailed:
		return SummaryFaulted
	default:
		return SummaryIdleEnabled
	}
}

func summaryRunningFamily(health HealthState) SummaryKey {
	if health == HealthUpdateAvailable {
		return SummaryRunningUpdate
	}
	return SummaryRunning
}

// Visible 判定某入口是否可展示/放行：与"未安装或停用不得旁路"的裁决一致——
//
//	installed ∧ (enabled ∨ mandatory) ∧ 非 blocked。
//
// 运行状态不影响可见性（懒激活是入口自身职责）；健康撤回类以 blocked 语义收口，
// 由 Project 的状态机先行拦截。
func (s ModuleState) Visible() bool {
	return s.Delivery == DeliveryInstalled &&
		(s.Policy == PolicyEnabled || s.Policy == PolicyMandatory)
}

// ---------------------------------------------------------------------------
// 状态转换合法性（非法转换为 0 的 DoD 守卫）
// ---------------------------------------------------------------------------

// 各维度允许的稳态转换；同态（from==to）一律合法（幂等重放）。
var (
	deliveryTransitions = map[DeliveryState][]DeliveryState{
		DeliveryAbsent:     {DeliveryInstalling},
		DeliveryInstalling: {DeliveryInstalled, DeliveryAbsent}, // 成功或失败回退
		DeliveryInstalled:  {DeliveryUpdating, DeliveryRemoving, DeliveryRepairing, DeliveryOrphaned},
		DeliveryUpdating:   {DeliveryInstalled, DeliveryOrphaned},
		DeliveryRemoving:   {DeliveryAbsent, DeliveryOrphaned},
		DeliveryRepairing:  {DeliveryInstalled, DeliveryOrphaned},
		DeliveryOrphaned:   {DeliveryRepairing, DeliveryRemoving, DeliveryAbsent},
	}
	policyTransitions = map[PolicyState][]PolicyState{
		PolicyEnabled:        {PolicyDisabled, PolicyBlocked, PolicyPendingConsent},
		PolicyDisabled:       {PolicyEnabled, PolicyBlocked, PolicyPendingConsent},
		PolicyBlocked:        {PolicyEnabled, PolicyDisabled},
		PolicyPendingConsent: {PolicyEnabled, PolicyDisabled, PolicyBlocked},
		PolicyMandatory:      {}, // Core 不允许策略迁移
	}
	runtimeTransitions = map[RuntimeState][]RuntimeState{
		RuntimeInactive:   {RuntimeActivating},
		RuntimeActivating: {RuntimeActive, RuntimeFailed, RuntimeInactive},
		RuntimeActive:     {RuntimeBusy, RuntimeStopping, RuntimeInactive, RuntimeCrashed},
		RuntimeBusy:       {RuntimeActive, RuntimeStopping, RuntimeCrashed},
		RuntimeStopping:   {RuntimeInactive, RuntimeFailed, RuntimeCrashed},
		RuntimeCrashed:    {RuntimeActivating, RuntimeInactive},
		RuntimeFailed:     {RuntimeActivating, RuntimeInactive},
	}
)

// CanDeliveryTransition / CanPolicyTransition / CanRuntimeTransition 供
// Registry 与事务引擎在变更前校验；Health 为观察值不设转换门。
func CanDeliveryTransition(from, to DeliveryState) bool {
	return transitionAllowed(deliveryTransitions, from, to)
}
func CanPolicyTransition(from, to PolicyState) bool {
	return transitionAllowed(policyTransitions, from, to)
}
func CanRuntimeTransition(from, to RuntimeState) bool {
	return transitionAllowed(runtimeTransitions, from, to)
}

func transitionAllowed[T comparable](table map[T][]T, from, to T) bool {
	if from == to {
		return true
	}
	return slices.Contains(table[from], to)
}

// ---------------------------------------------------------------------------
// Operation：长操作与调用租约契约（引擎按 Wave 3/4 落地）
// ---------------------------------------------------------------------------

// OperationKind 操作类型。invoke 表示业务调用租约（Acquire 产物），
// 生命周期操作（install/remove/…）与业务调用共享同一观察协议。
type OperationKind string

const (
	OpInstall  OperationKind = "install"
	OpActivate OperationKind = "activate"
	OpStop     OperationKind = "stop"
	OpUpdate   OperationKind = "update"
	OpRollback OperationKind = "rollback"
	OpRepair   OperationKind = "repair"
	OpRemove   OperationKind = "remove"
	OpInvoke   OperationKind = "invoke"
)

// OperationStatus 操作终态机。
type OperationStatus string

const (
	OpQueued    OperationStatus = "queued"
	OpRunning   OperationStatus = "running"
	OpSucceeded OperationStatus = "succeeded"
	OpFailed    OperationStatus = "failed"
	OpCancelled OperationStatus = "cancelled"
)

// OperationError 结构化错误：code 稳定可枚举，recoverable 决定是否引导重试。
type OperationError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

// Operation 统一表达安装、启停、更新、回滚、修复、卸载与业务调用的生命周期；
// 同一 Operation 应能被模块中心详情、首页最近任务与诊断视图观察，
// 前端重载后能从后端恢复，而非依赖组件内存。
type Operation struct {
	Schema int    `json:"schema"`
	ID     string `json:"id"`
	// TxnID 是支撑该操作的 journal 事务裸 ID（可空=纯内存操作无账本）。
	// resumable 回灌记录的 ID 是展示用合成键，DismissResumable 等收口通道
	// 一律消费本字段——前端禁止从 ID 剥前缀反推事务。
	TxnID       string          `json:"txnId,omitempty"`
	ModuleID    string          `json:"moduleId"`
	Kind        OperationKind   `json:"kind"`
	Phase       string          `json:"phase"`
	Status      OperationStatus `json:"status"`
	Progress    *int            `json:"progress,omitempty"` // 0-100；nil=不可量化
	Cancellable bool            `json:"cancellable"`
	StartedAt   string          `json:"startedAt"` // RFC3339
	FinishedAt  string          `json:"finishedAt,omitempty"`
	Error       *OperationError `json:"error,omitempty"`
}
