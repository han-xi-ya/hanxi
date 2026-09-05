// 内联 SVG 图标注册表（设计纪律：emoji 不作主图标，docs/FRONTEND.md 铁律 6 / §8 AppIcon）。
// 规范：24×24 viewBox，纯 <path> stroke 几何，currentColor 描边、无填充；
// 新增图标必须走本表集中登记，禁止在视图里散写 <svg>（Snipaste 字标等品牌图形例外）。
//
// 分批迁移计划（§8 清单落地）：
//   阶段1 ✅ 壳与状态：home/file-text/gear/info/bell/sun/moon/monitor + 通知严重度改 CSS 色点；
//   阶段2 ⏳ navigation.ts 模块图标 26 枚（frpc/fileshare/memo/…，保持模块识别度）；
//   阶段3 ⏳ 家族视图 MAIN_TABS 前缀 emoji 与控制按钮 emoji（📂🖥⚡ 等 ≈40 处）。

/** 图标名 → path d 列表（stroke 渲染，圆/环用双弧 path 表达）。 */
export const ICON_PATHS = {
  home: ['M3 11l9-7 9 7', 'M5 10v9a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-9', 'M9 21v-7h6v7'],
  'file-text': ['M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5z', 'M14 3v5h5', 'M9 13h6', 'M9 17h4'],
  gear: ['M4 7h10', 'M18 7h2', 'M4 17h2', 'M10 17h10', 'M16 5a2 2 0 1 0 0 4 2 2 0 0 0 0-4', 'M8 15a2 2 0 1 0 0 4 2 2 0 0 0 0-4'],
  info: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18', 'M12 11v5', 'M12 8v.01'],
  bell: ['M6 9a6 6 0 0 1 12 0c0 5 2 6 2 6H4s2-1 2-6', 'M10.3 20a2 2 0 0 0 3.4 0'],
  inbox: ['M4 13l2-8h12l2 8v6H4z', 'M4 13h5l1 2h4l1-2h5'],
  sun: ['M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8', 'M12 2v2', 'M12 20v2', 'M2 12h2', 'M20 12h2', 'M4.9 4.9l1.4 1.4', 'M17.7 17.7l1.4 1.4', 'M19.1 4.9l-1.4 1.4', 'M6.3 17.7l-1.4 1.4'],
  moon: ['M20 13.5A8.5 8.5 0 1 1 10.5 4a6.5 6.5 0 0 0 9.5 9.5'],
  monitor: ['M3 5h18v11H3z', 'M9 20h6', 'M12 16v4'],
} as const

export type IconName = keyof typeof ICON_PATHS

/** AppIcon 的 name prop 校验用集合（模板中 `i:` 前缀约定剥离后可查）。 */
export const ICON_NAMES = Object.keys(ICON_PATHS) as IconName[]
