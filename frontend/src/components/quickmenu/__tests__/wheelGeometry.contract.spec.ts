// 轮盘几何跨语言契约 + 共享件推导回锁（N5 老账"前后端几何不同源"的防回潮锁）。
//
// 三层：
//  1. Go 源文本对账——internal/modules/quickmenu/service.go 的 popupWidth/popupHeight/
//     popupMargin 编译常量与前端 WHEEL.size/POPUP_MARGIN_DIP 逐值相等、派生关系成立
//     （半窗−边距 = rCapOut = R_DISMISS）。512 无法经 bindings/事件下发到前端
//     （quickmenuservice.js 无任何几何出口），双写由本 spec 焊死：改 Go 不改 TS 或反之，
//     此处直接红。形制循 constants/__tests__/contract-enum.spec.ts（readFileSync + 正则提取，
//     提取空集即红防假绿）。
//  2. trim 窗推导回锁——wheelGeometry.trimPad/trimViewBox 与 v3（788b95e）手量常量
//     [82, 430]（viewBox "82 82 348 348"）逐值相等：数学改了出处，没改观感。
//  3. 密度档回锁——densityFor 四档与 QuickMenuPopup v3 内联字面量表逐值相等，
//     CAP_ICON/PREVIEW_ICON 与原组件裸值相等（迁移纯归口，零语义变更）。
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  WHEEL, POPUP_MARGIN_DIP, R_DISMISS,
  DISC_INK_R, TRIM_INK_HALF, TRIM_SAFE, trimPad, trimViewBox, fullViewBox, toTrimWindowPercent,
  densityFor, CAP_ICON, PREVIEW_ICON, mainAnchor,
} from '../wheelGeometry'

// ── 第 1 层：Go 编译常量 ↔ 前端共享件 ──

const goSource = readFileSync(
  join(process.cwd(), '..', 'internal', 'modules', 'quickmenu', 'service.go'),
  'utf8',
)

/** 提取 Go const 块中 `name = 数字`（容忍行内注释），无产出即抛——防正则失效假绿。 */
function goIntConst(name: string): number {
  const match = new RegExp(`^\\s*${name}\\s*=\\s*(\\d+)`, 'm').exec(goSource)
  if (!match) throw new Error(`service.go 未提取到常量 ${name}（Go 侧改名/删除/换写法，对账契约需同步检修）`)
  return Number(match[1])
}

describe('quickmenu 弹窗几何 Go↔TS 常量对（service.go 源文本对账）', () => {
  it('WHEEL.size === popupWidth === popupHeight（512 双写焊死）', () => {
    expect(goIntConst('popupWidth')).toBe(WHEEL.size)
    expect(goIntConst('popupHeight')).toBe(WHEEL.size)
  })

  it('POPUP_MARGIN_DIP === popupMargin', () => {
    expect(goIntConst('popupMargin')).toBe(POPUP_MARGIN_DIP)
  })

  it('派生关系：半窗 − 边距 = 帽带外缘 rCapOut = R_DISMISS；取消圆 rCancel 不出窗口', () => {
    // 后端 dismissIfOutside：判定半径 = 半窗 − margin×scale（物理），DIP 口径即
    // size/2 − popupMargin；该圆与前端帽带外缘 236 重合是"点投影圈即收"的设计前提。
    expect(WHEEL.size / 2 - goIntConst('popupMargin')).toBe(WHEEL.rCapOut)
    expect(R_DISMISS).toBe(WHEEL.rCapOut)
    expect(WHEEL.rCancel).toBeLessThan(WHEEL.size / 2) // 取消圆（视觉态）仍在窗内，两圆职责不同
  })
})

// ── 第 2 层：trim 窗推导回锁 v3 手量值 ──

describe('trim 裁切窗推导 = v3（788b95e）手量常量', () => {
  it('墨迹半经：rDisc−1.25 圆 + 描边外半 = 169.5，上界取整 + 安全边 4 = 174', () => {
    expect(DISC_INK_R).toBeCloseTo(169.5, 9)
    expect(TRIM_SAFE).toBe(4)
    expect(TRIM_INK_HALF).toBe(174)
  })

  it('trimPad 512 窗 → 82；viewBox → "82 82 348 348"（即手量窗 [82, 430]）', () => {
    expect(trimPad()).toBe(82)
    expect(trimPad(WHEEL.size)).toBe(82)
    expect(trimViewBox()).toBe('82 82 348 348')
    expect(82 + 348).toBe(430) // 窗右/下缘与 v3 注释 [82,430] 对得上
  })

  it('全窗 viewBox 与弹窗模板原串 `0 0 512 512` 逐字相等', () => {
    expect(fullViewBox()).toBe('0 0 512 512')
  })

  it('toTrimWindowPercent = v3 组件内 toWindow 的算式镜像（50% → 50%、窗缘点折 0/100%）', () => {
    expect(parseFloat(toTrimWindowPercent('50%'))).toBeCloseTo(50, 9) // 圆心恒映窗心
    expect(parseFloat(toTrimWindowPercent(`${(82 / 512) * 100}%`))).toBeCloseTo(0, 9)
    expect(parseFloat(toTrimWindowPercent(`${(430 / 512) * 100}%`))).toBeCloseTo(100, 9)
    // 与旧公式逐值互证：n=4 第 0 枚主锚点（mainAnchor 输出 → 窗内 %）
    const a = mainAnchor(0, 4)
    const legacy = (s: string) => (((parseFloat(s) / 100) * 512 - 82) / (512 - 82 * 2)) * 100
    expect(parseFloat(toTrimWindowPercent(a.left))).toBeCloseTo(legacy(a.left), 9)
    expect(parseFloat(toTrimWindowPercent(a.top))).toBeCloseTo(legacy(a.top), 9)
  })
})

// ── 第 3 层：密度档/图标基数迁移回锁 ──

describe('densityFor 四档 = QuickMenuPopup v3 内联字面量（迁移零语义变更）', () => {
  it('档值逐字锁定', () => {
    expect(densityFor(0)).toEqual({ btnW: 64, well: 36, icon: 22, name: 12, pop: 8 })
    expect(densityFor(6)).toEqual({ btnW: 64, well: 36, icon: 22, name: 12, pop: 8 })
    expect(densityFor(7)).toEqual({ btnW: 58, well: 32, icon: 18, name: 12, pop: 8 })
    expect(densityFor(9)).toEqual({ btnW: 58, well: 32, icon: 18, name: 12, pop: 8 })
    expect(densityFor(10)).toEqual({ btnW: 52, well: 28, icon: 16, name: 11, pop: 5 })
    expect(densityFor(12)).toEqual({ btnW: 52, well: 28, icon: 16, name: 11, pop: 5 })
    expect(densityFor(13)).toEqual({ btnW: 48, well: 26, icon: 15, name: 11, pop: 5 })
  })

  it('帽带/预览图标基数 = 原组件裸值（15/16）', () => {
    expect(CAP_ICON).toBe(15)
    expect(PREVIEW_ICON).toBe(16)
  })
})
