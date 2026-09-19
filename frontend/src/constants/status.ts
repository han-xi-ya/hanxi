// 托管引擎状态与开发环境检测状态的语义单一表（重构蓝图 §4：constants/status）。
// tone 词表与 styles/components.css 的 .chip-{tone}/.banner-{tone} 严格对齐，
// 是 Phase 3/4 迁移时 UiStatusChip / StatePanel 的语义源——视图文案不得再自造。
//
// ── 与四维状态词表的关系（Wave 0 契约冻结）──
// 本文件下半部分是模块四维状态（delivery/policy/runtime/health）+ 主操作 + 摘要的
// 统一词汇表，JSON 字面量与后端 internal/extapi/catalog.go 冻结契约逐字对齐：
// 同一业务状态在首页/模块中心/详情/导航必须共用此处的文案与色调。
// 上方 TOOL_STATE_META / ENV_STATUS_META 为历史视图表（icon 仍是 ●◐○ 占位字符），
// Wave 3 收口时并入四维词表口径，本次不动、不改其消费方。
// 纪律：状态不只依赖颜色——词表每项带 icon 语义（icons.ts 注册名）+ 中文文字 + tone。

import type { IconName } from './icons'

export type StateTone = 'positive' | 'information' | 'warning' | 'danger' | 'neutral'

export interface StateMeta {
  text: string
  /** 占位字符图标；将被 AppIcon 内联 SVG 替换（设计纪律：字符/emoji 不作主图标）。 */
  icon: string
  tone: StateTone
}

/**
 * 托管引擎快照状态（instance.Snapshot.state）通用语义。
 * 视图的更细文案（如 MarkerOn running+drawing 的"标注已开启"）在此之上按业务扩展，
 * 未识别状态一律回退 stopped——状态不明时宁可报"无"，不误报在跑。
 */
export const TOOL_STATE_META: Record<string, StateMeta> = {
  running: { text: '运行中', icon: '●', tone: 'positive' }, // §9.5-5 文案裁决：状态标签统一「运行中」；「已启动」仅存于后端动作回执 toast，不再作状态词
  starting: { text: '启动中…', icon: '◐', tone: 'information' },
  stopped: { text: '未运行', icon: '○', tone: 'neutral' },
  failed: { text: '异常退出', icon: '!', tone: 'danger' },
  external: { text: '外部运行', icon: '◍', tone: 'warning' },
}

/**
 * envcheck 工具检测状态。text/icon 自 EnvCheckView 内联 STATUS_META 原样移植
 * （Phase 4 收编后删除该视图副本），旧 cls 字段由 tone 取代。
 */
export const ENV_STATUS_META: Record<string, StateMeta> = {
  installed: { text: '已安装', icon: '✓', tone: 'positive' },
  missing: { text: '未安装', icon: '○', tone: 'neutral' },
  error: { text: '检测失败', icon: '!', tone: 'danger' },
  'store-stub': { text: '商店存根', icon: '⚠', tone: 'warning' },
}

/** 托管状态 → 语义元信息；未知键回退 stopped（对齐托管视图 stateText 的 default 分支）。 */
export function toolStateMeta(state: string): StateMeta {
  return TOOL_STATE_META[state] ?? TOOL_STATE_META.stopped
}

/** 检测状态 → 语义元信息；未知键回退 error（对齐 EnvCheckView metaOf 的兜底口径）。 */
export function envStatusMeta(status: string): StateMeta {
  return ENV_STATUS_META[status] ?? ENV_STATUS_META.error
}

// ---------------------------------------------------------------------------
// 模块四维状态统一词表（Wave 0 契约冻结的前端词汇部分）
// 枚举值全集逐字对齐 internal/extapi/catalog.go 的 JSON 字面量；
// 此处仅登记"值 → 展示语义"的词表，业务状态机仍归后端，绑定类型等
// wails3 重新生成后另行 import，不在本文件手写。
// ---------------------------------------------------------------------------

/** 交付维度：本机是否持有可运行的安装凭据/资产。 */
export type DeliveryStateValue =
  | 'absent' | 'installing' | 'installed' | 'updating' | 'removing' | 'repairing' | 'orphaned'

/** 策略维度：用户/平台是否允许使用。 */
export type PolicyStateValue =
  | 'enabled' | 'disabled' | 'blocked' | 'pending-consent' | 'mandatory'

/** 运行维度：当前是否占用运行资源。 */
export type RuntimeStateValue =
  | 'inactive' | 'activating' | 'active' | 'busy' | 'stopping' | 'crashed' | 'failed'

/** 健康与版本维度：本机内容与官方目录的一致性判断。 */
export type HealthStateValue =
  | 'current' | 'update-available' | 'pinned' | 'incompatible' | 'corrupt'
  | 'revoked' | 'unverified' | 'degraded' | 'offline-stale'

/** 主操作语义键（catalog.go PrimaryAction）。 */
export type PrimaryActionValue =
  | 'install' | 'enable' | 'disable' | 'open' | 'update' | 'retry' | 'repair' | 'uninstall' | 'none'

/** 四维派生摘要语义键（catalog.go SummaryKey）。 */
export type SummaryKeyValue =
  | 'not-installed' | 'installed-disabled' | 'installed-enabled' | 'running'
  | 'running-update' | 'blocked' | 'faulted' | 'in-progress'

/** UiButton 的 variant 词表（与 components/ui/UiButton.vue 私有同名类型保持一致，Wave 3 收口统一为本文件导出源）。 */
export type UiButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost'

/** 四维词表条目的展示语义：icon 必须是 icons.ts 注册名（AppIcon 渲染，非占位字符）。 */
export interface SemanticMeta {
  text: string
  tone: StateTone
  icon: IconName
}

/** 未知/缺失状态的统一兜底：不抛错，报"状态未知"，中性、问号图标。 */
const UNKNOWN_STATE_META: SemanticMeta = { text: '状态未知', tone: 'neutral', icon: 'help-circle' }

/** 交付维度 → 展示语义。 */
export const DELIVERY_META = {
  absent: { text: '未安装', tone: 'neutral', icon: 'circle' },
  installing: { text: '正在安装…', tone: 'information', icon: 'download' },
  installed: { text: '已安装', tone: 'positive', icon: 'box' },
  updating: { text: '正在更新…', tone: 'information', icon: 'refresh-cw' },
  removing: { text: '正在卸载…', tone: 'warning', icon: 'trash-2' },
  repairing: { text: '正在修复…', tone: 'information', icon: 'wrench' },
  orphaned: { text: '本机内容与安装记录不一致', tone: 'warning', icon: 'alert-triangle' },
} satisfies Record<DeliveryStateValue, SemanticMeta>

/** 策略维度 → 展示语义。 */
export const POLICY_META = {
  enabled: { text: '已启用', tone: 'positive', icon: 'check-square' },
  disabled: { text: '已停用', tone: 'neutral', icon: 'circle' },
  blocked: { text: '已被策略阻止', tone: 'danger', icon: 'x-octagon' },
  'pending-consent': { text: '待你确认授权', tone: 'warning', icon: 'clock' },
  mandatory: { text: '平台强制启用，不可停用', tone: 'information', icon: 'shield' },
} satisfies Record<PolicyStateValue, SemanticMeta>

/** 运行维度 → 展示语义。 */
export const RUNTIME_META = {
  inactive: { text: '未运行', tone: 'neutral', icon: 'circle' },
  activating: { text: '正在启动…', tone: 'information', icon: 'zap' },
  active: { text: '运行中', tone: 'positive', icon: 'activity' },
  busy: { text: '正忙，处理中…', tone: 'information', icon: 'cpu' },
  stopping: { text: '正在停止…', tone: 'information', icon: 'power' },
  crashed: { text: '意外崩溃退出', tone: 'danger', icon: 'alert-triangle' },
  failed: { text: '启动失败，可重试', tone: 'danger', icon: 'x-octagon' },
} satisfies Record<RuntimeStateValue, SemanticMeta>

/** 健康与版本维度 → 展示语义。 */
export const HEALTH_META = {
  current: { text: '版本正常', tone: 'positive', icon: 'check-square' },
  'update-available': { text: '有可用更新', tone: 'information', icon: 'arrow-up-circle' },
  pinned: { text: '已固定当前版本', tone: 'neutral', icon: 'pin' },
  incompatible: { text: '与宿主环境不兼容', tone: 'danger', icon: 'unlink' },
  corrupt: { text: '内容校验不一致，需修复', tone: 'danger', icon: 'alert-triangle' },
  revoked: { text: '版本已被官方撤回', tone: 'danger', icon: 'x-octagon' },
  unverified: { text: '未通过官方验证', tone: 'warning', icon: 'help-circle' },
  degraded: { text: '可用但功能降级', tone: 'warning', icon: 'gauge' },
  'offline-stale': { text: '离线，版本信息可能过期', tone: 'warning', icon: 'wifi-off' },
} satisfies Record<HealthStateValue, SemanticMeta>

/** 主操作 → 按钮语义（variant 对齐 UiButton；none 渲染为 ghost 且禁用）。 */
export interface PrimaryActionMeta {
  label: string
  variant: UiButtonVariant
  /** true 时按钮必须禁用（仅 none）。 */
  disabled: boolean
}

export const PRIMARY_ACTION_META = {
  install: { label: '安装', variant: 'primary', disabled: false },
  enable: { label: '启用', variant: 'primary', disabled: false },
  open: { label: '打开', variant: 'primary', disabled: false },
  update: { label: '更新', variant: 'secondary', disabled: false },
  repair: { label: '修复', variant: 'secondary', disabled: false },
  retry: { label: '重试', variant: 'secondary', disabled: false },
  disable: { label: '停用', variant: 'secondary', disabled: false },
  uninstall: { label: '卸载', variant: 'danger', disabled: false },
  none: { label: '无可用操作', variant: 'ghost', disabled: true },
} satisfies Record<PrimaryActionValue, PrimaryActionMeta>

/** 摘要键 → 首页/模块中心/详情共用的一句话短语。 */
export const SUMMARY_META = {
  'not-installed': '未安装',
  'installed-disabled': '已安装，未启用',
  'installed-enabled': '已安装，未运行',
  running: '运行中',
  'running-update': '运行中，有更新',
  blocked: '已被阻止运行',
  faulted: '异常，可重试',
  'in-progress': '操作进行中',
} satisfies Record<SummaryKeyValue, string>

/** 交付维度取值 → 语义；未知值兜底"状态未知"，不抛错。 */
export function deliveryMeta(value: string): SemanticMeta {
  return DELIVERY_META[value as DeliveryStateValue] ?? UNKNOWN_STATE_META
}

/** 策略维度取值 → 语义；未知值兜底"状态未知"，不抛错。 */
export function policyMeta(value: string): SemanticMeta {
  return POLICY_META[value as PolicyStateValue] ?? UNKNOWN_STATE_META
}

/** 运行维度取值 → 语义；未知值兜底"状态未知"，不抛错。 */
export function runtimeMeta(value: string): SemanticMeta {
  return RUNTIME_META[value as RuntimeStateValue] ?? UNKNOWN_STATE_META
}

/** 健康维度取值 → 语义；未知值兜底"状态未知"，不抛错。 */
export function healthMeta(value: string): SemanticMeta {
  return HEALTH_META[value as HealthStateValue] ?? UNKNOWN_STATE_META
}

/**
 * update-available 展示短语：投影带 remoteVersion 时追加" → 新版本号"，
 * 无版本记录（旧缓存迁移/感知未拿到版本）原样返回词表文案——绝不编造版本。
 * 徽标/详情行/首页更新列表共用此单一口径，保证跨页一致。
 */
export function updateAvailableText(version?: string | null): string {
  const base = HEALTH_META['update-available'].text
  const v = String(version ?? '')
  return v ? `${base} → ${v}` : base
}

// ---------------------------------------------------------------------------
// 统一 Operation 观察面词表（Wave 4）
// kind/status 两族取值逐字对齐 bindings 的 OperationKind / OperationStatus 枚举；
// phase 是后端字符串词汇（internal/ops：开账写 "resolve"、事务步骤迁移写步骤名
// ——由各模块 InstallSteps 清单登记，当前样本为 download/verify/unpack/place、
// 收口写 "done"），故未知/自定义步骤名原样透出不谎报"状态未知"。
// 纪律：本文件只登记"值 → 展示语义"，操作状态机真相仍全在后端 journal/Hub。
// ---------------------------------------------------------------------------

/** 操作种类取值（extapi.OperationKind）。 */
export type OperationKindValue =
  | 'install' | 'activate' | 'stop' | 'update' | 'rollback' | 'repair' | 'remove' | 'invoke'

/** 操作状态取值（extapi.OperationStatus：queued/running 为在途，其余为终态）。 */
export type OperationStatusValue =
  | 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'

/** 事务阶段取值（后端 internal/ops 词汇：开账 resolve + 步骤名 + 收口 done）。 */
export type OperationPhaseValue =
  | 'resolve' | 'download' | 'verify' | 'unpack' | 'place' | 'done'

/** 操作种类 → 展示语义（首页/模块中心/详情共用；invoke 为业务调用租约）。 */
export const OPERATION_KIND_META = {
  install: { text: '安装', tone: 'information', icon: 'download' },
  activate: { text: '启动', tone: 'positive', icon: 'zap' },
  stop: { text: '停止', tone: 'neutral', icon: 'power' },
  update: { text: '更新', tone: 'information', icon: 'refresh-cw' },
  rollback: { text: '回滚', tone: 'warning', icon: 'chevrons-left' },
  repair: { text: '修复', tone: 'information', icon: 'wrench' },
  remove: { text: '卸载', tone: 'danger', icon: 'trash-2' },
  invoke: { text: '调用', tone: 'neutral', icon: 'terminal' },
} satisfies Record<OperationKindValue, SemanticMeta>

/** 操作状态 → 展示语义（图标按状态族固定：成功=对勾、失败=警示、取消=电源）。 */
export const OPERATION_STATUS_META = {
  queued: { text: '排队中', tone: 'neutral', icon: 'clock' },
  running: { text: '进行中', tone: 'information', icon: 'activity' },
  succeeded: { text: '已完成', tone: 'positive', icon: 'check-square' },
  failed: { text: '失败', tone: 'danger', icon: 'alert-triangle' },
  cancelled: { text: '已取消', tone: 'neutral', icon: 'power' },
} satisfies Record<OperationStatusValue, SemanticMeta>

/** 事务阶段 → 中文短语（进度行与"上次中断于"文案共用）。 */
export const PHASE_META = {
  resolve: '解析版本',
  download: '下载',
  verify: '校验',
  unpack: '解压',
  place: '落位',
  done: '完成',
} satisfies Record<OperationPhaseValue, string>

/** 未知操作种类的统一兜底：不抛错，如实报"未知操作"。 */
const UNKNOWN_OPERATION_KIND: SemanticMeta = { text: '未知操作', tone: 'neutral', icon: 'help-circle' }

/** 操作种类取值 → 语义；未知值兜底"未知操作"，不抛错。 */
export function operationKindMeta(value: string): SemanticMeta {
  return OPERATION_KIND_META[value as OperationKindValue] ?? UNKNOWN_OPERATION_KIND
}

/** 操作状态取值 → 语义；未知值兜底"状态未知"，不抛错。 */
export function operationStatusMeta(value: string): SemanticMeta {
  return OPERATION_STATUS_META[value as OperationStatusValue] ?? UNKNOWN_STATE_META
}

/** 事务阶段取值 → 中文；未登记步骤名原样透出（后端步骤清单按模块自定义，不做假映射）。 */
export function operationPhaseText(value: string): string {
  return PHASE_META[value as OperationPhaseValue] ?? value
}

/** 能力入口类型（catalog.go Entrypoint）：导航/搜索/托盘/热键等所有产品面共享的入口词汇。 */
export type EntrypointValue =
  | 'rpc' | 'navigation' | 'search' | 'tray' | 'hotkey' | 'mcp' | 'window' | 'background'

/** 入口取值 → 中文标签：前端入口词汇单一来源（最简词表，消费方需要 tone/icon 时再扩展）。 */
export const ENTRYPOINT_META = {
  rpc: { label: '服务调用' },
  navigation: { label: '导航入口' },
  search: { label: '命令搜索' },
  tray: { label: '托盘菜单' },
  hotkey: { label: '全局热键' },
  mcp: { label: 'MCP 工具' },
  window: { label: '独立窗口' },
  background: { label: '后台常驻' },
} satisfies Record<EntrypointValue, { label: string }>
