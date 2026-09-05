// navigation.ts 是路由单一来源：防"新增视图忘登记 / 双表漂移"的回归锁。
// 断言与 App.vue 迁移前的两份手写表内容一一对应（特征基线）。
import { describe, expect, it } from 'vitest'
import { ROUTES, moduleIdOf, routeComponent, placeholderComponent, fallbackComponent } from '../navigation'

describe('constants/navigation', () => {
  it('登记了全部 34 条路由', () => {
    expect(Object.keys(ROUTES)).toHaveLength(34)
    for (const route of ['/', '/frpc', '/logs', '/settings', '/about', '/ext/markeron', '/ext/envcheck', '/ext/rufus']) {
      expect(ROUTES[route]).toBeDefined()
    }
  })

  it('模块门禁集合与原 ROUTE_MODULE_MAP 一致（30 个 ext + frpc，核心页无 moduleId）', () => {
    const withModule = Object.entries(ROUTES)
      .filter(([, def]) => def.moduleId !== undefined)
      .map(([route, def]) => `${route}=${def.moduleId}`)
      .sort()
    expect(withModule).toHaveLength(30)
    expect(withModule).toContain('/frpc=frpc')
    expect(withModule).toContain('/ext/envcheck=envcheck')
    expect(withModule).toContain('/ext/subnetdesk=subnetdesk')
    expect(withModule).toContain('/ext/rustdesk=rustdesk')
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

  it('组件解析：已知路由稳定返回同一异步组件；未知返回 undefined；占位/回退可用', () => {
    const first = routeComponent('/frpc')
    expect(first).toBeTruthy()
    expect(routeComponent('/frpc')).toBe(first) // 引用稳定：KeepAlive 缓存键依赖此语义
    expect(routeComponent('/nope')).toBeUndefined()
    expect(placeholderComponent()).toBeTruthy()
    expect(fallbackComponent()).toBe(routeComponent('/'))
  })
})