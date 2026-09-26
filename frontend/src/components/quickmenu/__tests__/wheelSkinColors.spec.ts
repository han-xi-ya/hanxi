// 皮肤色算特征测试：主色簇提取（占量×饱和度加权，噪点不夺主）、全灰回落 null、
// 色域钳制、类型色回落轨、tint→CSS 随盘透明度压亮度。
import { describe, expect, it } from 'vitest'
import { dominantIconColor, rgbToHsl, wheelTypeTint } from '../wheelSkinColors'
import { wheelTintCss } from '../wheelSkin'

/** 造像素：n 个 (r,g,b,a) 同色像素。 */
const px = (n: number, r: number, g: number, b: number, a = 255) => {
  const arr = new Uint8ClampedArray(n * 4)
  for (let i = 0; i < n; i++) {
    arr[i * 4] = r
    arr[i * 4 + 1] = g
    arr[i * 4 + 2] = b
    arr[i * 4 + 3] = a
  }
  return arr
}
const merge = (...parts: Uint8ClampedArray[]) => {
  const out = new Uint8ClampedArray(parts.reduce((s, p) => s + p.length, 0))
  let o = 0
  for (const p of parts) {
    out.set(p, o)
    o += p.length
  }
  return out
}

describe('rgbToHsl', () => {
  it('原色三兄弟与灰阶', () => {
    expect(rgbToHsl({ r: 255, g: 0, b: 0 })).toMatchObject({ h: 0, s: 1 })
    expect(rgbToHsl({ r: 0, g: 255, b: 0 })).toMatchObject({ h: 120 })
    expect(rgbToHsl({ r: 0, g: 0, b: 255 })).toMatchObject({ h: 240 })
    expect(rgbToHsl({ r: 128, g: 128, b: 128 }).s).toBe(0)
  })
})

describe('dominantIconColor', () => {
  it('红色主导图标 → h≈0；零星蓝噪点不夺主', () => {
    const hsl = dominantIconColor(merge(px(200, 200, 40, 40), px(3, 30, 60, 240)))
    expect(hsl).not.toBeNull()
    expect(hsl!.h).toBeLessThan(20)
  })

  it('全灰图标如实返回 null（宁回落类型色不硬造彩色）', () => {
    expect(dominantIconColor(px(100, 160, 160, 160))).toBeNull()
    expect(dominantIconColor(new Uint8ClampedArray(0))).toBeNull()
  })

  it('透明像素不参与（图标空角不误报灰/彩色）', () => {
    const hsl = dominantIconColor(merge(px(80, 0, 0, 0, 0), px(40, 30, 180, 90)))!
    expect(hsl.h).toBeGreaterThan(120)
    expect(hsl.h).toBeLessThan(180)
  })

  it('色域钳制：荧光跳色被收进 s≤0.45 / l∈[0.35,0.72] 工作台色域', () => {
    const hsl = dominantIconColor(px(300, 255, 0, 255))! // 纯品红 s=1 l=0.5
    expect(hsl.s).toBeLessThanOrEqual(0.45)
    expect(hsl.l).toBeGreaterThanOrEqual(0.35)
    expect(hsl.l).toBeLessThanOrEqual(0.72)
  })
})

describe('wheelTypeTint / wheelTintCss', () => {
  it('四类各归其位，未知类型落 exe 轨', () => {
    expect(wheelTypeTint('command')).toBe('command')
    expect(wheelTypeTint('route')).toBe('route')
    expect(wheelTypeTint('group')).toBe('group')
    expect(wheelTypeTint('exe')).toBe('exe')
    expect(wheelTypeTint('weird')).toBe('exe')
  })

  it('无主色出空串；实色出 hsl()；盘越透色越压深', () => {
    expect(wheelTintCss(null, 1)).toBe('')
    const solid = wheelTintCss({ h: 200, s: 0.4, l: 0.6 }, 1)
    expect(solid).toMatch(/^hsl\(200\.0 40\.0% 60\.0%\)$/)
    const onVeil = wheelTintCss({ h: 200, s: 0.4, l: 0.6 }, 0.35)
    const lOf = (s: string) => Number(/(\d+\.\d)%\)$/.exec(s)![1])
    expect(lOf(onVeil)).toBeLessThan(lOf(solid))
  })
})
