// clipboardFormat 纯函数单源的行为锁（时间口径是 epoch 毫秒——契约 §3 注明，
// 与随手记 ISO 串不同谱，边界必须在这里逐档钉死）：相对时间四档与回落日期、
// 非法值 '—'、时钟回拨（未来戳）消化、摘要兜底的类别三分支、data URL 拼接、
// 状态行文案。now 全部显式注入，不吃挂钟。
import { describe, expect, it } from 'vitest'
import type { ClipEntry } from '../../../types/clipboard'
import {
  CLIP_FILTER_LABELS,
  blobDataUrl,
  clipStatusLine,
  clipThumbSrc,
  filePathDir,
  filePathName,
  fmtClipAgo,
  fmtClipDate,
  fmtClipDateTime,
  groupClipEntries,
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

describe('clipThumbSrc 缩略图安全闸（R-G2 内联 dataURL 唯一渲染入口）', () => {
  it('data:image/ 前缀原样放行', () => {
    expect(clipThumbSrc('data:image/jpeg;base64,/9j/AAA')).toBe('data:image/jpeg;base64,/9j/AAA')
    expect(clipThumbSrc('data:image/png;base64,iVBOR')).toBe('data:image/png;base64,iVBOR')
  })
  it('非图 dataURL、远端 URL、javascript: 脏值与空值一律给空串（呈现层回落图标）', () => {
    expect(clipThumbSrc('javascript:alert(1)')).toBe('')
    expect(clipThumbSrc('https://example.com/a.png')).toBe('')
    expect(clipThumbSrc('data:application/html,x')).toBe('')
    expect(clipThumbSrc(undefined)).toBe('')
    expect(clipThumbSrc(null)).toBe('')
    expect(clipThumbSrc('')).toBe('')
  })
})

describe('文件路径拆分（详情清单结构化）', () => {
  it('基名：反斜杠/正斜杠都认，取尾段；裸名原样', () => {
    expect(filePathName('C:\\tmp\\甲.txt')).toBe('甲.txt')
    expect(filePathName('D:/b.pdf')).toBe('b.pdf')
    expect(filePathName('readme.md')).toBe('readme.md')
    expect(filePathName('')).toBe('')
  })
  it('父目录：去尾分隔符；盘根父补全尾斜杠；裸名给空串（呈现层省略该行）', () => {
    expect(filePathDir('C:\\tmp\\甲.txt')).toBe('C:\\tmp')
    expect(filePathDir('D:/b.pdf')).toBe('D:\\')
    expect(filePathDir('readme.md')).toBe('')
    expect(filePathDir('C:\\a.txt')).toBe('C:\\')
  })
})

describe('groupClipEntries 日档分组', () => {
  const mk = (over: Partial<ClipEntry> = {}): ClipEntry =>
    ({ id: 'x', hash: 'h', kind: 'text', preview: 'p', byteSize: 1, createdAt: NOW.getTime(), ...over }) as ClipEntry

  it('今天/昨天/本周内/更早四档归位，组内保持入参序，零条目档不产段', () => {
    const groups = groupClipEntries(
      [
        mk({ id: 'a', createdAt: NOW.getTime() - 5 * MIN }), // 正午前 5 分钟 → 今天
        mk({ id: 'b', createdAt: NOW.getTime() - 26 * 60 * MIN }), // 昨天凌晨
        mk({ id: 'c', createdAt: NOW.getTime() - 3 * 24 * 60 * MIN }), // 3 天前
        mk({ id: 'd', createdAt: NOW.getTime() - 30 * 24 * 60 * MIN }), // 30 天前
        mk({ id: 'e', createdAt: NOW.getTime() - 6 * 24 * 60 * MIN - 1 * MIN }), // 6 天余（仍 <7 天档）
      ],
      NOW,
    )
    expect(groups.map((g) => [g.key, g.label, g.items.map((i) => i.id)])).toEqual([
      ['today', '今天', ['a']],
      ['yesterday', '昨天', ['b']],
      ['week', '本周内', ['c', 'e']],
      ['older', '更早', ['d']],
    ])
  })

  it('未来戳并入「今天」（与 fmtClipAgo 时钟偏移消化同谱）；脏时间戳归「更早」不蒸发', () => {
    const groups = groupClipEntries(
      [mk({ id: 'f', createdAt: NOW.getTime() + 3 * 24 * 60 * MIN }), mk({ id: 'n', createdAt: NaN })],
      NOW,
    )
    expect(groups.map((g) => g.key)).toEqual(['today', 'older'])
    expect(groups[1].items.map((i) => i.id)).toEqual(['n'])
  })

  it('空账本零段', () => {
    expect(groupClipEntries([], NOW)).toEqual([])
  })
})
