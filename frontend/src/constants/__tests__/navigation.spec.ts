// navigation.ts 是路由单一来源：防"新增视图忘登记 / 双表漂移"的回归锁。
// 断言与 App.vue 迁移前的两份手写表内容一一对应（特征基线）。
import { describe, expect, it } from 'vitest'
import {
  ROUTES, moduleIdOf, routeComponent, placeholderComponent, fallbackComponent,
  GROUP_META, MODULE_GROUP, groupOfModule, MODULE_PRESENTATION, FALLBACK_MODULE_ICON,
  SETTINGS_SECTIONS, settingsSectionOf,
} from '../navigation'
import { ICON_NAMES } from '../icons'

describe('constants/navigation', () => {
  it('登记了全部 49 条路由（含设置页 7 个分区子路由）', () => {
    expect(Object.keys(ROUTES)).toHaveLength(49)
    for (const route of ['/', '/frpc', '/logs', '/settings', '/about', '/ext/markeron', '/ext/envcheck', '/ext/wsl', '/ext/rufus', '/ext/bili23', '/ext/vscode', '/ext/translucenttb', '/ext/paseo', '/ext/quickmenu', '/ext/ocr']) {
      expect(ROUTES[route]).toBeDefined()
    }
    for (const s of SETTINGS_SECTIONS) {
      expect(ROUTES[s.route]).toBeDefined()
    }
  })

  it('模块门禁集合与原 ROUTE_MODULE_MAP 一致（37 个 ext + frpc + paseo，核心页无 moduleId）', () => {
    const withModule = Object.entries(ROUTES)
      .filter(([, def]) => def.moduleId !== undefined)
      .map(([route, def]) => `${route}=${def.moduleId}`)
      .sort()
    expect(withModule).toHaveLength(38)
    expect(withModule).toContain('/frpc=frpc')
    expect(withModule).toContain('/ext/ocr=ocr')
    expect(withModule).toContain('/ext/envcheck=envcheck')
    expect(withModule).toContain('/ext/wsl=wsl')
    expect(withModule).toContain('/ext/subnetdesk=subnetdesk')
    expect(withModule).toContain('/ext/rustdesk=rustdesk')
    expect(withModule).toContain('/ext/bili23=bili23')
    expect(withModule).toContain('/ext/vscode=vscode')
    expect(withModule).toContain('/ext/translucenttb=translucenttb')
    expect(withModule).toContain('/ext/paseo=paseo')
    expect(withModule).toContain('/ext/quickmenu=quickmenu')
    expect(moduleIdOf('/')).toBeUndefined()
    expect(moduleIdOf('/settings')).toBeUndefined()
  })

  it('route 与 moduleId 自洽：/ext/<id> 路由的 moduleId 即 <id>', () => {
    for (const [route, def] of Object.entries(ROUTES)) {
      if (route.startsWith('/ext/')) {
        expect(def.moduleId).toBe(route.slice('/ext/'.length))
      }
    }
  })

  it('GROUP_META 六组齐全，order 1..6 连续，图标名均已登记', () => {
    expect(Object.keys(GROUP_META)).toHaveLength(6)
    expect(Object.values(GROUP_META).map((g) => g.order).sort((a, b) => a - b)).toEqual([1, 2, 3, 4, 5, 6])
    for (const g of Object.values(GROUP_META)) {
      expect(ICON_NAMES).toContain(g.icon)
      expect(g.title).toBeTruthy()
      expect(g.desc).toBeTruthy()
    }
  })

  it('MODULE_GROUP 覆盖 ROUTES 全部 moduleId，且值均属六组之一', () => {
    const groups = new Set(Object.keys(GROUP_META))
    for (const def of Object.values(ROUTES)) {
      if (def.moduleId) expect(MODULE_GROUP[def.moduleId]).toBeDefined()
    }
    for (const g of Object.values(MODULE_GROUP)) expect(groups.has(g)).toBe(true)
    expect(Object.keys(MODULE_GROUP)).toHaveLength(38)
  })

  it('groupOfModule：已知返回分组，未知返回 undefined', () => {
    expect(groupOfModule('frpc')).toBe('network')
    expect(groupOfModule('paseo')).toBe('developer')
    expect(groupOfModule('nope')).toBeUndefined()
  })

  it('MODULE_PRESENTATION 图标全部为已登记的 i: 名，回退为 i:box', () => {
    expect(FALLBACK_MODULE_ICON).toBe('i:box')
    for (const p of Object.values(MODULE_PRESENTATION)) {
      expect(p.icon.startsWith('i:')).toBe(true)
      expect(ICON_NAMES).toContain(p.icon.slice(2))
    }
  })

  it('组件解析：已知路由稳定返回同一异步组件；未知返回 undefined；占位/回退可用', () => {
    const first = routeComponent('/frpc')
    expect(first).toBeTruthy()
    expect(routeComponent('/frpc')).toBe(first) // 引用稳定：KeepAlive 缓存键依赖此语义
    expect(routeComponent('/nope')).toBeUndefined()
    expect(placeholderComponent()).toBeTruthy()
    expect(fallbackComponent()).toBe(routeComponent('/'))
    // 设置主入口与常规分区共享同一组件（'/settings' 兼容串直达内容）
    expect(routeComponent('/settings')).toBe(routeComponent('/settings/general'))
  })
})

describe('设置分区注册表', () => {
  it('SETTINGS_SECTIONS 图标均已登记，route 与 /settings/<id> 对齐且全部进 ROUTES', () => {
    expect(SETTINGS_SECTIONS.length).toBeGreaterThanOrEqual(6)
    for (const s of SETTINGS_SECTIONS) {
      expect(ICON_NAMES).toContain(s.icon)
      expect(s.route).toBe(`/settings/${s.id}`)
      expect(ROUTES[s.route]).toBeDefined()
      expect(moduleIdOf(s.route)).toBeUndefined() // 分区为前端核心页，不进模块门禁
    }
  })

  it('settingsSectionOf：主入口与未知子段回落 general，识别分区，非设置路由返回 null', () => {
    expect(settingsSectionOf('/settings')).toBe('general')
    expect(settingsSectionOf('/settings/theme')).toBe('theme')
    expect(settingsSectionOf('/settings/nope')).toBe('general')
    expect(settingsSectionOf('/')).toBeNull()
    expect(settingsSectionOf('/ext/memo')).toBeNull()
    expect(settingsSectionOf('/settingsx')).toBeNull() // 前缀相似不误伤
  })
})