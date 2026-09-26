// 图标像素预算特征测试：小/中/大密度档 × DPR 1/1.5/2 全矩阵，锁三不变式——
// 物理边长恒整数、恒不放大（≤该枚源档台账）、已落在干净档时期望值不被无端改动。
// 2026-09-26 升切批后预算默认档 64（sources.go 台账），并锁前端台账与资产文件对账。
import { describe, expect, it } from 'vitest'
import {
  APP_ICON_SRC_PX,
  ICON_SRC_LEDGER,
  RUNTIME_ICON_SRC_LEDGER,
  isRasterWheelIcon,
  snapIconCssPx,
  srcPxForWheelIcon,
} from '../wheelIconBudget'

const phys = (cssPx: number, dpr: number) => cssPx * dpr

describe('snapIconCssPx：密度档 × DPR 矩阵（默认源档 64 台账）', () => {
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

  it('DPR 1 整数期望原样保留（22 落 16/32/64 干净档容差外 → 不换挡不缩号）', () => {
    expect(snapIconCssPx(22, 1)).toBe(22)
  })

  it('DPR 1 邻干净档吸附：18→16、15→16（整数比降采样最锐）', () => {
    expect(snapIconCssPx(18, 1)).toBe(16)
    expect(snapIconCssPx(15, 1)).toBe(16)
  })

  it('DPR 1.5 大档：22(=33 物理) 紧邻干净半档 32（容差内换挡 → css 21.33）', () => {
    const out = snapIconCssPx(22, 1.5)
    expect(phys(out, 1.5)).toBeCloseTo(32, 6)
  })

  it('DPR 2 升切红利：大档 44 物理不再被 32 源钉死，整数直出 css 22；小档 30 吸 32 干净', () => {
    expect(phys(snapIconCssPx(22, 2), 2)).toBeCloseTo(44, 6) // 64 源容得下：取整不换挡
    expect(snapIconCssPx(22, 2)).toBe(22)
    expect(phys(snapIconCssPx(15, 2), 2)).toBeCloseTo(32, 6) // 30 → 吸 32（容差 2）
    expect(snapIconCssPx(16, 2)).toBe(16) // 恰 32=64 半档 1:1 不动
  })

  it('非法入参原样退回（NaN/负数由调用方兜底，不产 NaN 尺寸）', () => {
    expect(snapIconCssPx(Number.NaN, 2)).toBe(Number.NaN)
    expect(snapIconCssPx(-4, 2)).toBe(-4)
    expect(snapIconCssPx(22, 0)).toBe(22) // dpr 坏值按 1 处理
  })

  it('源档参数化仍有效：32 旧源档调用（升切前回退/未登记件）大档被钉 1:1 css 16', () => {
    expect(snapIconCssPx(22, 2, 32)).toBe(16) // 44 ≥ 32 源 → 32 物理 1:1
    expect(snapIconCssPx(15, 2, 48)).toBe(15) // 48 源二分档 48/24/12，30 容差外取整
    expect(snapIconCssPx(11, 2, 48)).toBe(12) // 22 物理离 24 差 2 → 吸 24 干净半档
  })
})

describe('srcPxForWheelIcon：逐枚源尺寸台账', () => {
  it('在册 64 件、48 天花板件、未登记保守 32、非 app: 轨 32', () => {
    expect(srcPxForWheelIcon('app:snipaste')).toBe(64)
    expect(srcPxForWheelIcon('app:windterm')).toBe(64)
    expect(srcPxForWheelIcon('app:ddnsgo')).toBe(48)
    expect(srcPxForWheelIcon('app:frpc')).toBe(48)
    expect(srcPxForWheelIcon('app:nope-not-registered')).toBe(32) // generic 回落保守档
    expect(srcPxForWheelIcon('i:terminal')).toBe(32)
  })

  it('台账与 assets/apps 实际文件对账：每个非 generic 位图必登记且登记值 ∈ {64,48}（防新件漏登或放大凑数回潮）', () => {
    const files = import.meta.glob('../../../assets/apps/*.png')
    const ids = Object.keys(files).map((p) => p.slice(p.lastIndexOf('/') + 1, -'.png'.length))
    expect(ids.length).toBeGreaterThanOrEqual(26)
    for (const id of ids) {
      if (id === 'generic') continue // 自绘徽标不入册（托盘从不消费、轮盘走 AppIcon 回落档 32）
      expect(ICON_SRC_LEDGER[id], `asset ${id} 未登记源尺寸`).toBeTruthy()
      expect([64, 48], `asset ${id} 登记值异常`).toContain(ICON_SRC_LEDGER[id])
    }
    for (const id of Object.keys(ICON_SRC_LEDGER)) {
      expect(ids, `台账幽灵键 ${id} 无资产文件`).toContain(id)
    }
  })
})

describe('isRasterWheelIcon：app: 与矢量轨口径（存量回归锁）', () => {
  it('仅 app: 位图轨吃预算，i: 矢量与裸注册名不吃', () => {
    expect(isRasterWheelIcon('app:snipaste')).toBe(true)
    expect(isRasterWheelIcon('box')).toBe(false)
    expect(isRasterWheelIcon('terminal')).toBe(false)
  })
})

describe('isRasterWheelIcon / srcPxForWheelIcon：rt 运行期轨', () => {
  it('rt: 未提取成功时不吃位图预算（画回落矢量），源档按运行期台账给（宁标足额不放大）', () => {
    // 本测试环境无 wails 桥，runtimeIcons 恒未就绪 → isRaster=false（回落矢量轨）
    expect(isRasterWheelIcon('rt:rammap|i:gauge')).toBe(false)
    expect(srcPxForWheelIcon('rt:rammap|i:gauge')).toBe(32) // RAMMap64.exe 实测最大 32²
    expect(srcPxForWheelIcon('rt:vscode|i:code')).toBe(256)
    expect(srcPxForWheelIcon('rt:recordly|i:video')).toBe(256)
    expect(srcPxForWheelIcon('rt:nope|i:box')).toBe(32) // 未登记 rt id 保守档
  })
  it('运行期台账键集与红线三枚一致', () => {
    expect(Object.keys(RUNTIME_ICON_SRC_LEDGER).sort()).toEqual(['rammap', 'recordly', 'vscode'])
  })
})
