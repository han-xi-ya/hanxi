// memoMetrics 独立契约测试：零桩零依赖，纯函数逐位钉现 MemoView 内联实现的
// 行为口径（收编即锁版）——接线时删除视图内联副本后，本 spec 是这批算法的
// 唯一守卫。日期类断言一律注入固定 now，不吃真实时钟。
import { describe, expect, it } from 'vitest'
import {
  cardPreview,
  dayBucket,
  firstMeaningfulLine,
  flattenPastedLines,
  fmtAgo,
  groupMemoItems,
  memoColorHex,
  parseTagTokens,
} from '../memoMetrics'

/** 本地正午构造距今 N 天的时间串（避开的时区/边界噪声：两端都取本地零点差） */
function daysAgo(from: Date, n: number, hour = 12): string {
  const d = new Date(from.getFullYear(), from.getMonth(), from.getDate() - n, hour)
  return d.toISOString()
}

describe('dayBucket 时间切桶', () => {
  const now = new Date(2026, 8, 26, 15, 30) // 2026-09-26 15:30 本地

  it('今天/昨天/7 天内/30 天内/更早 五档与逐日边界', () => {
    expect(dayBucket(daysAgo(now, 0), now)).toBe('今天')
    expect(dayBucket(daysAgo(now, 1), now)).toBe('昨天')
    expect(dayBucket(daysAgo(now, 2), now)).toBe('7 天内')
    expect(dayBucket(daysAgo(now, 7), now)).toBe('7 天内')
    expect(dayBucket(daysAgo(now, 8), now)).toBe('30 天内')
    expect(dayBucket(daysAgo(now, 30), now)).toBe('30 天内')
    expect(dayBucket(daysAgo(now, 31), now)).toBe('更早')
  })

  it('未来时刻与同日凌晨都归今天（days<=0 口径，与现视图逐位一致）', () => {
    expect(dayBucket(daysAgo(now, -3), now)).toBe('今天')
    expect(dayBucket(daysAgo(now, 0, 0), now)).toBe('今天')
  })

  it('跨时区串按本地日历日判定：UTC 清晨仍是本地昨天不算今天', () => {
    // 本地东八区语义下，本地昨天 23 点的 ISO 串在 UTC 侧是"今天"，切桶必须跟本地走
    const lastNight = new Date(2026, 8, 25, 23, 0).toISOString()
    expect(dayBucket(lastNight, now)).toBe('昨天')
  })

  it('非法/空时间串落「更早」，不产 NaN 文案', () => {
    expect(dayBucket('not-a-date', now)).toBe('更早')
    expect(dayBucket('', now)).toBe('更早')
  })
})

describe('groupMemoItems 分组桶序', () => {
  const now = new Date(2026, 8, 26, 15, 30)
  const item = (id: string, isPinned = false, days = 0) => ({
    id,
    isPinned,
    updatedAt: daysAgo(now, days),
  })

  it('按 置顶→今天→昨天→7 天内→30 天内→更早 出桶，空桶缺席', () => {
    const groups = groupMemoItems(
      [item('a'), item('c', false, 3), item('b', true, 200), item('d', false, 400)],
      now,
    )
    expect(groups.map((g) => g.key)).toEqual(['置顶', '今天', '7 天内', '更早'])
    expect(groups[0].items.map((m) => m.id)).toEqual(['b']) // 置顶不论日期先进顶桶
  })

  it('桶内保持输入顺序（后端 updated 降序直传即保序）', () => {
    const groups = groupMemoItems([item('x', false, 0), item('y', false, 0)], now)
    expect(groups).toHaveLength(1)
    expect(groups[0].items.map((m) => m.id)).toEqual(['x', 'y'])
  })

  it('key 与 label 同值；空列表出空数组', () => {
    const groups = groupMemoItems([item('z')], now)
    expect(groups[0].label).toBe(groups[0].key)
    expect(groupMemoItems([], now)).toEqual([])
  })

  it('非法 updatedAt 条目落「更早」桶而非炸桶序', () => {
    const groups = groupMemoItems([{ id: 'bad', isPinned: false, updatedAt: '??' }], now)
    expect(groups.map((g) => g.key)).toEqual(['更早'])
  })
})

describe('fmtAgo 相对时间', () => {
  const now = new Date(2026, 8, 26, 15, 0)
  const minAgo = (m: number) => new Date(now.getTime() - m * 60000).toISOString()

  it('刚刚 / N 分钟前 / N 小时前 三档进位', () => {
    expect(fmtAgo(minAgo(0), now)).toBe('刚刚')
    expect(fmtAgo(minAgo(30), now)).toBe('30 分钟前')
    expect(fmtAgo(minAgo(59), now)).toBe('59 分钟前')
    expect(fmtAgo(minAgo(61), now)).toBe('1 小时前')
    expect(fmtAgo(minAgo(23 * 60 + 59), now)).toBe('23 小时前')
  })

  it('满 24 小时回落 fmtDate 的 yyyy-MM-dd', () => {
    expect(fmtAgo(minAgo(24 * 60), now)).toBe('2026-09-25')
  })

  it('非法串给「—」', () => {
    expect(fmtAgo('nope', now)).toBe('—')
  })
})

describe('cardPreview / firstMeaningfulLine 摘要提取', () => {
  it('剔除空白行取前 5 行，被截断补 \n…', () => {
    const src = 'a\n\nb\nc\n\n\nd\ne\nf\ng'
    expect(cardPreview(src)).toBe('a\nb\nc\nd\ne\n…')
  })

  it('不足 5 行原样收口、空正文得空串', () => {
    expect(cardPreview('a\nb')).toBe('a\nb')
    expect(cardPreview('')).toBe('')
    expect(cardPreview('  \n \n')).toBe('')
  })

  it('行数上限可覆盖（列表档等更矮口径共用）', () => {
    expect(cardPreview('a\nb\nc\nd\ne\nf', 2)).toBe('a\nb\n…')
  })

  it('首非空行：跳过前导空白行', () => {
    expect(firstMeaningfulLine('\n  \n第一行\n第二行')).toBe('第一行')
    expect(firstMeaningfulLine('')).toBe('')
  })
})

describe('parseTagTokens 速记标签解析', () => {
  it('中英标点分词、自动补 #、去重保序', () => {
    expect(parseTagTokens('SQL #SQL,token，密钥；jwt  jwt')).toEqual([
      '#SQL',
      '#token',
      '#密钥',
      '#jwt',
    ])
  })

  it('空输入得空数组', () => {
    expect(parseTagTokens('   ')).toEqual([])
  })
})

describe('flattenPastedLines 多行粘贴并条（速记卡 M3 语义，收口轮两面共用）', () => {
  it('单行与全空行返回 null——消费方零干预放行原生插入', () => {
    expect(flattenPastedLines('本就一行')).toBeNull()
    expect(flattenPastedLines('')).toBeNull()
    expect(flattenPastedLines('\n  \n')).toBeNull()
  })

  it('CRLF/LF 混排逐行 trim、剔空行、空格并条，行数如实', () => {
    expect(flattenPastedLines('行一\r\n行二\n \n行三')).toEqual({
      flat: '行一 行二 行三',
      lineCount: 3,
    })
  })
})

describe('memoColorHex 用户色板', () => {
  it('五色与现视图 COLOR_OPTIONS 逐字同源', () => {
    expect(memoColorHex('blue')).toBe('#3b82f6')
    expect(memoColorHex('emerald')).toBe('#10b981')
    expect(memoColorHex('amber')).toBe('#f59e0b')
    expect(memoColorHex('rose')).toBe('#f43f5e')
    expect(memoColorHex('purple')).toBe('#8b5cf6')
  })

  it('空值与历史未知值（如 green）回落 blue', () => {
    expect(memoColorHex()).toBe('#3b82f6')
    expect(memoColorHex('')).toBe('#3b82f6')
    expect(memoColorHex('green')).toBe('#3b82f6')
  })
})
