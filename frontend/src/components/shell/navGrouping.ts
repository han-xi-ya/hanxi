// 双栏外壳的分组归属共享逻辑（AppNavRail / AppSidebar 消费）。
//
// 契约来源（并行 agent 落地中，本文件只消费不实现）：
//   · bindings NavEntry 将新增 `group?: string`，取值 NavGroup 六键；
//     落地前以本地交叉类型 NavEntryWithGroup 声明消费点，签名保持与 NavEntry 兼容。
//   · constants/navigation.ts 将导出 GROUP_META / NavGroup / MODULE_GROUP / groupOfModule，
//     本文件按契约直接 import（落地前 typecheck 报缺导出属预期）。
//
// 归属解析规则（优先级从高到低）：
//   1. nav.group（后端注册表显式声明）——仅当其为 GROUP_META 已知键时采信；
//   2. 回落 groupOfModule(模块 ID)——模块 ID 解析自 route 末段
//      （'/ext/memo' → 'memo'、'/frpc' → 'frpc'；route 缺失时退 id 去 '-manager'
//      后缀再取 '-' 首段，如 'portscan-manager' → 'portscan'）；
//   3. 仍未命中 → 固定 'other' 兜底组（只进二级面板不进 rail，图标 i:box）。
import type { NavEntry } from '../../../bindings/hanxi/internal/extapi/models'
import { GROUP_META, groupOfModule, type NavGroup } from '../../constants/navigation'

/** bindings 重生成前的本地过渡类型：NavEntry + 契约字段 group（可选，向后兼容）。 */
export type NavEntryWithGroup = NavEntry & { group?: string }

/** 未知分组的兜底组键：不进 rail，仅出现在二级面板。 */
export const OTHER_GROUP = 'other'

/** rail/面板可呈现的分组键（含兜底组）。 */
export type ShellGroup = NavGroup | typeof OTHER_GROUP

/** route/id 双路解析模块 ID：route 末段优先，id 前缀兜底。 */
export function moduleIdOfNav(nav: NavEntryWithGroup): string {
  const segs = (nav.route ?? '').split('/').filter(Boolean)
  if (segs.length > 0) return segs[segs.length - 1]
  return (nav.id ?? '').split('-')[0]
}

/** 单个导航项的分组归属（规则见文件头注释）。 */
export function groupOfNav(nav: NavEntryWithGroup): ShellGroup {
  if (nav.group && nav.group in GROUP_META) return nav.group as NavGroup
  return groupOfModule(moduleIdOfNav(nav)) ?? OTHER_GROUP
}

/** 按分组键归类导航项；GROUP_META 之外的键只有 OTHER_GROUP 一种。 */
export function groupNavs(navs: NavEntryWithGroup[]): Map<ShellGroup, NavEntryWithGroup[]> {
  const map = new Map<ShellGroup, NavEntryWithGroup[]>()
  for (const n of navs) {
    const g = groupOfNav(n)
    const list = map.get(g)
    if (list) list.push(n)
    else map.set(g, [n])
  }
  return map
}

/** rail 分类按钮顺序：GROUP_META 六组按 order 升序（other 不进 rail）。 */
export function orderedRailGroups(): NavGroup[] {
  return (Object.keys(GROUP_META) as NavGroup[]).sort((a, b) => GROUP_META[a].order - GROUP_META[b].order)
}

/** GROUP_META.icon 剥离 `i:` 前缀（rail/面板头统一走 AppIcon）。 */
export function groupIconName(icon: string): string {
  return icon.startsWith('i:') ? icon.slice(2) : icon
}

/** 模块运行状态嗅探（本次不接后端 API，runningIds 由外层注入，缺省视为无运行）。 */
export function isNavRunning(nav: NavEntryWithGroup, runningIds: string[]): boolean {
  const id = moduleIdOfNav(nav)
  return runningIds.includes(id) || runningIds.includes(nav.id)
}

/** 组内运行模块数（rail 绿点与面板页脚共用）。 */
export function runningCountOf(navs: NavEntryWithGroup[], runningIds: string[]): number {
  return navs.filter((n) => isNavRunning(n, runningIds)).length
}

// ── 最近使用（localStorage 键 hanxi.recentRoutes，最多 6 条，AppSidebar 本地维护）──

export const RECENT_ROUTES_KEY = 'hanxi.recentRoutes'
export const RECENT_ROUTES_MAX = 6

/** 读取最近使用路由；解析失败一律按空处理（脏数据不炸外壳）。 */
export function loadRecentRoutes(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_ROUTES_KEY)
    if (!raw) return []
    const arr: unknown = JSON.parse(raw)
    if (!Array.isArray(arr)) return []
    return arr.filter((x): x is string => typeof x === 'string').slice(0, RECENT_ROUTES_MAX)
  } catch {
    return []
  }
}

/** 压入一条最近使用：去重置顶、截断 6 条并回写（写失败静默，同列宽记忆范式）。 */
export function pushRecentRoute(route: string): string[] {
  const next = [route, ...loadRecentRoutes().filter((r) => r !== route)].slice(0, RECENT_ROUTES_MAX)
  try {
    localStorage.setItem(RECENT_ROUTES_KEY, JSON.stringify(next))
  } catch {
    /* 忽略写失败 */
  }
  return next
}
