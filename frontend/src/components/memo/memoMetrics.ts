// 随手记 · 度量与纯函数单源模块（组件库四路之四，模式照抄留言板 boardMetrics 先例）。
//
// 收编动机：时间分组切桶（dayBucket/置顶档）、相对时间（fmtAgo）、摘要行提取
// （cardPreview）、速记标签解析（parseTagTokens）、色彩标识换色（memoColorHex）
// 此前内联在 MemoView 的 script 里。本模块只收编满足「真被两处以上需要或有独立
// 测试价值」的函数：
//   - groupMemoItems/dayBucket/fmtAgo/cardPreview：MemoCard 呈现件与 M2 主视图
//     分组标题两处消费，且日期边界（7/30 天切点、跨午夜、非法 ISO 串）有独立
//     测试价值；
//   - parseTagTokens：现 MemoView.parseQuickTags 与 QuickMemoSheet.parseTags
//     已存在逐字重复的两份实现（本轮禁碰两视图，先落单源供接线时删重）；
//   - memoColorHex：MemoCard 左封边取色需要，色板与视图 COLOR_OPTIONS 逐字
//     同源——色彩标识是用户数据值（colorTag 持久化进后端），非主题表面，
//     五个十六进制色沿用视图治理注记的豁免口径，不算新色值。
// 刻意不收编的：「高亮分段」在现视图与 markdown.ts 中都不存在（搜索命中
// 目前后端过滤、前端不高亮），为不存在的第二消费方抽函数即"为抽而抽"，不预造。
//
// 零 Vue、零 DOM、零后端绑定，可独立测试。

import { fmtDate } from '../../utils/format'

/** 分组顺序与桶名（与 MemoView 现实现逐字一致的六档口径） */
export const MEMO_GROUP_ORDER = ['置顶', '今天', '昨天', '7 天内', '30 天内', '更早'] as const

/** 色彩标识 → 十六进制（用户数据色板：blue/emerald/amber/rose/purple） */
export const MEMO_COLOR_HEX: Record<string, string> = {
  blue: '#3b82f6',
  emerald: '#10b981',
  amber: '#f59e0b',
  rose: '#f43f5e',
  purple: '#8b5cf6',
}
/** 未知/缺省色彩标识的兜底档（视图现行为：colorTag || 'blue'） */
export const MEMO_COLOR_FALLBACK = 'blue'

/** 便签色彩标识换十六进制色；未知值（含历史 green 等）与空串一律回落 blue。 */
export function memoColorHex(color?: string | null): string {
  return MEMO_COLOR_HEX[color ?? ''] ?? MEMO_COLOR_HEX[MEMO_COLOR_FALLBACK]
}

/**
 * 自然日档切桶（与 MemoView.dayBucket 逐位同式）：按本地日历日零点差取整——
 * 今天（含未来/同日，days<=0）、昨天、7 天内、30 天内、更早；非法时间串落
 * 「更早」（不给渲染层抛 NaN）。now 可注入（测试钉边界；分组一次遍历共用同一
 * now，杜绝旧实现每条各取 new Date() 可能跨午夜分裂的隐患）。
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
 * 置顶→今天→昨天→7 天内→30 天内→更早 的分桶遍历（与 MemoView.groupedMemos
 * 同行为：置顶条目不论日期先进「置顶」桶；空桶不出现在结果里）。
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
 * 相对时间（与 MemoView.fmtAgo 逐位同式）：刚刚 / N 分钟前 / N 小时前，
 * 超过 24 小时回落 fmtDate 的 yyyy-MM-dd；非法串给「—」。now 可注入测试。
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

/** 卡片摘要行数上限（与 MemoView.cardPreview 现行为一致的默认档） */
export const CARD_PREVIEW_LINES = 5

/**
 * 摘要行提取（与 MemoView.cardPreview 逐位同式）：剔除空白行后取前 maxLines
 * 行、换行拼接；被截断时尾部补「\n…」。全空白正文得空串（呈现层负责占位文案）。
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
 * 速记标签解析（收编 MemoView.parseQuickTags 与 QuickMemoSheet.parseTags 的
 * 两份逐字重复实现）：空白/逗号/顿号/分号（中英）分词、自动补 #、去重保序。
 * 当前激活标签的附加逻辑（仅主窗有过滤态）留在视图侧组合，这里保持纯函数。
 */
export function parseTagTokens(raw: string): string[] {
  const tokens = raw.split(/[\s,，、;；]+/).filter(Boolean)
  const set = new Set<string>()
  for (const t of tokens) set.add(t.startsWith('#') ? t : `#${t}`)
  return [...set]
}
