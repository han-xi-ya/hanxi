// 剪贴板 · 纯格式化单源模块（A4 主界面私有，模式照 components/memo/memoMetrics 先例）。
//
// 收编动机：本条目的时间口径是 epoch 毫秒（types/clipboard.ts 注明，与随手记的
// ISO 串不同谱），utils/format 的 fmtDate/fmtAgo 是 ISO 档不吃毫秒——相对时间、
// 全量时间、日期兜底全部在本模块落地，视图与行/详情组件只消费此处函数，
// 模板零内联格式化表达式。字节档复用共享 fmtSize 单源（通用口径，不造副本）。
//
// 零 Vue、零 DOM、零后端绑定（types/ 是纯类型层，允许 import），可独立测试。

import { fmtSize } from '../../utils/format'
import type { ClipEntry, ClipKind, ClipStatus } from '../../types/clipboard'

/** 字节大小格式化：直接复用共享 fmtSize 单源（KB/MB/GB/TB 档零副本）。 */
export const fmtClipBytes = fmtSize

/**
 * 工具条 kind 过滤档位：契约四档（all/text/image/file）+ 前端合成的「片段」档。
 * 后端 List 的 kind 参数只有 text/image/file（manual 不是类别，是标志位），
 * 「片段」档拉无过滤结果后由视图侧按 manual===true 过滤行（契约 §4 不越面）。
 */
export type ClipFilter = 'all' | ClipKind | 'manual'

export const CLIP_FILTER_LABELS: Record<ClipFilter, string> = {
  all: '全部',
  text: '文本',
  image: '图片',
  file: '文件',
  manual: '片段',
}

const KIND_LABELS: Record<ClipKind, string> = { text: '文本', image: '图片', file: '文件' }

/** 类别中文名；未知值兜底「条目」（后端契约外脏数据不给渲染层抛错）。 */
export function kindLabel(kind: ClipKind): string {
  return KIND_LABELS[kind] ?? '条目'
}

/** 行/详情主图标名：manual 片段优先于其 text 类身份（片段要有自己的视觉身份）。 */
export function rowGlyph(entry: Pick<ClipEntry, 'kind' | 'manual'>): string {
  if (entry.manual) return 'sticky'
  return entry.kind in KIND_LABELS ? entry.kind : 'text'
}

/** epoch 毫秒 → yyyy-MM-dd（本地日历日）；非法值 '—'（不给渲染层抛 NaN）。 */
export function fmtClipDate(ms?: number | null): string {
  if (typeof ms !== 'number' || !Number.isFinite(ms)) return '—'
  const d = new Date(ms)
  if (Number.isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** epoch 毫秒 → 本地完整日期时间（行内时间的悬浮全量档）；非法值 '—'。 */
export function fmtClipDateTime(ms?: number | null): string {
  if (typeof ms !== 'number' || !Number.isFinite(ms)) return '—'
  const d = new Date(ms)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString()
}

/**
 * 相对时间：刚刚 / N 分钟前 / N 小时前，超过 24 小时回落 yyyy-MM-dd；
 * 非法值 '—'。未来时间戳（时钟偏移）按「刚刚」消化。now 可注入测试。
 */
export function fmtClipAgo(ms?: number | null, now: Date = new Date()): string {
  if (typeof ms !== 'number' || !Number.isFinite(ms)) return '—'
  if (Number.isNaN(new Date(ms).getTime())) return '—'
  const min = Math.floor((now.getTime() - ms) / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  if (min < 24 * 60) return `${Math.floor(min / 60)} 小时前`
  return fmtClipDate(ms)
}

/**
 * 行摘要兜底：后端 preview 为空白时按类别给机器线索（只报结构事实不编造描述）——
 * 图片报尺寸（有则报）、文件报个数、文本给空占位。
 */
export function previewFallback(
  entry: Pick<ClipEntry, 'kind' | 'preview' | 'files' | 'width' | 'height'>,
): string {
  const p = (entry.preview ?? '').trim()
  if (p) return p
  switch (entry.kind) {
    case 'image':
      return entry.width && entry.height ? `图片 ${entry.width}×${entry.height}` : '图片'
    case 'file':
      return entry.files?.length ? `${entry.files.length} 个文件` : '文件列表'
    default:
      return '（空文本）'
  }
}

/** Get 回填的 base64 PNG → data URL；空值给 ''（呈现层负责占位分支）。 */
export function blobDataUrl(base64?: string | null): string {
  return base64 ? `data:image/png;base64,${base64}` : ''
}

/**
 * 行缩略图安全闸（契约 v1.7 R-G2：image 条目 List 起内联 64px JPEG dataURL）：
 * 唯一渲染入口——非 `data:image/` 前缀一律视为无图给 ''（呈现层回落图标），
 * 脏值不得直通 <img src>。空串/缺省同为 ''。
 */
export function clipThumbSrc(thumb?: string | null): string {
  return typeof thumb === 'string' && thumb.startsWith('data:image/') ? thumb : ''
}

/** 路径基名：CF_HDROP 路径取尾段（\ 与 / 分隔符都认；目录形尾斜杠给空串）。 */
export function filePathName(path: string): string {
  const norm = (path ?? '').replace(/\//g, '\\')
  const i = norm.lastIndexOf('\\')
  return i >= 0 ? norm.slice(i + 1) : norm
}

/** 路径父目录（去尾分隔符）；裸文件名给 ''（呈现层省略该行）；盘根父补全
 *  尾斜杠（"C:\a.txt" 的父目录显式 "C:\"，裸 "C:" 不成形）。 */
export function filePathDir(path: string): string {
  const norm = (path ?? '').replace(/\//g, '\\')
  const i = norm.lastIndexOf('\\')
  if (i <= 0) return ''
  const dir = norm.slice(0, i)
  return dir.endsWith(':') ? `${dir}\\` : dir
}

/** 索引日期档分组（吸顶小字档，语言照随手记 v2 段头；剪贴板账本只按日分档）。 */
export interface ClipDayGroup {
  key: 'today' | 'yesterday' | 'week' | 'older'
  label: string
  items: ClipEntry[]
}

const CLIP_GROUP_LABELS: Record<ClipDayGroup['key'], string> = {
  today: '今天',
  yesterday: '昨天',
  week: '本周内',
  older: '更早',
}

const GROUP_ORDER: readonly ClipDayGroup['key'][] = ['today', 'yesterday', 'week', 'older']

function localDayStart(ms: number): number {
  const d = new Date(ms)
  if (Number.isNaN(d.getTime())) return NaN
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/**
 * 按 createdAt 的自然日档切组：今天 / 昨天 / 本周内（<7 天）/ 更早。
 * 入参顺序即账本顺序（后端新→旧），组内保序；未来戳并入「今天」（与
 * fmtClipAgo「刚刚」消化时钟偏移同谱）；脏时间戳（非法/缺失）归「更早」
 * 不蒸发；零条目档不产段（不给空段头）。now 可注入测试。
 */
export function groupClipEntries(
  entries: readonly ClipEntry[],
  now: Date = new Date(),
): ClipDayGroup[] {
  const buckets: Record<ClipDayGroup['key'], ClipEntry[]> = {
    today: [],
    yesterday: [],
    week: [],
    older: [],
  }
  const base = localDayStart(now.getTime())
  for (const e of entries) {
    const created = localDayStart(e.createdAt)
    const days =
      Number.isNaN(created) || Number.isNaN(base)
        ? Infinity
        : Math.round((base - created) / 86_400_000)
    if (days <= 0) buckets.today.push(e)
    else if (days === 1) buckets.yesterday.push(e)
    else if (days < 7) buckets.week.push(e)
    else buckets.older.push(e)
  }
  return GROUP_ORDER.filter((k) => buckets[k].length > 0).map((k) => ({
    key: k,
    label: CLIP_GROUP_LABELS[k],
    items: buckets[k],
  }))
}

/** 状态条一行文案：条目数与图片 blob 用量（决策相关数字，消费方挂 mono）。 */
export function clipStatusLine(
  s: Pick<ClipStatus, 'entryCount' | 'maxEntries' | 'blobBytes' | 'maxBlobBytes'>,
): string {
  return `条目 ${s.entryCount}/${s.maxEntries} · 图片占 ${fmtSize(s.blobBytes)}/${fmtSize(s.maxBlobBytes)}`
}
