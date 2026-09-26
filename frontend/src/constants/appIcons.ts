// 真图标注册表（N27 批 A）：托管工具自己的 exe 主图标，作为 AppIcon 的
// **第二来源**（第一来源是 icons.ts 的 54 枚自绘矢量 `i:`）。
//
// 取材纪律（侦查收口 PLAN_N27_ICON.md）：
//   - 静态入库优于运行时提取——一次性 `scripts/extract_app_icons.ps1` 从已装
//     托管 exe 的 PE 资源取**最大真实画幅**、等比降采样到 64px 封顶（源不足则
//     保留真实尺寸、绝不放大凑数）落 `assets/apps/<moduleId>.png`（逐枚出货边长
//     见 internal/app/appicons/sources.go 台账）；运行期零依赖（版本目录可能未装，
//     也不给前端开取图通道）。
//   - 取不到的（无 GUI 头件 ddnsgo/frpc、许可受限 Sysinternals）一律回落
//     `generic.png` 通用徽标——宁缺毋滥，绝不伪造"像"的图标。
//   - 每个入库 logo 的许可状态逐个过 docs/THIRD_PARTY_NOTICES.md，未过审不入库。
//
// 键即模块 ID（与后端注册表 nav moduleID 同名）；`appIconUrl` 永不返回空串，
// 缺图回落 generic，保证 AppIcon `app:` 分支总能出图。
import { ICON_NAMES, type RenderableIcon } from './icons'

const files = import.meta.glob('../assets/apps/*.png', { eager: true, import: 'default' }) as Record<string, string>

const key = (id: string) => `../assets/apps/${id}.png`

/** 通用徽标 URL（取不到真图标时的回落位）。 */
export const APP_ICON_GENERIC_URL: string = files[key('generic')] ?? ''

/** 已入库真图标的模块 ID 清单（不含 generic 本身）。 */
export const APP_ICON_IDS: string[] = Object.keys(files)
  .map((k) => k.replace('../assets/apps/', '').replace('.png', ''))
  .filter((id) => id !== 'generic')

/** 按模块 ID 取图标 URL；缺图回落通用徽标（永不空串）。 */
export function appIconUrl(id: string): string {
  return files[key(id)] || APP_ICON_GENERIC_URL
}

/** 归一后的图标解析结果：交 AppIcon 渲染（svg/app）或文本回退。 */
export type ResolvedIcon =
  | { kind: 'svg'; name: string } // icons.ts 注册表名（无 i: 前缀）
  | { kind: 'app'; name: string } // app:<id>（原样喂 AppIcon，缺图自动 generic）
  | { kind: 'text'; text: string } // 裸 emoji/字符回退
  | { kind: 'blank' } // i: 未登记 / 空：占位不渲染

/**
 * 解析导航/卡片图标的三形态（各消费面共用，杜绝 `startsWith('i:')` 逻辑散落漂移）：
 * `app:<id>` 恒可渲染（缺图回落由 appIconUrl 保证）；`i:<name>` 须已在 ICON_PATHS
 * 登记否则 blank；其余按裸文本回退（阶段3 退役中）。入参允许已剥前缀的裸 svg 名
 * （轮盘扇区即此形态），故对无 `i:`/`app:` 前缀的已知注册名也认 svg。
 */
export function resolveIcon(icon: string | undefined | null): ResolvedIcon {
  if (!icon) return { kind: 'blank' }
  if (icon.startsWith('app:')) return { kind: 'app', name: icon }
  if (icon.startsWith('i:')) {
    const name = icon.slice(2)
    return (ICON_NAMES as readonly string[]).includes(name) ? { kind: 'svg', name } : { kind: 'blank' }
  }
  if ((ICON_NAMES as readonly string[]).includes(icon)) return { kind: 'svg', name: icon }
  return { kind: 'text', text: icon }
}

/**
 * 消费面主入口：图标串 → 可交 `<AppIcon :name>` 的名（svg 剥前缀、app 原样），
 * 不可渲染（裸文本/未登记 i:）回 undefined 由调用面走各自文本回退轨。
 */
export function appIconName(icon: string | undefined | null): RenderableIcon | undefined {
  const r = resolveIcon(icon)
  if (r.kind === 'svg') return r.name as RenderableIcon
  if (r.kind === 'app') return r.name as RenderableIcon
  return undefined
}
