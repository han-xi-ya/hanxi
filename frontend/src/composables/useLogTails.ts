// 日志面板家族公共件（2026-10-26 审查 A 区台账 §五-7 收编）：
// FrpcProjectsView 日志抽屉 / DdnsGoView dd-log 区 / LogsView 主日志页的逐字共同逻辑
// 单源——ANSI 剥离、warn/error 着色判定形制、"贴底自动滚动"阈值
// （scrollTop+clientHeight >= scrollHeight-40，原为 frpc/ddns-go 两处逐字内联表达式）、
// 满仓裁头（保尾不保头）。
//
// 逐字对账后的分叉登记（刻意不统一，保留为参数或视图侧原样）：
//  - 台账所称"三份逐字"实况为两处（frpc 抽屉 + ddns-go dd-log）；LogsView 无该阈值
//    表达式——其刷新贴底恒无条件（无自动滚动闩），仅共用 pinToBottom 落底动作。
//  - ANSI 剥离实况仅 frpc 一份；ddns-go/LogsView 原样直出，强加剥离会改可见文本与
//    复制内容，不收编。
//  - 着色词表三家互异：frpc 认 [W]/WARN/error/fail（对已剥 ANSI 的文本判定）；
//    ddns-go 认中英成败词并带否定档"未变化/no change"；LogsView 整页 <code> 纯文本
//    无逐行着色——加色即改可见行为，不收编。词表在本件收紧为命名参数（*_RE 导出），
//    判定结构（正表命中且不被负表豁免）由 createWarnMatcher 单源。
//  - 缓冲上限与入表节奏留在视图：frpc 2000 行 rAF 合帧（spec 锁"帧前不可见"）、
//    ddns-go 400 行逐行即时（spec 锁 nextTick 后可见）、LogsView 容量由后端
//    ReadLogContent(maxLines) 截尾。本件只吃 maxLines 参数负责裁头公式本身。
import { nextTick, ref } from 'vue'
import type { Ref } from 'vue'

/** 贴底判定阈值（px）：距底 ≤ 该值视为"用户在跟尾"，自动滚动保持咬合。 */
export const LOG_BOTTOM_THRESHOLD = 40

/** isNearBottom 的结构化入参：HTMLElement 天然满足，测试可用裸对象断外部事实。 */
export interface ScrollBottomMetrics {
  scrollTop: number
  clientHeight: number
  scrollHeight: number
}

/** 剥离 SGR  ANSI 色码（\x1b[1;34m 等），非色码 CSI（如 \x1b[2K）与任意文本原样保留。 */
export function stripAnsi(s: string): string {
  return s.replace(/\x1b\[[\d;]*m/g, '')
}

/** 滚动可视区是否贴底（距底 ≤ threshold 记为真，`>=` 含恰等边界）。 */
export function isNearBottom(el: ScrollBottomMetrics, threshold: number = LOG_BOTTOM_THRESHOLD): boolean {
  return el.scrollTop + el.clientHeight >= el.scrollHeight - threshold
}

/** 无条件落底（LogsView 形制：刷新即贴底，不问用户是否已上翻）。 */
export function pinToBottom(el: Pick<ScrollBottomMetrics, 'scrollTop' | 'scrollHeight'>): void {
  el.scrollTop = el.scrollHeight
}

/**
 * 满仓裁头（原地）：超过 cap 时从头部丢弃多余项、保留最近 cap 条，返回同一引用。
 * 原地性是被消费的契约——ddns-go 的 logLines 是 ref 数组，push+splice 原位变异走
 * 深层 reactive 触发；换成新数组会改写另一处视图的变异节奏。
 */
export function trimToCap<T>(lines: T[], cap: number): T[] {
  if (lines.length > cap) lines.splice(0, lines.length - cap)
  return lines
}

/** 切尾副本（不改源数组）：历史整表置换用（wire 数组不得与视图缓冲别名互污）。 */
export function keepLastLines<T>(lines: T[], cap: number): T[] {
  return lines.slice(-cap)
}

/** frpc 抽屉着色正表（接收期对已剥 ANSI 的行文本判定）。 */
export const FRPC_LOG_WARN_RE = /\[W\]|WARN|error|fail/i
/** ddns-go 着色正表（上游输出含中英文两种语言）。 */
export const DDNS_LOG_WARN_RE = /失败|错误|异常|error|fail|refused|timeout/i
/** ddns-go 着色负表（"未变化/no change" 属轮询常态，命中正表也豁免）。 */
export const DDNS_LOG_WARN_EXCLUDE_RE = /未变化|no change/i

/**
 * 着色判定工厂（结构单源、词表为参）：命中正表且（如给定）不被负表豁免。
 * 正/负表均为非全局 /i 正则，.test 无状态，跨行复用同一实例与逐字新建正则等价。
 */
export function createWarnMatcher(positive: RegExp, negative?: RegExp): (line: string) => boolean {
  return (line: string) => positive.test(line) && !(negative?.test(line) ?? false)
}

export interface UseLogTailsOptions {
  /** 缓冲上限（行数）：视图各自保留常量声明后传入，裁头公式在此单源。 */
  maxLines: number
  /** 贴底阈值覆写（默认 LOG_BOTTOM_THRESHOLD=40，现两消费视图均吃默认档）。 */
  bottomThreshold?: number
}

export interface LogTails {
  /** 自动滚动闩（默认开；视图 checkbox v-model 直绑）。 */
  autoScroll: Ref<boolean>
  /** 滚动容器模板 ref（视图模板 ref="bodyRef 解构名" 挂上）。 */
  bodyRef: Ref<HTMLElement | null>
  /** 滚动事件回写闩（模板 @scroll.passive="onBodyScroll"，与原内联表达式等价）。 */
  onBodyScroll(event: Event): void
  /** 闩控贴底：nextTick 后若闩开且容器在场则落底（frpc/ddns-go 逐字共用形制）。 */
  scrollToBottom(): Promise<void>
  /** 原地裁头（cap=maxLines），供 push 增量入表后调用。 */
  trimHead<T>(lines: T[]): T[]
  /** 切尾副本（cap=maxLines），供历史整表置换调用。 */
  keepLast<T>(lines: T[]): T[]
}

/**
 * 日志尾跟随器：自动滚动闩 + 贴底阈值 + 满仓裁头公式的每实例装配。
 * 只管机构不管数据——行缓冲的形状（string[] / 预解析 LogRow[]）与入表节奏
 * （逐行即时 / rAF 合帧）留在视图侧，此处仅收编两视图逐字相同的四块机构。
 */
export function useLogTails(options: UseLogTailsOptions): LogTails {
  const maxLines = options.maxLines
  const bottomThreshold = options.bottomThreshold ?? LOG_BOTTOM_THRESHOLD
  const autoScroll = ref(true)
  const bodyRef = ref<HTMLElement | null>(null)

  function onBodyScroll(event: Event): void {
    const el = event.target as HTMLElement | null
    // 原内联表达式经 as HTMLElement 直读字段；target 在场性由 DOM scroll 事件天然保证，
    // 这里的空守卫仅为消除非 DOM 场景的理论崩点，不改变任何真实滚动语义。
    if (el) autoScroll.value = isNearBottom(el, bottomThreshold)
  }

  async function scrollToBottom(): Promise<void> {
    await nextTick()
    const el = bodyRef.value
    if (el && autoScroll.value) el.scrollTop = el.scrollHeight
  }

  return {
    autoScroll,
    bodyRef,
    onBodyScroll,
    scrollToBottom,
    trimHead: <T>(lines: T[]): T[] => trimToCap(lines, maxLines),
    keepLast: <T>(lines: T[]): T[] => keepLastLines(lines, maxLines),
  }
}
