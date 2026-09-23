// wheelGeometry 纯函数单元测试：锁定 512 窗口角度约定的 polar 定值、wedgePath
// 的大弧标志与两位小数格式、capSpanDeg 三种钳位分支、capSectorAngles 的对称
// 与留缝规则、capAnchorDeg/mainAnchor 的百分比字符串形态。
// 定值全部按几何定义手算（0°=12 点、顺时针、C=256），不抄实现输出。
import { describe, expect, it } from 'vitest'
import {
  WHEEL,
  polar,
  wedgePath,
  mainSectorAngles,
  capSpanDeg,
  capSectorAngles,
  capAnchorDeg,
  mainAnchor,
  slotOf,
} from '../wheelGeometry'

/** 解析 "x%" 字符串为数值，顺带校验形态 */
const pct = (s: string): number => {
  expect(s).toMatch(/^-?\d+(\.\d+)?%$/)
  return parseFloat(s)
}

describe('WHEEL 常量表', () => {
  it('512 窗口定稿值逐字锁定', () => {
    expect(WHEEL).toEqual({
      size: 512,
      c: 256,
      rDisc: 170,
      rHub: 62,
      rSecIn: 74,
      rSecOut: 162,
      rCapIn: 174,
      rCapOut: 236,
      rCancel: 244,
    })
  })

  it('半径序自洽：帽带落在盘缘之外、取消圆之内，主环扇区带不越盘缘', () => {
    expect(WHEEL.c).toBe(WHEEL.size / 2)
    expect(WHEEL.rHub).toBeLessThan(WHEEL.rSecIn)
    expect(WHEEL.rSecOut).toBeLessThan(WHEEL.rDisc)
    expect(WHEEL.rDisc).toBeLessThan(WHEEL.rCapIn)
    expect(WHEEL.rCapOut).toBeLessThan(WHEEL.rCancel)
    expect(WHEEL.rCancel).toBeLessThan(WHEEL.c) // 取消圆不出窗口
  })
})

describe('polar', () => {
  it('四正方位定值（0°=12 点、顺时针增长，r=100）', () => {
    expect(polar(100, 0).x).toBeCloseTo(256, 9)
    expect(polar(100, 0).y).toBeCloseTo(156, 9) // 正上方：256-100
    expect(polar(100, 90).x).toBeCloseTo(356, 9) // 正右方
    expect(polar(100, 90).y).toBeCloseTo(256, 9)
    expect(polar(100, 180).x).toBeCloseTo(256, 9)
    expect(polar(100, 180).y).toBeCloseTo(356, 9) // 正下方
    expect(polar(100, 270).x).toBeCloseTo(156, 9) // 正左方
    expect(polar(100, 270).y).toBeCloseTo(256, 9)
  })

  it('45° 落在右上 45 度线（与 QuickMenuPopup.polar 同式的 512 口径）', () => {
    const d = 100 / Math.SQRT2 // 手算：r·cos45 = r·sin45 ≈ 70.711
    expect(polar(100, 45).x).toBeCloseTo(256 + d, 9)
    expect(polar(100, 45).y).toBeCloseTo(256 - d, 9)
  })
})

describe('wedgePath', () => {
  it('90°→180° 主环环段全串定值（两位小数、sweep 外 1 内 0）', () => {
    expect(wedgePath(74, 162, 90, 180)).toBe(
      'M 330.00 256.00 L 418.00 256.00 A 162 162 0 0 1 256.00 418.00 ' +
        'L 256.00 330.00 A 74 74 0 0 0 330.00 256.00 Z',
    )
  })

  it('跨角 >180° 置大弧标志（外缘与内缘弧同置）', () => {
    const p = wedgePath(174, 236, 0, 200)
    expect(p).toContain('A 236 236 0 1 1 ')
    expect(p).toContain('A 174 174 0 1 0 ')
  })

  it('跨角 ≤180° 大弧标志为 0（恰 180° 亦为 0）', () => {
    expect(wedgePath(174, 236, 0, 100)).toContain('A 236 236 0 0 1 ')
    expect(wedgePath(174, 236, 0, 100)).toContain('A 174 174 0 0 0 ')
    expect(wedgePath(174, 236, 0, 180)).toContain('A 236 236 0 0 1 ')
  })
})

describe('mainSectorAngles', () => {
  it('n=4 默认留缝：第 0 枚 = [0.9°, 89.1°]', () => {
    expect(mainSectorAngles(0, 4).a0).toBeCloseTo(0.9, 9)
    expect(mainSectorAngles(0, 4).a1).toBeCloseTo(89.1, 9)
  })

  it('padDeg=0 时整圆无缝等分', () => {
    expect(mainSectorAngles(1, 6, 0)).toEqual({ a0: 60, a1: 120 })
  })

  it('相邻扇区缝隙恒为两侧 padDeg', () => {
    const gap = mainSectorAngles(2, 7).a0 - mainSectorAngles(1, 7).a1
    expect(gap).toBeCloseTo(1.8, 9)
  })
})

describe('capSpanDeg', () => {
  it('窄父楔形：子数把帽带撑开到 子数×24°（4 子 × 36° 父 → 96°）', () => {
    expect(capSpanDeg(4, 36)).toBeCloseTo(96, 9)
  })

  it('宽父楔形：保持父步长下限（2 子 × 120° 父 → 120°）', () => {
    expect(capSpanDeg(2, 120)).toBeCloseTo(120, 9)
  })

  it('超 180° 截断（20 子 → 180°；n=1 的 360° 父步长同样截到 180°）', () => {
    expect(capSpanDeg(20, 90)).toBeCloseTo(180, 9)
    expect(capSpanDeg(1, 360)).toBeCloseTo(180, 9)
  })

  it('childCount ≤ 0 返回 0', () => {
    expect(capSpanDeg(0, 90)).toBe(0)
    expect(capSpanDeg(-3, 90)).toBe(0)
  })
})

describe('capSectorAngles', () => {
  it('childCount ≤ 0 返回 null，j 越界亦返回 null', () => {
    expect(capSectorAngles(0, 0, 90, 0)).toBeNull()
    expect(capSectorAngles(-1, 0, 90, 0)).toBeNull()
    expect(capSectorAngles(3, 0, 90, -1)).toBeNull()
    expect(capSectorAngles(3, 0, 90, 3)).toBeNull()
  })

  it('单子不留缝：完整占满 span 且关于父中线对称', () => {
    const one = capSectorAngles(1, 0, 90, 0)!
    expect(one.a0).toBeCloseTo(-45, 9)
    expect(one.a1).toBeCloseTo(45, 9)
    expect(one.a1 - one.a0).toBeCloseTo(capSpanDeg(1, 90), 9) // span 即 capSpanDeg
    expect((one.a0 + one.a1) / 2).toBeCloseTo(0, 9)
  })

  it('奇数子（n=3）：首末子扇区互为镜像，中线覆盖父中线', () => {
    const first = capSectorAngles(3, 0, 90, 0)!
    const middle = capSectorAngles(3, 0, 90, 1)!
    const last = capSectorAngles(3, 0, 90, 2)!
    expect(first.a0).toBeCloseTo(-44.1, 9) // -45 + pad
    expect(first.a1).toBeCloseTo(-15.9, 9) // -15 - pad
    expect(first.a0).toBeCloseTo(-last.a1, 9)
    expect(first.a1).toBeCloseTo(-last.a0, 9)
    expect((middle.a0 + middle.a1) / 2).toBeCloseTo(0, 9)
    // 单子情形首末镜像同理（对称性对奇偶一致成立）：
    const single = capSectorAngles(1, 0, 90, 0)!
    expect(single.a0).toBeCloseTo(-single.a1, 9)
  })

  it('偶数子（n=4，父中线偏到 100°）：首末与次对首末均关于 100° 镜像', () => {
    const j0 = capSectorAngles(4, 100, 90, 0)!
    const j3 = capSectorAngles(4, 100, 90, 3)!
    const j1 = capSectorAngles(4, 100, 90, 1)!
    const j2 = capSectorAngles(4, 100, 90, 2)!
    // 4 子 ×24=96 > 父步长 90 → span=96，slot=24
    expect(j3.a1 - j0.a0).toBeCloseTo(96 - 1.8, 9)
    expect(j0.a0 + j3.a1).toBeCloseTo(200, 9)
    expect(j0.a1 + j3.a0).toBeCloseTo(200, 9)
    expect(j1.a0 + j2.a1).toBeCloseTo(200, 9)
  })

  it('子数 >1 缝隙恒为两侧 padDeg，单子为 0', () => {
    const a = capSectorAngles(5, 40, 72, 1)!
    const b = capSectorAngles(5, 40, 72, 2)!
    expect(b.a0 - a.a1).toBeCloseTo(1.8, 9)
    expect(capSpanDeg(1, 72)).toBeGreaterThan(0)
  })
})

describe('capAnchorDeg', () => {
  it('0° 角中线锚点落在画布竖直中线（left 恰为 "50%"），形态为百分比串', () => {
    const a = capAnchorDeg(-20, 20)
    expect(a.left).toBe('50%')
    // top 手算：y = 256 − 205（帽带中线半径）= 51 → 51/512 = 9.9609375%
    expect(a.top).toBe('9.9609375%')
  })

  it('90° 角中线锚点落在水平中线，半径即帽带中线 205', () => {
    const a = capAnchorDeg(80, 100)
    expect(a.top).toBe('50%')
    // left 手算：(256+205)/512 = 461/512 = 90.0390625%
    expect(a.left).toBe('90.0390625%')
    expect(pct(a.left)).toBeCloseTo(90.0390625, 6)
  })
})

describe('mainAnchor', () => {
  it('百分比字符串形态 + 扇区带中线半径 118 处（n=4 第 0 枚，45°）', () => {
    const a = mainAnchor(0, 4)
    // 手算：50 ± 118·cos45°/512·100 = 50 ± 16.2966%
    const off = (118 / Math.SQRT2 / 512) * 100
    expect(pct(a.left)).toBeCloseTo(50 + off, 6)
    expect(pct(a.top)).toBeCloseTo(50 - off, 6)
  })

  it('n=1 单枚主扇区锚点在正下方中线上（left 恰为 "50%"）', () => {
    const a = mainAnchor(0, 1) // 名义中线 180°
    expect(a.left).toBe('50%')
    expect(pct(a.top)).toBeCloseTo(((256 + 118) / 512) * 100, 6) // 374/512 = 73.046875%
  })
})

describe('slotOf（N40 全扇面角向归属）', () => {
  it('n=4 名义角域四分：0°→槽0、90°→槽1、270°→槽3', () => {
    expect(slotOf(0, 4)).toBe(0)
    expect(slotOf(89.9, 4)).toBe(0) // step=90，89.9 仍在槽 0
    expect(slotOf(90, 4)).toBe(1)
    expect(slotOf(270, 4)).toBe(3)
  })

  it('缝隙带像素归属其所属扇区（pad 边界不判死区）', () => {
    // 第 0 枚楔形绘制止于 90-0.9=89.1°；89.5° 落在 pad 缝但名义仍属槽 0
    expect(slotOf(89.5, 4)).toBe(0)
    // 90.5° 已过名义界，归槽 1（其楔形从 90+0.9 起绘，90.5 也在缝内但仍属槽 1）
    expect(slotOf(90.5, 4)).toBe(1)
  })

  it('任意负角/超 360 角归一：-90°→槽3、370°→槽0（360/4）', () => {
    expect(slotOf(-90, 4)).toBe(3)
    expect(slotOf(370, 4)).toBe(0)
  })

  it('n 非整除也恒返回 [0,n) 内合法索引（n=3 覆盖 118/119/359）', () => {
    expect(slotOf(118, 3)).toBe(0) // step=120
    expect(slotOf(121, 3)).toBe(1)
    expect(slotOf(359, 3)).toBe(2)
  })
})
