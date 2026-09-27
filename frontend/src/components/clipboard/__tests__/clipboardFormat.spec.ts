// clipboardFormat 纯函数单源的行为锁（时间口径是 epoch 毫秒——契约 §3 注明，
// 与随手记 ISO 串不同谱，边界必须在这里逐档钉死）：相对时间四档与回落日期、
// 非法值 '—'、时钟回拨（未来戳）消化、摘要兜底的类别三分支、data URL 拼接、
// 状态行文案。now 全部显式注入，不吃挂钟。
import { describe, expect, it } from 'vitest'
import {
  CLIP_FILTER_LABELS,
  blobDataUrl,
  clipStatusLine,
  fmtClipAgo,
  fmtClipDate,
  fmtClipDateTime,
  kindLabel,
  previewFallback,
  rowGlyph,
} from '../clipboardFormat'

const NOW = new Date(2026, 8, 26, 12, 0, 0) // 本地正午，避开时区边界
const MIN = 60_000
const HOUR = 60 * MIN
const ago = (ms: number) => NOW.getTime() - ms

describe('fmtClipAgo 相对时间档', () => {
  it('刚刚 / N 分钟前 / N 小时前三档与取整下界', () => {
    expect(fmtClipAgo(ago(30_000), NOW)).toBe('刚刚')
    expect(fmtClipAgo(ago(59_999), NOW)).toBe('刚刚')
    expect(fmtClipAgo(ago(MIN), NOW)).toBe('1 分钟前')
    expect(fmtClipAgo(ago(59 * MIN), NOW)).toBe('59 分钟前')
    expect(fmtClipAgo(ago(HOUR), NOW)).toBe('1 小时前')
    expect(fmtClipAgo(ago(23 * HOUR + 59 * MIN), NOW)).toBe('23 小时前')
  })

  it('超过 24 小时回落 yyyy-MM-dd（本地日历日）', () => {
    expect(fmtClipAgo(ago(24 * HOUR), NOW)).toBe('2026-09-25')
    expect(fmtClipAgo(ago(48 * HOUR + 3 * MIN), NOW)).toBe('2026-09-24')
  })

  it('未来时间戳（时钟偏移）按「刚刚」消化，不给「-3 分钟前」', () => {
    expect(fmtClipAgo(NOW.getTime() + 10 * MIN, NOW)).toBe('刚刚')
  })

  it('非法值一律 ' + "'—'" + '：null/undefined/NaN/Infinity/非数字类型', () => {
    expect(fmtClipAgo(null, NOW)).toBe('—')
    expect(fmtClipAgo(undefined, NOW)).toBe('—')
    expect(fmtClipAgo(Number.NaN, NOW)).toBe('—')
    expect(fmtClipAgo(Number.POSITIVE_INFINITY, NOW)).toBe('—')
    expect(fmtClipAgo('123' as unknown as number, NOW)).toBe('—')
  })
})

describe('fmtClipDate / fmtClipDateTime', () => {
  it('epoch 毫秒 → 本地 yyyy-MM-dd（补零）；非法值 —', () => {
    expect(fmtClipDate(new Date(2026, 0, 5, 8, 0, 0).getTime())).toBe('2026-01-05')
    expect(fmtClipDate(NaN)).toBe('—')
    expect(fmtClipDate(undefined)).toBe('—')
  })

  it('全量时间是本地可读串；非法值 —', () => {
    const s = fmtClipDateTime(new Date(2026, 8, 26, 9, 30, 0).getTime())
    expect(s).toContain('2026')
    expect(s).toContain('9')
    expect(fmtClipDateTime(null)).toBe('—')
  })
})

describe('类别与档位文案', () => {
  it('kindLabel 三档 + 未知值兜底「条目」（脏数据不抛错）', () => {
    expect(kindLabel('text')).toBe('文本')
    expect(kindLabel('image')).toBe('图片')
    expect(kindLabel('file')).toBe('文件')
    expect(kindLabel('bogus' as never)).toBe('条目')
  })

  it('「片段」是过滤合成档的文案单源', () => {
    expect(CLIP_FILTER_LABELS.manual).toBe('片段')
    expect(CLIP_FILTER_LABELS.all).toBe('全部')
  })

  it('rowGlyph：manual 片段优先于 text 身份；未知 kind 落 text 图标', () => {
    expect(rowGlyph({ kind: 'text', manual: true })).toBe('sticky')
    expect(rowGlyph({ kind: 'text' })).toBe('text')
    expect(rowGlyph({ kind: 'image' })).toBe('image')
    expect(rowGlyph({ kind: 'file' })).toBe('file')
    expect(rowGlyph({ kind: 'weird' as never })).toBe('text')
  })
})

describe('previewFallback 摘要兜底（只报结构事实不编造描述）', () => {
  it('有 preview 原样（trim 后）优先', () => {
    expect(previewFallback({ kind: 'text', preview: '  git status  ' })).toBe('git status')
  })
  it('图片：有尺寸报尺寸，无尺寸报类别', () => {
    expect(previewFallback({ kind: 'image', preview: '', width: 800, height: 600 })).toBe('图片 800×600')
    expect(previewFallback({ kind: 'image', preview: '   ' })).toBe('图片')
  })
  it('文件：有清单报个数，空清单报类别占位', () => {
    expect(previewFallback({ kind: 'file', preview: '', files: ['a', 'b', 'c'] })).toBe('3 个文件')
    expect(previewFallback({ kind: 'file', preview: '', files: [] })).toBe('文件列表')
  })
  it('文本空摘要给（空文本）占位', () => {
    expect(previewFallback({ kind: 'text', preview: '' })).toBe('（空文本）')
  })
})

describe('blobDataUrl 与状态行', () => {
  it('base64 → PNG data URL；空值给空串（呈现层占位）', () => {
    expect(blobDataUrl('iVBORw0KG')).toBe('data:image/png;base64,iVBORw0KG')
    expect(blobDataUrl(undefined)).toBe('')
    expect(blobDataUrl('')).toBe('')
  })

  it('clipStatusLine 条目与图片用量一行（fmtSize 共享档）', () => {
    expect(
      clipStatusLine({ entryCount: 12, maxEntries: 500, blobBytes: 2048, maxBlobBytes: 104857600 }),
    ).toBe('条目 12/500 · 图片占 2 KB/100.0 MB')
  })

  it('零 blob 用量走 fmtSize 的「—」档（共享单源怪癖原样继承）', () => {
    expect(
      clipStatusLine({ entryCount: 0, maxEntries: 500, blobBytes: 0, maxBlobBytes: 104857600 }),
    ).toBe('条目 0/500 · 图片占 —/100.0 MB')
  })
})
