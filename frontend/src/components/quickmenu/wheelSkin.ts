// 轮盘皮肤账（机主拍板 2026-09-26"轮盘皮肤做"）：预设 × 盘面透明度 × 描边强度
// × 跟随模块色开关的模型、缺省兼容与持久化。
//
// 持久化通道现状（重要口径）：quickmenu 后端无皮肤字段可用（AppSettings 只有
// TwoTier/HoldMs/MovePx；本轮纪律禁碰后端与 bindings），故皮肤账走前端
// localStorage（键 hanxi.wheelSkin）——与 rail 展开态 / 面板折叠 / 最近导航同一
// "纯视觉开关，localStorage 持久化，不参与业务"哲学（AppNavRail/AppSidebar 先例），
// 且轮盘弹窗与主窗同源共享 localStorage：设置页存、弹窗经 storage 事件即时跟皮。
// 升级后端通道时**只需替换本模块读写侧**（GetConfig/SetConfig 契约已按下方
// WheelSkin 形状在交付报告中给出），消费面零改动。
//
// 缺省兼容红线：旧设置里没有这份账 = 读出默认皮肤；JSON 坏值/越界/未知预设
// 一律归一（normalize），任何输入都不抛错不炸盘。
import { hslToCss, type Hsl } from './wheelSkinColors'

export const WHEEL_SKIN_PRESETS = ['frost', 'veil', 'ink'] as const
export type WheelSkinPreset = (typeof WHEEL_SKIN_PRESETS)[number]

export const WHEEL_SKIN_PRESET_LABEL: Record<WheelSkinPreset, string> = {
  frost: '素瓷', // 现 N40③ 皮批基线：面板浅盘 + 浅纱花瓣
  veil: '雾青', // 盘面/花瓣染主色薄雾，随色板轴联动
  ink: '玄影', // 墨纱盘：文字 token 混深的暗盘，明暗主题皆可玄
}

/** 盘纱不透明度三档域（0.35 起：再低透出桌面噪点，盘面读不出"一块盘"）。 */
export const FACE_ALPHA_MIN = 0.35
export const FACE_ALPHA_MAX = 1
/** 描边强度 0–1：0=描边并入瓣色（缝内几乎不可见），1=模块/类型色满强度。 */
export const STROKE_MIN = 0
export const STROKE_MAX = 1

export interface WheelSkin {
  preset: WheelSkinPreset
  /** 盘面纱底不透明度（1=实底，越低越透） */
  faceAlpha: number
  /** 扇区描边强度：色源与瓣色的 color-mix 占比 0–1 */
  stroke: number
  /** 跟随模块色：描边色源从"类型 token"升级为条目真图标主色调 */
  followModuleColor: boolean
}

export const DEFAULT_WHEEL_SKIN: WheelSkin = {
  preset: 'frost',
  faceAlpha: 1,
  stroke: 0.55,
  followModuleColor: false,
}

/** 皮肤账本地键（与 hanxi.railExpanded 等纯视觉键同族）。 */
export const WHEEL_SKIN_STORAGE_KEY = 'hanxi.wheelSkin'

function clampNum(v: unknown, fallback: number, min: number, max: number): number {
  if (typeof v !== 'number' || !Number.isFinite(v)) return fallback
  return Math.min(max, Math.max(min, v))
}

/** 任意形状脏值 → 合法皮肤（未知预设回落默认，数值越界钳制，类型错回落）。 */
export function normalizeWheelSkin(raw: unknown): WheelSkin {
  if (typeof raw !== 'object' || raw === null) return { ...DEFAULT_WHEEL_SKIN }
  const r = raw as Partial<WheelSkin>
  return {
    preset: (WHEEL_SKIN_PRESETS as readonly unknown[]).includes(r.preset)
      ? r.preset as WheelSkinPreset
      : DEFAULT_WHEEL_SKIN.preset,
    faceAlpha: clampNum(r.faceAlpha, DEFAULT_WHEEL_SKIN.faceAlpha, FACE_ALPHA_MIN, FACE_ALPHA_MAX),
    stroke: clampNum(r.stroke, DEFAULT_WHEEL_SKIN.stroke, STROKE_MIN, STROKE_MAX),
    followModuleColor: r.followModuleColor === true,
  }
}

/** JSON 串（null/坏串/缺字段/脏值均可）→ 合法皮肤；永不抛错。 */
export function parseWheelSkin(raw: string | null | undefined): WheelSkin {
  if (!raw) return { ...DEFAULT_WHEEL_SKIN }
  try {
    return normalizeWheelSkin(JSON.parse(raw))
  } catch {
    return { ...DEFAULT_WHEEL_SKIN }
  }
}

/** 读皮肤账（存储被禁用/隐私模式 → 默认皮肤，不抛错）。 */
export function readWheelSkin(): WheelSkin {
  try {
    return parseWheelSkin(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY))
  } catch {
    return { ...DEFAULT_WHEEL_SKIN }
  }
}

/** 合入写盘并返回归一后的完整皮肤（存储写失败静默降级为仅内存生效）。 */
export function saveWheelSkin(patch: Partial<WheelSkin>, current: WheelSkin = readWheelSkin()): WheelSkin {
  const next = normalizeWheelSkin({ ...current, ...patch })
  try {
    localStorage.setItem(WHEEL_SKIN_STORAGE_KEY, JSON.stringify(next))
  } catch {
    /* 存储满/隐私模式：皮肤只活本会话，如实降级不炸设置页 */
  }
  return next
}

/** 描边强度 0–1 → 色混占比（10%–60%：0 档也不清零，留一丝缝内同色轮廓的收口） */
export function wheelStrokeMixPercent(stroke: number): number {
  return Math.round(10 + clampNum(stroke, DEFAULT_WHEEL_SKIN.stroke, STROKE_MIN, STROKE_MAX) * 50)
}

/** 皮肤 → 下发给盘面样式层的 CSS 变量（预设走类名，此处只承担连续量）。 */
export function wheelSkinVars(skin: WheelSkin): Record<string, string> {
  return {
    '--wf-face-a': String(skin.faceAlpha),
    '--wf-edge': `${wheelStrokeMixPercent(skin.stroke)}%`,
  }
}

/**
 * 模块色（图标主色）→ 描边 CSS 色：在色算钳制之上再随盘面透明度压一档亮度，
 * 输出色仅供 --wf-edge 的 color-mix 使用，永不直接做大色块。
 */
export function wheelTintCss(color: Hsl | null, faceAlpha: number): string {
  if (!color) return ''
  // 盘面越透，纱底越浅，描边色需略深才压得住——亮度按透明度反向微调（最多 -4%）。
  const a = clampNum(faceAlpha, FACE_ALPHA_MAX, FACE_ALPHA_MIN, FACE_ALPHA_MAX)
  const lift = (FACE_ALPHA_MAX - a) * 0.04
  return hslToCss({ ...color, l: Math.max(0.28, color.l - lift) })
}
