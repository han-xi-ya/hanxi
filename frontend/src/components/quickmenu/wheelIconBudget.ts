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
// 策略（纯函数，组件侧只对 app: 位图消费；2026-09-26 资产已升切 64 封顶，
// 本表按 sources.go 台账逐枚给源——放大凑数与静默小图两头都堵死）：
//  1. 物理边长取整——杜绝分数设备像素的亚像素重采样糊；
//  2. 永不放大——物理目标超过源档即钉死 1:1（宁可小半档，不可糊一档）；
//  3. 降采样区间向源的干净二分档（64/32/16…）吸附（差 ≤ SNAP_TOL_PX 物理 px 时），
//     整数比降采样是 1:1 之下最锐利的档位。
// 返回值仍是 CSS px（物理边长 / dpr），直接喂 <AppIcon :size>。

/** assets/apps 展示档默认分辨率（升切批出货主档；改提取脚本 -Px 须同步此值与台账）。 */
export const APP_ICON_SRC_PX = 64

/** 逐枚源尺寸台账（对齐 internal/app/appicons/sources.go 的 26 键；升切批出货实况：
 *  24 枚 64 封顶、ddnsgo/frpc 源天花板 48；generic 自绘 32 不入册，托盘菜单变体不
 *  走本预算。新件入库须同批登记，未登记名保守按 32 档防放大凑数）。导出供测试对账。 */
export const ICON_SRC_LEDGER: Record<string, number> = {
  ...Object.fromEntries(
    [
      'bcu', 'bili23', 'ccswitch', 'douzy', 'eartrumpet', 'everything', 'flclash',
      'guoheview', 'keyviz', 'litemonitor', 'mangodisk', 'markeron', 'nanazip',
      'papertodo', 'paseo', 'piclite', 'quicklook', 'rufus', 'rustdesk', 'snipaste',
      'subnetdesk', 'termora', 'translucenttb', 'windterm',
    ].map((id) => [id, APP_ICON_SRC_PX] as const),
  ),
  ddnsgo: 48,
  frpc: 48,
}

/** app: 图标名的源档查询——预算封顶按枚给，不再用统一常数（未知名=文件不在位，
 *  AppIcon 回落 32px 自绘 generic，保守取 32 防把通用徽图当 64 源放大）。 */
export function srcPxForWheelIcon(icon: string): number {
  const id = icon.startsWith('app:') ? icon.slice(4) : ''
  return ICON_SRC_LEDGER[id] ?? 32
}

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
