// 真图标注册表（N27 批 A）：托管工具自己的 exe 主图标，作为 AppIcon 的
// **第二来源**（第一来源是 icons.ts 的 54 枚自绘矢量 `i:`）。
//
// 取材纪律（侦查收口 PLAN_N27_ICON.md）：
//   - 静态入库优于运行时提取——一次性 `scripts/extract_app_icons.ps1` 从已装
//     托管 exe 提主图标、归一 32px PNG 落 `assets/apps/<moduleId>.png`；
//     运行期零依赖（版本目录可能未装，也不给前端开取图通道）。
//   - 取不到的（无 GUI 头件 ddnsgo/frpc、许可受限 Sysinternals）一律回落
//     `generic.png` 通用徽标——宁缺毋滥，绝不伪造"像"的图标。
//   - 每个入库 logo 的许可状态逐个过 docs/THIRD_PARTY_NOTICES.md，未过审不入库。
//
// 键即模块 ID（与后端注册表 nav moduleID 同名）；`appIconUrl` 永不返回空串，
// 缺图回落 generic，保证 AppIcon `app:` 分支总能出图。
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
