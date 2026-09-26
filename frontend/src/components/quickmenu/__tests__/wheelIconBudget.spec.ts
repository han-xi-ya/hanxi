// 图标像素预算特征测试：小/中/大密度档 × DPR 1/1.5/2 全矩阵，锁三不变式——
// 物理边长恒整数、恒不放大（≤32）、已落在干净档时期望值不被无端改动。
import { describe, expect, it } from 'vitest'
import { APP_ICON_SRC_PX, isRasterWheelIcon, snapIconCssPx } from '../wheelIconBudget'

const phys = (cssPx: number, dpr: number) => cssPx * dpr

describe('snapIconCssPx：密度档 × DPR 矩阵', () => {
  const tiers = { 大: 22, 中: 18, 小: 15, 帽带: 16 }
  const dprs = [1, 1.5, 2]

  for (const [name, css] of Object.entries(tiers)) {
    for (const dpr of dprs) {
      it(`${name}档 ${css}px × DPR ${dpr}`, () => {
        const out = snapIconCssPx(css, dpr)
        expect(out).toBeGreaterThan(0)
        // 永不放大：物理边长 ≤ 源档；且不比期望长出容差(2px)以外（只吸不吹）
        expect(phys(out, dpr)).toBeLessThanOrEqual(APP_ICON_SRC_PX + 1e-9)
        expect(phys(out, dpr)).toBeLessThanOrEqual(css * dpr + 2 + 1e-9)
        expect(Math.abs(phys(out, dpr) - Math.round(phys(out, dpr)))).toBeLessThan(1e-9) // 物理整数
      })
    }
  }

  it('DPR 1 整数期望原样保留（22 落 16/32 干净档容差外 → 不换挡不缩号）', () => {
    expect(snapIconCssPx(22, 1)).toBe(22)
  })

  it('DPR 1 邻干净档吸附：18→16、15→16（整数比 2:1 降采样最锐）', () => {
    expect(snapIconCssPx(18, 1)).toBe(16)
    expect(snapIconCssPx(15, 1)).toBe(16)
  })

  it('DPR 1.5 放大预算封顶：22px(=33物理) → 钉 1:1 的 21.33px', () => {
    const out = snapIconCssPx(22, 1.5)
    expect(phys(out, 1.5)).toBeCloseTo(APP_ICON_SRC_PX, 6)
  })

  it('DPR 2 大档预算封顶 → css 16px（32 物理 1:1），小档物理钉 32 干净', () => {
    expect(phys(snapIconCssPx(22, 2), 2)).toBeCloseTo(32, 6)
    expect(phys(snapIconCssPx(15, 2), 2)).toBeCloseTo(32, 6) // 30 → 吸 32（容差 2）
    expect(snapIconCssPx(16, 2)).toBe(16) // 恰 1:1 不动
  })

  it('非法入参原样退回（NaN/负数由调用方兜底，不产 NaN 尺寸）', () => {
    expect(snapIconCssPx(Number.NaN, 2)).toBe(Number.NaN)
    expect(snapIconCssPx(-4, 2)).toBe(-4)
    expect(snapIconCssPx(22, 0)).toBe(22) // dpr 坏值按 1 处理
  })

  it('源档参数化：未来资产升 64px 时同函数即得 44→44（预算翻倍不放大不吸附换挡）', () => {
    expect(snapIconCssPx(22, 2, 64)).toBe(22) // 44 物理 < 64：无吸附档在容差内 → 取整 44/2
  })
})

describe('isRasterWheelIcon', () => {
  it('仅 app: 位图轨吃预算，i: 矢量与裸注册名不吃', () => {
    expect(isRasterWheelIcon('app:snipaste')).toBe(true)
    expect(isRasterWheelIcon('box')).toBe(false)
    expect(isRasterWheelIcon('terminal')).toBe(false)
  })
})
