// 留言牌 · 度量与缩放纯函数模块（v4 牌面子组件收编）。
//
// 收编动机：牌面定标（卡宽系数 ×13、副行系数 ×0.42）与预览缩放换算
// （grow/fit/cap 三值取小、--bc-max-h 反算）此前内联在 MsgBoardView 的
// script 里——BoardCard 与宿主各握一份系数，就是"牌面双真相"的雏形。
// 本模块把它们抽成零 Vue、零 DOM 的纯函数：BoardCard/BoardStage 从这里
// 取数，宿主（A 路舞台）接线同一函数，数值口径单源。
//
// 两套缩放口径并存在此（都是纯函数、都可测）：
//   stageScale——v3 grow/fit/cap 旧档，数值与 v3 视图实现逐位一致
//     （scale(0.34)/scale(0.18461538461538463) 逐字复现）；
//   stageScaleMeasured——v4"量实测不猜 ×13"的牌桌档，与 A 路 v4 重写的
//     内联式逐位一致（64px/300 盒＝288/832＝0.34615…，封顶 1.0）。
// BoardStage 默认走 measured；宿主想沿用哪档都从同一模块取数。

/** 卡宽上限＝字号 ×13（"约 13 个标题字宽"的定标契约，BoardCard 与防裁换算共用） */
export const CARD_WIDTH_FACTOR = 13
/** 副行字号＝主题字号 ×0.42（显式像素换算，防 em 基准漂移的假联动） */
export const SUB_FONT_FACTOR = 0.42
/** 副行字号下限：再小的字也不印成噪点 */
export const SUB_FONT_MIN = 12
/** 预览缩放基准：300px 盒时的观感（v2 常数，v3 沿用为换算原点） */
export const PREVIEW_BASE_SCALE = 0.34
/** 基准缩放对应的盒宽（RO 测不到时——happy-dom/首帧——也回落这条常数线） */
export const PREVIEW_BASE_BOX_W = 300
/** 盒内边距容纳线：卡宽/超高换算前各扣 12px，防贴边裁切 */
export const PREVIEW_EDGE_PAD = 12
/** 页内小预览封顶：预览再大也不劫持舞台柱（挂出仍是唯一 1:1 形态） */
export const PREVIEW_GROW_CAP = 0.8
/** 舞台封顶：BoardStage 默认允许缩到 1.0——"看真实大小"的大预览形态 */
export const STAGE_SCALE_CAP = 1.0
/** --bc-max-h 反算下限：盒再矮也给牌留 48px 可视高度 */
export const CARD_MIN_MAX_H = 48
/**
 * 缩放地板：真实宿主（盒宽 CSS 有下限）永远碰不到，只防 degenerate 入参
 * 把负数/零 scale 漏进 transform（镜像翻转事故）。与视图内联式的逐位一致
 * 口径只在正常值域成立——视图现实现同样没有这道闸，靠 CSS 下限兜底。
 */
export const MIN_SCALE = 0.001

/** 卡自然宽（px）＝字号 ×13，防裁切换算的横轴基准（不取整，供比值运算） */
export function cardNaturalWidth(fontSize: number): number {
  return fontSize * CARD_WIDTH_FACTOR
}

/** 卡宽上限内联样式取整值（px）——BoardCard maxWidth 的 min(round(×13), 88vw) 前半段 */
export function cardMaxWidthPx(fontSize: number): number {
  return Math.round(fontSize * CARD_WIDTH_FACTOR)
}

/** 副行字号（px）＝max(12, round(×0.42))——BoardCard 内联换算唯一口径 */
export function subFontSizePx(fontSize: number): number {
  return Math.max(SUB_FONT_MIN, Math.round(fontSize * SUB_FONT_FACTOR))
}

/** 牌面主题＝正文第一行（版式契约：表情即首字符，无表情即纯文字便利贴） */
export function boardTitleOf(text: string): string {
  return text.split('\n', 1)[0] ?? ''
}

/** 牌面副行＝其余行；trailing 空行不产生空气副行（与收编前 BoardCard 逐字同行为） */
export function boardSubOf(text: string): string {
  const rest = text.split('\n').slice(1)
  while (rest.length && rest[rest.length - 1].trim() === '') rest.pop()
  return rest.join('\n').trim()
}

/** stageScale 入参：boxWidth 为盒实测宽（px），<=0/NaN 视作未测到、回落基准常数 */
export interface StageScaleInput {
  fontSize: number
  boxWidth: number
  /** 缩放封顶：舞台大预览用默认 1.0；页内小预览传 PREVIEW_GROW_CAP(0.8) */
  cap?: number
  /** 缩放原点档，默认 PREVIEW_BASE_SCALE / PREVIEW_BASE_BOX_W 成对覆盖 */
  baseScale?: number
  baseBoxWidth?: number
  /** 容纳边距，默认 PREVIEW_EDGE_PAD */
  edgePad?: number
}

/**
 * 自适应缩放（grow/fit/cap 三值取小，与 MsgBoardView v3 previewScale 逐位同式）：
 *   grow = 基准 × (盒宽/基准盒宽) —— 盒越宽牌越大；
 *   fit  = (盒宽−12)/(字号×13)     —— 卡宽上限防横向裁切，越界自动收缩；
 *   cap  = 封顶（默认 1.0，即"输出 1:1 大预览"的舞台上限契约）。
 */
export function stageScale(input: StageScaleInput): number {
  const baseScale = input.baseScale ?? PREVIEW_BASE_SCALE
  const baseBoxWidth = input.baseBoxWidth ?? PREVIEW_BASE_BOX_W
  const edgePad = input.edgePad ?? PREVIEW_EDGE_PAD
  const cap = input.cap ?? STAGE_SCALE_CAP
  const w = input.boxWidth > 0 ? input.boxWidth : baseBoxWidth
  const grow = baseScale * (w / baseBoxWidth)
  const fit = (w - edgePad) / cardNaturalWidth(input.fontSize)
  return Math.max(MIN_SCALE, Math.min(cap, grow, fit))
}

/** stageScaleMeasured 入参：cardWidth 为实测牌面自然布局宽（RO 量得），<=0 回落 字号×13 兜底线 */
export interface StageScaleMeasuredInput {
  boxWidth: number
  cardWidth: number
  fontSize: number
  /** 缩放封顶：舞台大预览默认 1.0 */
  cap?: number
  /** 容纳边距，默认 PREVIEW_EDGE_PAD */
  pad?: number
}

/**
 * v4 牌桌口径（A 路 MsgBoardView v4 重写现用公式的逐位镜像）：
 *   scale = min(cap, (盒宽 − pad) / 卡片实际宽)
 * "能 1:1 就 1:1"——短文案在宽舞台原大呈现；防裁切从"猜 字号×13"升级为
 * "量实测自然宽 ×13 线只作首帧/测不到时的兜底"。与视图内联式同式，
 * 差异仅 MIN_SCALE 地板（真实值域不可达，见上注）。
 */
export function stageScaleMeasured(input: StageScaleMeasuredInput): number {
  const cap = input.cap ?? STAGE_SCALE_CAP
  const pad = input.pad ?? PREVIEW_EDGE_PAD
  const w = input.boxWidth > 0 ? input.boxWidth : PREVIEW_BASE_BOX_W
  const natural = input.cardWidth > 0 ? input.cardWidth : cardNaturalWidth(input.fontSize)
  return Math.max(MIN_SCALE, Math.min(cap, (w - pad) / natural))
}

/**
 * 超高裁剪反算（px）：BoardCard 的 --bc-max-h ＝ (盒高−12)/缩放——"只裁不滚"的
 * 裁剪线随盒收在预览框内。scale<=0（异常入参）回落下限常数，不产出 Infinity。
 */
export function cardMaxHeightPx(boxHeight: number, scale: number): number {
  if (!(scale > 0)) return CARD_MIN_MAX_H
  return Math.max(CARD_MIN_MAX_H, Math.floor((boxHeight - PREVIEW_EDGE_PAD) / scale))
}

/** 大白话缩放百分比（下限 1%）：previewPct 的收编口径 */
export function scalePercent(scale: number): number {
  return Math.max(1, Math.round(scale * 100))
}

/** 真牌相对预览的放大倍数（下限 1 倍）：previewZoom 的收编口径 */
export function scaleZoomFactor(scale: number): number {
  return Math.max(1, Math.round(1 / scale))
}
