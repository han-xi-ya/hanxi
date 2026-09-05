// 托管引擎状态与开发环境检测状态的语义单一表（重构蓝图 §4：constants/status）。
// tone 词表与 styles/components.css 的 .chip-{tone}/.banner-{tone} 严格对齐，
// 是 Phase 3/4 迁移时 UiStatusChip / StatePanel 的语义源——视图文案不得再自造。

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
  running: { text: '已启动', icon: '●', tone: 'positive' },
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
