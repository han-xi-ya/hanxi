// "跟随模块色"取色端（Vue + canvas，非纯函数侧在 wheelSkinColors）：把 app: 真
// 图标位图缩进 24×24 离屏 canvas 采样，喂 dominantIconColor 得主色调。
// 纪律：
//  - 位图与页面同源（vite 打包进 assetserver），canvas 不被跨源污染，getImageData 合法；
//  - 结果按图标名模块级缓存（正负都缓存，失败不重试不刷屏）；任何环节不可用
//    （无 document/解码失败/tainted）都如实返回 null，消费面回落类型色 token；
//  - 竞态：seq 门闩，皮肤开关连拨/条目热改时旧批采样作废，不写脏。
import { ref, type Ref } from 'vue'
import { appIconUrl } from '../../constants/appIcons'
import { isRasterWheelIcon } from './wheelIconBudget'
import { dominantIconColor, type Hsl } from './wheelSkinColors'

const SAMPLE_PX = 24

/** 图标名 → 主色（null=取不到色，负缓存）。模块级：弹窗常驻复用不重采。 */
const tintCache = new Map<string, Hsl | null>()

function loadSampled(url: string): Promise<Uint8ClampedArray | null> {
  if (typeof document === 'undefined') return Promise.resolve(null)
  return new Promise((resolve) => {
    const img = new Image()
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas')
        canvas.width = SAMPLE_PX
        canvas.height = SAMPLE_PX
        const ctx = canvas.getContext('2d')
        if (!ctx) return resolve(null)
        ctx.drawImage(img, 0, 0, SAMPLE_PX, SAMPLE_PX)
        resolve(ctx.getImageData(0, 0, SAMPLE_PX, SAMPLE_PX).data)
      } catch {
        resolve(null) // tainted canvas / 绘制失败：如实无主色
      }
    }
    img.onerror = () => resolve(null)
    img.src = url
  })
}

/** 单个图标名的主色（缓存命中同步返回）；null = 无模块色（灰图标/取不到）。 */
async function iconDominant(icon: string): Promise<Hsl | null> {
  if (!isRasterWheelIcon(icon)) return null
  const hit = tintCache.get(icon)
  if (hit !== undefined) return hit
  const data = await loadSampled(appIconUrl(icon.slice(4)))
  const hsl = data ? dominantIconColor(data) : null
  tintCache.set(icon, hsl)
  return hsl
}

/**
 * 消费面入口：喂 `icons`（图标名收集器）与开关/盘透明度读数，得到响应式
 * `tints[icon] → css` 映射与手动 `refresh()`（items/皮肤变化时由组件调用）。
 */
export function useWheelTints(
  icons: () => string[],
  enabled: () => boolean,
): { tints: Ref<Record<string, Hsl | null>>; refresh: () => void } {
  const tints = ref<Record<string, Hsl | null>>({})
  let seq = 0

  async function refresh(): Promise<void> {
    if (!enabled()) return
    const mine = ++seq
    for (const icon of new Set(icons())) {
      if (mine !== seq) return // 更新的批次已起跑：本批作废防脏写
      if (icon in tints.value) continue
      const dom = await iconDominant(icon)
      if (mine !== seq) return
      tints.value = { ...tints.value, [icon]: dom }
    }
  }

  return { tints, refresh }
}
