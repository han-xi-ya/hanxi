// 轮盘皮肤色算纯函数：从 app: 真图标 PNG 派生"模块色"（扇区描边 tint）与盘面
// 纱色梯度。无 Vue / 无 DOM 依赖——位图像素由调用方加载后以 Uint8ClampedArray
// 喂入（QuickMenuPopup 用 <img>+offscreen canvas 取数），本模块只管色彩数学。
//
// 纪律口径：派生色是**条目数据的呈现**（类似头像/封面色），不是设计系统色板，
// 因此不违"观感走 token 不造硬编码色值"红线——token 皮肤层（盘底/扇区底/文字）
// 全部仍由 QuickMenuPopup.vue 的 CSS var(--…) 承担；本模块产出的色只经 CSS 变量
// 注入且一律再经 color-mix 降饱和进盘面，杜绝荧光跳色。

/** 简单 RGB 三元组，通道 0-255。 */
export interface Rgb {
  r: number
  g: number
  b: number
}

/** HSL 三元组：h 为角度 0-360，s/l 为 0-1。 */
export interface Hsl {
  h: number
  s: number
  l: number
}

export function rgbToHsl({ r, g, b }: Rgb): Hsl {
  const rn = r / 255
  const gn = g / 255
  const bn = b / 255
  const max = Math.max(rn, gn, bn)
  const min = Math.min(rn, gn, bn)
  const l = (max + min) / 2
  if (max === min) return { h: 0, s: 0, l }
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h: number
  switch (max) {
    case rn: h = ((gn - bn) / d + (gn < bn ? 6 : 0)) * 60; break
    case gn: h = ((bn - rn) / d + 2) * 60; break
    default: h = ((rn - gn) / d + 4) * 60
  }
  return { h, s, l }
}

export function hslToCss({ h, s, l }: Hsl, alpha = 1): string {
  const hue = ((h % 360) + 360) % 360
  const sp = Math.min(Math.max(s, 0), 1) * 100
  const lp = Math.min(Math.max(l, 0), 1) * 100
  return alpha >= 1
    ? `hsl(${hue.toFixed(1)} ${sp.toFixed(1)}% ${lp.toFixed(1)}%)`
    : `hsl(${hue.toFixed(1)} ${sp.toFixed(1)}% ${lp.toFixed(1)}% / ${alpha})`
}

/**
 * 图标像素 → 模块色：把像素按色相 12 桶（6 档暖冷×…）聚类，取"饱和度×出现量"
 * 加权最强的一簇均色——主色调应来自占视觉重量的大色块，而非零星高饱和噪点。
 * 全灰图标（无彩色簇）、空像素集返回 null，调用方回落类型色 token。
 * 结果钳进工作台的克制色域：饱和度 ≤0.45、亮度 0.35–0.72（暗盘不刺眼、亮盘不漂）。
 */
export function dominantIconColor(pixels: Uint8ClampedArray | Uint8Array): Hsl | null {
  const HUE_BINS = 12
  const binCount = new Array<number>(HUE_BINS).fill(0)
  const binSat = new Array<number>(HUE_BINS).fill(0)
  const binR = new Array<number>(HUE_BINS).fill(0)
  const binG = new Array<number>(HUE_BINS).fill(0)
  const binB = new Array<number>(HUE_BINS).fill(0)
  let achromat = { r: 0, g: 0, b: 0, n: 0 }

  for (let i = 0; i + 3 < pixels.length; i += 4) {
    const a = pixels[i + 3]
    if (a < 24) continue // 近透明像素不参与（图标圆角外的空角）
    const rgb: Rgb = { r: pixels[i], g: pixels[i + 1], b: pixels[i + 2] }
    const { h, s, l } = rgbToHsl(rgb)
    if (s < 0.12 || l < 0.08 || l > 0.95) {
      achromat = { r: achromat.r + rgb.r, g: achromat.g + rgb.g, b: achromat.b + rgb.b, n: achromat.n + 1 }
      continue
    }
    const bin = Math.min(HUE_BINS - 1, Math.floor((h / 360) * HUE_BINS))
    binCount[bin] += 1
    binSat[bin] += s
    binR[bin] += rgb.r
    binG[bin] += rgb.g
    binB[bin] += rgb.b
  }

  let best = -1
  let bestScore = 0
  for (let b = 0; b < HUE_BINS; b++) {
    if (binCount[b] === 0) continue
    const score = binCount[b] * (binSat[b] / binCount[b]) // count × mean saturation
    if (score > bestScore) {
      bestScore = score
      best = b
    }
  }
  if (best < 0) return null // 全灰图标：如实无模块色，不硬造
  const n = binCount[best]
  const mean = rgbToHsl({ r: binR[best] / n, g: binG[best] / n, b: binB[best] / n })
  return { h: mean.h, s: Math.min(mean.s, 0.45), l: Math.min(Math.max(mean.l, 0.35), 0.72) }
}

/** 类型轨模块色（矢量/未知图标的回落）：与 WHEEL_TYPE_ICON 同族的四型。 */
export type WheelTypeTint = 'exe' | 'command' | 'route' | 'group'
export function wheelTypeTint(type: string): WheelTypeTint {
  return type === 'command' || type === 'route' || type === 'group' ? type as WheelTypeTint : 'exe'
}
