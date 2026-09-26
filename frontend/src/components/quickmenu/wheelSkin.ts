// 轮盘皮肤账（机主拍板 2026-09-26"轮盘皮肤做"；后端化收权批）：预设 × 盘面
// 透明度 × 描边强度 × 跟随模块色开关的模型、缺省兼容与双源持久化。
//
// 双源口径（真相在 backend，本模块是唯一读写封装）：
//  - **真相 = quickmenu 后端账**（AppSettings.QuickMenuSkin，GetSkin/SetSkin）：
//    皮肤随 hanxidata 数据目录走，NAS 随身同步；
//  - **localStorage（键 hanxi.wheelSkin）= 同源镜像缓存**：① 后端不可达
//    （模块停用/RPC 失败）时的降级兜底；② 弹窗与主窗两 webview 的跨窗即时性
//    （主窗写镜像触发弹窗 storage 事件秒级跟皮，不必等下次唤出重读）。
//  - 不做"本地旧账一次性上收"：面板落地当日即后端为真，localStorage 里的旧稿
//    在首次成功读后自然被后端真相覆写镜像（论证见交付报告）。
//
// 缺省兼容红线（两侧同源）：旧设置无此账 = 出厂素瓷盘；JSON 坏值/越界/未知预设
// 一律归一（前端 normalizeWheelSkin 与 Go normalizeSkin 语义一致），任何输入都
// 不抛错不炸盘。
import * as QuickMenuAPI from '../../../bindings/hanxi/internal/modules/quickmenu'
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

/** 读镜像缓存（后端不可达时的降级皮肤源；存储被禁用/隐私模式 → 默认皮肤，不抛错）。 */
export function readWheelSkin(): WheelSkin {
  try {
    return parseWheelSkin(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY))
  } catch {
    return { ...DEFAULT_WHEEL_SKIN }
  }
}

/** 合入写镜像并返回归一后的完整皮肤（存储写失败静默降级为仅内存生效）。 */
export function saveWheelSkin(patch: Partial<WheelSkin>, current: WheelSkin = readWheelSkin()): WheelSkin {
  const next = normalizeWheelSkin({ ...current, ...patch })
  try {
    localStorage.setItem(WHEEL_SKIN_STORAGE_KEY, JSON.stringify(next))
  } catch {
    /* 存储满/隐私模式：皮肤只活本会话，如实降级不炸设置页 */
  }
  return next
}

// —— 后端真相通道（收权批；wiring 面消费 QuickMenuService.GetSkin/SetSkin，
// 绑定再生成前此处成员不存在，运行期调用即抛、由双源调用侧落镜像兜底）——

/** 后端 quickmenu.Skin 线格式：整数百分比域（0–100）。 */
export interface WheelSkinDTO {
  preset: string
  faceAlpha: number
  stroke: number
  followModuleColor: boolean
}

/** 线格式值（含脏值/非对象）→ 合法皮肤：整数百分比化回 0–1，同一 normalize 归一。 */
export function wheelSkinFromDto(raw: unknown): WheelSkin {
  const r = (typeof raw === 'object' && raw !== null ? raw : {}) as Partial<WheelSkinDTO>
  return normalizeWheelSkin({
    preset: r.preset,
    faceAlpha: typeof r.faceAlpha === 'number' && Number.isFinite(r.faceAlpha) ? r.faceAlpha / 100 : undefined,
    stroke: typeof r.stroke === 'number' && Number.isFinite(r.stroke) ? r.stroke / 100 : undefined,
    followModuleColor: r.followModuleColor,
  })
}

/** 皮肤 → 写线格式：先本地归一再化百分（线上面永远落在后端合法域，回显=输入）。 */
export function wheelSkinToDto(skin: WheelSkin): WheelSkinDTO {
  const s = normalizeWheelSkin(skin)
  return {
    preset: s.preset,
    faceAlpha: Math.round(s.faceAlpha * 100),
    stroke: Math.round(s.stroke * 100),
    followModuleColor: s.followModuleColor,
  }
}

/**
 * 读后端真相。任何失败（模块停用、RPC 错误、坏线格式判不出对象）如实抛——
 * 调用侧契约：catch 后以 readWheelSkin() 镜像降级，绝不用假数据覆写真机皮肤。
 */
export async function fetchWheelSkin(): Promise<WheelSkin> {
  const raw = await QuickMenuAPI.QuickMenuService.GetSkin()
  if (typeof raw !== 'object' || raw === null) throw new Error('轮盘皮肤线格式非对象，按后端不可达处理')
  return wheelSkinFromDto(raw)
}

/** 写后端并取回钳域归正后的回显值（后端 normalize 与本地同语义；失败上抛由调用侧保镜像）。 */
export async function pushWheelSkin(skin: WheelSkin): Promise<WheelSkin> {
  const echo = await QuickMenuAPI.QuickMenuService.SetSkin(wheelSkinToDto(skin))
  if (typeof echo !== 'object' || echo === null) throw new Error('轮盘皮肤回显线格式非对象')
  return wheelSkinFromDto(echo)
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
