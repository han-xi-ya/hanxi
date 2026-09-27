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

/** 状态条一行文案：条目数与图片 blob 用量（决策相关数字，消费方挂 mono）。 */
export function clipStatusLine(
  s: Pick<ClipStatus, 'entryCount' | 'maxEntries' | 'blobBytes' | 'maxBlobBytes'>,
): string {
  return `条目 ${s.entryCount}/${s.maxEntries} · 图片占 ${fmtSize(s.blobBytes)}/${fmtSize(s.maxBlobBytes)}`
}
