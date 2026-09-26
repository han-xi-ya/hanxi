// boardMetrics 纯函数契约测试（v4 牌面收编的数值锚点）。
// 关键使命：与视图两侧现实现逐位对账——stageScale 复现 v3 grow 档历史字面量
// （scale(0.34)/scale(0.1846…)），stageScaleMeasured 复现 v4 牌桌量测档现字面量
// （64px/300 盒＝0.34615384615384615）；A 路把内联式换成调用时零断言迁移。
import { describe, expect, it } from 'vitest'
import {
  boardSubOf,
  boardTitleOf,
  cardMaxHeightPx,
  cardMaxWidthPx,
  cardNaturalWidth,
  MIN_SCALE,
  scalePercent,
  scaleZoomFactor,
  stageScale,
  stageScaleMeasured,
  subFontSizePx,
} from '../boardMetrics'

describe('stageScale：grow/fit/cap 三值取小（与视图 v3 逐位同式）', () => {
  it('300px 盒 + 字号 64 维持 0.34 基准（v2 观感原点）', () => {
    expect(stageScale({ fontSize: 64, boxWidth: 300, cap: 0.8 })).toBe(0.34)
  })

  it('字号翻倍按卡宽上限收缩：fit=288/(120×13)≈0.18461538461538463 逐位一致', () => {
    expect(stageScale({ fontSize: 120, boxWidth: 300, cap: 0.8 })).toBe(0.18461538461538463)
  })

  it('盒宽线性放大 grow：600px 盒字号 24 时 grow=0.68（fit 未越界）', () => {
    expect(stageScale({ fontSize: 24, boxWidth: 600, cap: 1 })).toBeCloseTo(0.68, 10)
  })

  it('页内小预览封顶 0.8：grow 越线后被 cap 截住', () => {
    expect(stageScale({ fontSize: 64, boxWidth: 2000, cap: 0.8 })).toBe(0.8)
  })

  it('舞台默认封顶 1.0：不传 cap 也绝不放大过 1:1', () => {
    expect(stageScale({ fontSize: 24, boxWidth: 1000 })).toBe(1)
  })

  it('盒宽未测到（0/负数/NaN）回落基准常数 300——happy-dom 与首帧口径', () => {
    const base = stageScale({ fontSize: 64, boxWidth: 300, cap: 0.8 })
    expect(stageScale({ fontSize: 64, boxWidth: 0, cap: 0.8 })).toBe(base)
    expect(stageScale({ fontSize: 64, boxWidth: -120, cap: 0.8 })).toBe(base)
    expect(stageScale({ fontSize: 64, boxWidth: NaN, cap: 0.8 })).toBe(base)
  })
})

describe('stageScaleMeasured：v4 牌桌"量实测不猜"档（与 A 路 v4 内联式逐位同式）', () => {
  it('牌宽测不到回落 字号×13：64px/300 盒＝288/832＝0.34615384615384615', () => {
    expect(stageScaleMeasured({ boxWidth: 300, cardWidth: 0, fontSize: 64 })).toBe(0.34615384615384615)
    expect(stageScaleMeasured({ boxWidth: 300, cardWidth: 0, fontSize: 120 })).toBe(0.18461538461538463)
  })

  it('实测牌宽优先：短牌 300 自然宽进 1000 盒取 min(1, 988/300)＝1（1:1 合法终态）', () => {
    expect(stageScaleMeasured({ boxWidth: 1000, cardWidth: 300, fontSize: 24 })).toBe(1)
  })

  it('cap 默认 1.0、可显式收紧；盒宽未测到回落基准 300', () => {
    expect(stageScaleMeasured({ boxWidth: 2000, cardWidth: 100, fontSize: 64 })).toBe(1)
    expect(stageScaleMeasured({ boxWidth: 2000, cardWidth: 100, fontSize: 64, cap: 0.8 })).toBe(0.8)
    expect(stageScaleMeasured({ boxWidth: 0, cardWidth: 0, fontSize: 64 })).toBe(0.34615384615384615)
  })

  it('degenerate 入参（盒宽<边距）落缩放地板而非负值镜像', () => {
    expect(stageScaleMeasured({ boxWidth: 5, cardWidth: 832, fontSize: 64 })).toBe(MIN_SCALE)
    expect(stageScale({ fontSize: 64, boxWidth: 5, cap: 0.8 })).toBe(MIN_SCALE)
  })
})

describe('cardMaxHeightPx：--bc-max-h 超高裁剪反算', () => {
  it('盒高 560、缩 0.34：(560−12)/0.34 取整=1611', () => {
    expect(cardMaxHeightPx(560, 0.34)).toBe(1611)
  })

  it('矮盒托底 48px：负值/零值都钳到下限，牌不留缝', () => {
    expect(cardMaxHeightPx(10, 0.34)).toBe(48)
    expect(cardMaxHeightPx(12, 0.34)).toBe(48)
  })

  it('scale<=0/NaN 异常入参回落下限而非 Infinity', () => {
    expect(cardMaxHeightPx(560, 0)).toBe(48)
    expect(cardMaxHeightPx(560, -1)).toBe(48)
    expect(cardMaxHeightPx(560, NaN)).toBe(48)
  })
})

describe('定标系数与版式拆分（BoardCard 收编口径）', () => {
  it('副行字号 max(12, round(×0.42))：96→40、64→27、24→12 托底', () => {
    expect(subFontSizePx(96)).toBe(40)
    expect(subFontSizePx(64)).toBe(27)
    expect(subFontSizePx(24)).toBe(12)
  })

  it('卡宽上限取整 832（64×13），自然宽不取整供比值运算', () => {
    expect(cardMaxWidthPx(64)).toBe(832)
    expect(cardNaturalWidth(64)).toBe(832)
    expect(cardNaturalWidth(63.5)).toBe(825.5)
  })

  it('主题=第一行；副行=其余行，trailing 空行不产生空气副行', () => {
    expect(boardTitleOf('☕ 去茶水间了\n20 分钟内回来')).toBe('☕ 去茶水间了')
    expect(boardSubOf('☕ 去茶水间了\n20 分钟内回来')).toBe('20 分钟内回来')
    expect(boardSubOf('马上回来\n\n ')).toBe('')
    expect(boardSubOf('马上回来')).toBe('')
    expect(boardSubOf('T\nA\nB')).toBe('A\nB')
    expect(boardTitleOf('')).toBe('')
  })
})

describe('大白话换算（预览文案口径）', () => {
  it('百分比下限 1；倍数走 round(1/scale) 且下限 1', () => {
    expect(scalePercent(0.18461538461538463)).toBe(18)
    expect(scalePercent(0.004)).toBe(1)
    expect(scaleZoomFactor(0.18461538461538463)).toBe(5)
    expect(scaleZoomFactor(0.34)).toBe(3)
    expect(scaleZoomFactor(1)).toBe(1)
  })
})
