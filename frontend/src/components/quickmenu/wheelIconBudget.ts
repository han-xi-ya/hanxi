// 轮盘真图标像素预算（机主反馈 2026-09-26："图标设置成小的情况下，就很模糊"）。
//
// 病灶实证：assets/apps/*.png 全部是 32×32 单档位图（scripts/extract_app_icons.ps1
// -Px 32 归一产物，PNG IHDR 逐枚核过），而扇区图标按密度档以 15/16/18/22 CSS px
// 绘制，尺寸从未按 devicePixelRatio 出预算——150% 缩放下 22 CSS px = 33 物理 px，
// 32px 源被 1.03× 拉伸；125% 下 15 CSS px = 18.75 物理 px 的分数栅格。两者都走
// 浏览器双线性重采样，观感即"发糊"。矢量档（i: 注册表图标）由渲染器按物理分辨率
// 即时重绘、任何 DPR 都锐利，故只有 app: 真图标中招——与机主"软件图标发糊"的
// 描述严格吻合。
//
// 策略（纯函数，组件侧只对 app: 位图消费；根治还要重切 64px 源档，见交付报告）：
//  1. 物理边长取整——杜绝分数设备像素的亚像素重采样糊；
//  2. 永不放大——物理目标超过源档即钉死 1:1（宁可小半档，不可糊一档）；
//  3. 降采样区间向源的干净二分档（32/16…）吸附（差 ≤ SNAP_TOL_PX 物理 px 时），
//     整数比降采样是 1:1 之下最锐利的档位。
// 返回值仍是 CSS px（物理边长 / dpr），直接喂 <AppIcon :size>。

/** assets/apps 位图统一分辨率（与提取脚本 -Px 默认值同源；改脚本须同步此值）。 */
export const APP_ICON_SRC_PX = 32

/** 干净二分档吸附容差（物理 px）：差在容差内才换挡，否则保持期望边长。 */
const SNAP_TOL_PX = 2

/** 图标最小物理边长：再小不可辨，也不参与二分档吸附。 */
const MIN_PHYS_PX = 12

/** app: 真图标（位图轨）判定——i: 矢量轨无需预算。 */
export function isRasterWheelIcon(icon: string): boolean {
  return icon.startsWith('app:')
}

/**
 * 期望 CSS px × DPR → 吸附后 CSS px。
 * 保证：物理边长恒为整数且 ≤ srcPx（永不放大）；期望值本身已落在干净档上时
 * 原样返回。非法入参（NaN/非正）原样退回，由调用方布局自洽兜底。
 */
export function snapIconCssPx(desiredCssPx: number, dpr: number, srcPx: number = APP_ICON_SRC_PX): number {
  if (!Number.isFinite(desiredCssPx) || desiredCssPx <= 0) return desiredCssPx
  const ratio = Number.isFinite(dpr) && dpr > 0 ? dpr : 1
  const target = desiredCssPx * ratio
  // 预算封顶：物理需求超过源档即钉 1:1（源不足是资产欠账，前端不造假分辨率）
  if (target >= srcPx) return srcPx / ratio
  // 向源的二分干净档（srcPx、srcPx/2、srcPx/4…）取最近，容差内换挡
  let best = srcPx
  let bestD = Math.abs(srcPx - target)
  for (let s = srcPx / 2; s >= MIN_PHYS_PX; s /= 2) {
    const d = Math.abs(s - target)
    if (d < bestD) {
      bestD = d
      best = s
    }
  }
  const phys = bestD <= SNAP_TOL_PX ? best : Math.round(target)
  return Math.min(phys, srcPx) / ratio
}
