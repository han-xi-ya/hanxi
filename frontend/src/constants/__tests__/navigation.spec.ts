// navigation.ts 是路由单一来源：防"新增视图忘登记 / 双表漂移"的回归锁。
// 断言与 App.vue 迁移前的两份手写表内容一一对应（特征基线）。
import { describe, expect, it } from 'vitest'
import {
  ROUTES, moduleIdOf, routeComponent, placeholderComponent, fallbackComponent,
  GROUP_META, MODULE_GROUP, groupOfModule, MODULE_PRESENTATION, FALLBACK_MODULE_ICON,
  SETTINGS_SECTIONS, settingsSectionOf, CORE_ROUTES, isCoreRoute,
} from '../navigation'
import { ICON_NAMES } from '../icons'
import compositionContract from '../../../../scripts/fixture/composition_contract.json'

const contractModules = compositionContract.modules
  .map(({ id, route, group }) => `${id}|${route}|${group}`)
  .sort()

const frontendModules = () => Object.entries(ROUTES)
  .filter((entry): entry is [string, (typeof ROUTES)[string] & { moduleId: string }] => entry[1].moduleId !== undefined)
  .map(([route, def]) => `${def.moduleId}|${route}|${MODULE_GROUP[def.moduleId]}`)
  .sort()

describe('constants/navigation', () => {
  it('登记了全部 56 条路由（含设置页 8 个分区子路由与模块中心）', () => {
    expect(Object.keys(ROUTES)).toHaveLength(56)
    for (const route of ['/', '/modules', '/frpc', '/logs', '/settings', '/about', '/ext/markeron', '/ext/envcheck', '/ext/wsl', '/ext/rufus', '/ext/bili23', '/ext/vscode', '/ext/translucenttb', '/ext/paseo', '/ext/quickmenu', '/ext/ocr', '/ext/webapp', '/ext/msgboard']) {
      expect(ROUTES[route]).toBeDefined()
    }
    for (const s of SETTINGS_SECTIONS) {
      expect(ROUTES[s.route]).toBeDefined()
    }
  })

  it('ROUTES/MODULE_GROUP 与后端 composition contract 的具体 ID/route/group 集合一致', () => {
    expect(frontendModules()).toEqual(contractModules)
    expect(frontendModules()).toHaveLength(compositionContract.counts.modules)
  })

  it('模块门禁集合覆盖后端 contract，核心页无 moduleId', () => {
    const withModule = Object.entries(ROUTES)
      .filter(([, def]) => def.moduleId !== undefined)
      .map(([route, def]) => `${route}=${def.moduleId}`)
      .sort()
    expect(withModule).toHaveLength(compositionContract.counts.modules)
    expect(withModule).toContain('/frpc=frpc')
    expect(withModule).toContain('/ext/webapp=webapp')
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
    expect(moduleIdOf('/modules')).toBeUndefined() // 模块中心为核心入口，不进模块门禁
    expect(routeComponent('/modules')).toBeTruthy()
  })

  it('CORE_ROUTES 豁免清单：核心页精确命中；模块路由与设置子分区不命中', () => {
    for (const r of ['/', '/modules', '/settings', '/logs', '/about']) {
      expect(CORE_ROUTES).toContain(r)
      expect(isCoreRoute(r)).toBe(true)
    }
    expect(isCoreRoute('/ext/memo')).toBe(false)
    expect(isCoreRoute('/settings/theme')).toBe(false) // 与迁移前字面量 || 串同语义（精确匹配零漂移）
    expect(isCoreRoute('/modulesx')).toBe(false) // 前缀相似不误伤
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
    expect(Object.keys(MODULE_GROUP)).toHaveLength(compositionContract.counts.modules)
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