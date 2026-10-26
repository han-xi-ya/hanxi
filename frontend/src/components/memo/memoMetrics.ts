// 随手记 · 度量与纯函数单源模块（模式照抄留言板 boardMetrics 先例）。
//
// 收录门槛：真被两处以上需要，或有独立测试价值；MemoView/QuickMemoSheet/
// MemoCard/MemoEditorPane 等消费方不留内联副本。
//   - groupMemoItems/dayBucket/fmtAgo/cardPreview：MemoCard 呈现件与主视图分组
//     标题两处消费，日期边界（7/30 天切点、跨午夜、非法 ISO 串）有独立测试价值；
//   - parseTagTokens：主窗速记行（MemoView.parseQuickTags）与速记卡
//     （QuickMemoSheet.commit）共用，曾两份逐字重复、已删重；
//   - flattenPastedLines：多行粘贴并条为速记卡与主窗速记行两面共用；
//   - memoColorHex/MEMO_COLOR_HEX：MemoCard 左封边与 MemoEditorPane 色板圆点
//     皆吃此单源——色彩标识是用户数据值（colorTag 持久化进后端），非主题表面，
//     五个十六进制色沿用视图治理注记的豁免口径，不算新色值。
// 刻意不收编的：「高亮分段」在现视图与 markdown.ts 中都不存在（搜索命中
// 目前后端过滤、前端不高亮），为不存在的第二消费方抽函数即"为抽而抽"，不预造。
//
// 零 Vue、零 DOM、零后端绑定，可独立测试。

import { fmtDate } from '../../utils/format'

/** 分组顺序与桶名（六档口径单源，主视图档头文案锚此） */
export const MEMO_GROUP_ORDER = ['置顶', '今天', '昨天', '7 天内', '30 天内', '更早'] as const

/** 色彩标识 → 十六进制（用户数据色板：blue/emerald/amber/rose/purple） */
export const MEMO_COLOR_HEX: Record<string, string> = {
  blue: '#3b82f6',
  emerald: '#10b981',
  amber: '#f59e0b',
  rose: '#f43f5e',
  purple: '#8b5cf6',
}
/** 未知/缺省色彩标识的兜底档（主视图页面板同口径：colorTag || 'blue'） */
export const MEMO_COLOR_FALLBACK = 'blue'

/** 便签色彩标识换十六进制色；未知值（含历史 green 等）与空串一律回落 blue。 */
export function memoColorHex(color?: string | null): string {
  return MEMO_COLOR_HEX[color ?? ''] ?? MEMO_COLOR_HEX[MEMO_COLOR_FALLBACK]
}

/**
 * 自然日档切桶：按本地日历日零点差取整——今天（含未来/同日，days<=0）、昨天、
 * 7 天内、30 天内、更早；非法时间串落「更早」（不给渲染层抛 NaN）。
 * now 可注入（测试钉边界；分组一次遍历共用同一 now，杜绝逐条各取
 * new Date() 可能跨午夜分裂的隐患）。
 */
export function dayBucket(iso: string, now: Date = new Date()): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '更早'
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const days = Math.round((midnight(now) - midnight(d)) / 86400000)
  if (days <= 0) return '今天'
  if (days === 1) return '昨天'
  if (days <= 7) return '7 天内'
  if (days <= 30) return '30 天内'
  return '更早'
}

/** groupMemoItems 消费的最小条目形态（MemoItem 结构子集，不 import 后端绑定类型） */
export interface MemoLike {
  isPinned: boolean
  updatedAt: string
}

/** 时间分组桶：key/label 同值（key 供 :key 与接线稳定标识，label 供标题呈现） */
export interface MemoGroup<T> {
  key: string
  label: string
  items: T[]
}

/**
 * 置顶→今天→昨天→7 天内→30 天内→更早 的分桶遍历：置顶条目不论日期先进
 * 「置顶」桶；空桶不出现在结果里。
 * 输入顺序即桶内顺序（后端 sortBy=updated 的降序直传即保序）。
 */
export function groupMemoItems<T extends MemoLike>(
  items: readonly T[],
  now: Date = new Date(),
): MemoGroup<T>[] {
  const map = new Map<string, T[]>()
  for (const m of items) {
    const key = m.isPinned ? '置顶' : dayBucket(m.updatedAt, now)
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(m)
  }
  return (MEMO_GROUP_ORDER as readonly string[])
    .filter((k) => map.has(k))
    .map((k) => ({ key: k, label: k, items: map.get(k)! }))
}

/**
 * 相对时间：刚刚 / N 分钟前 / N 小时前，超过 24 小时回落 fmtDate 的
 * yyyy-MM-dd；非法串给「—」。now 可注入测试。
 */
export function fmtAgo(iso: string, now: Date = new Date()): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '—'
  const min = Math.floor((now.getTime() - t) / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  if (min < 24 * 60) return `${Math.floor(min / 60)} 小时前`
  return fmtDate(iso)
}

/** 卡片摘要行数上限（默认档） */
export const CARD_PREVIEW_LINES = 5

/**
 * 卡片摘要行提取：剔除空白行后取前 maxLines 行、换行拼接；被截断时尾部补
 * 「\n…」。全空白正文得空串（呈现层负责占位文案）。
 */
export function cardPreview(content: string, maxLines: number = CARD_PREVIEW_LINES): string {
  const lines = content.split('\n').filter((l) => l.trim() !== '')
  if (lines.length === 0) return ''
  return lines.slice(0, maxLines).join('\n') + (lines.length > maxLines ? '\n…' : '')
}

/** 行形态摘要：首个非空白行（列表变体的单行预览，超长截断交给 CSS ellipsis）。 */
export function firstMeaningfulLine(content: string): string {
  for (const l of content.split('\n')) {
    if (l.trim() !== '') return l
  }
  return ''
}

/**
 * 速记标签解析（主窗速记行与速记卡共用）：空白/逗号/顿号/分号（中英）分词、
 * 自动补 #、去重保序。
 * 当前激活标签的附加逻辑（仅主窗有过滤态）留在视图侧组合，这里保持纯函数。
 */
export function parseTagTokens(raw: string): string[] {
  const tokens = raw.split(/[\s,，、;；]+/).filter(Boolean)
  const set = new Set<string>()
  for (const t of tokens) set.add(t.startsWith('#') ? t : `#${t}`)
  return [...set]
}

/**
 * 多行粘贴并条（速记卡与主窗速记行共用）：粘贴文本含换行
 * 时逐行 trim、剔空行、以空格并成一行，并如实报行数供轻提示；单行/无有效行
 * 返回 null——消费方据此零干预放行原生插入路径。DOM 侧的 preventDefault 与
 * 提示落点（卡内 tip / 主窗 toast）归各视图，这里保持纯函数。
 */
export function flattenPastedLines(
  raw: string,
): { flat: string; lineCount: number } | null {
  if (!/[\r\n]/.test(raw)) return null
  const lines = raw.split(/[\r\n]+/).map((s) => s.trim()).filter(Boolean)
  if (lines.length === 0) return null
  return { flat: lines.join(' '), lineCount: lines.length }
}
