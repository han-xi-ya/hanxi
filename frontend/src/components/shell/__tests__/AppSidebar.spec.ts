// 特征测试：AppSidebar（Phase 6 外壳组件化，自 App.vue 抽出）。
// 锁 props/emits 契约与展示行为：导航分组渲染、active 高亮随 prop、
// `i:` 图标双轨分支、未读徽标显隐、主题钮三态文案与点击 emit、backendReady 状态条文案。
// 另含 DOM 机检：渲染的 aside.sidebar outerHTML 与拆分前 App.vue 采集的基线
// （AppSidebar.dom.baseline.html，剥离 data-v-* scoped 哈希后）逐字节一致，
// 证明类名/层级/文案零漂移。AppSidebar 为纯展示组件，无需 vi.mock。
import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import AppSidebar from '../AppSidebar.vue'
import type { NavEntry } from '../../../../bindings/hanxi/internal/extapi/models'

const nav = (route: string, title: string, icon: string): NavEntry => ({
  id: route.slice(1),
  route,
  title,
  icon,
  section: 'ext',
  order: 0,
})

function factory(props: Partial<{
  navs: NavEntry[]
  activeRoute: string
  unreadCount: number
  themeMode: 'light' | 'dark' | 'system'
  backendReady: boolean
}> = {}) {
  return mount(AppSidebar, {
    props: {
      navs: [],
      activeRoute: '/',
      unreadCount: 0,
      themeMode: 'light',
      backendReady: true,
      ...props,
    },
  })
}

describe('AppSidebar', () => {
  it('DOM 机检：侧栏子树 outerHTML 与拆分前基线逐字节一致（类名/层级/文案零漂移）', () => {
    const baseline = readFileSync(
      resolve(process.cwd(), 'src/components/shell/__tests__/AppSidebar.dom.baseline.html'),
      'utf8',
    )
    const w = factory({ navs: [nav('/ext/memo', '随手记', '📝')] })
    const html = w.element.outerHTML.replace(/ ?data-v-[0-9a-f]+=""/g, '')
    expect(html).toBe(baseline)
  })

  it('渲染品牌区与三段导航：系统区、工具分组（有 navs 时）、底部区', () => {
    const w = factory({ navs: [nav('/ext/memo', '随手记', '📝'), nav('/ext/wifi', 'WiFi 密码', 'i:wifi')] })
    expect(w.find('.brand-mark').text()).toBe('HX')
    expect(w.find('.brand-name').text()).toBe('Hanxi')
    expect(w.find('.brand-desc').text()).toBe('开源工具工作台')
    // main-nav：首页 + 工具区两项，分组标签与分隔条出现
    const mainItems = w.findAll('.main-nav .nav-item')
    expect(mainItems.map((b) => b.find('.nav-text').text())).toEqual(['首页', '随手记', 'WiFi 密码'])
    expect(w.find('.nav-divider').exists()).toBe(true)
    expect(w.find('.nav-group-label').text()).toBe('工具')
    // bottom-nav：通知中心 + 日志/设置/关于 + 主题钮
    const bottomTexts = w.findAll('.bottom-nav .nav-item').map((b) => b.find('.nav-text').text())
    expect(bottomTexts).toEqual(['通知中心', '日志', '设置', '关于', '浅色主题'])
  })

  it('navs 为空时不渲染分隔条与"工具"分组标签', () => {
    const w = factory({ navs: [] })
    expect(w.find('.nav-divider').exists()).toBe(false)
    expect(w.find('.nav-group-label').exists()).toBe(false)
    expect(w.findAll('.main-nav .nav-item')).toHaveLength(1)
  })

  it('active 高亮跟随 activeRoute prop，切换 prop 即迁移', async () => {
    const w = factory({ navs: [nav('/ext/memo', '随手记', '📝')] })
    expect(w.find('.nav-item.active').text()).toContain('首页')
    await w.setProps({ activeRoute: '/settings' })
    const active = w.findAll('.nav-item.active')
    expect(active).toHaveLength(1)
    expect(active[0].text()).toContain('设置')
    await w.setProps({ activeRoute: '/ext/memo' })
    expect(w.find('.nav-item.active').text()).toContain('随手记')
  })

  it('点击导航项 emit navigate(路由)：系统项、扩展项、底部项同轨', async () => {
    const w = factory({ navs: [nav('/ext/memo', '随手记', '📝')] })
    const items = w.findAll('.nav-item')
    const byText = (t: string) => items.find((b) => b.find('.nav-text').text() === t)!
    await byText('首页').trigger('click')
    await byText('随手记').trigger('click')
    await byText('日志').trigger('click')
    await byText('关于').trigger('click')
    expect(w.emitted('navigate')).toEqual([['/'], ['/ext/memo'], ['/logs'], ['/about']])
  })

  it('图标双轨：`i:` 前缀渲染 AppIcon 内联 SVG，裸 emoji 走文本回退', () => {
    const w = factory({ navs: [nav('/ext/memo', '随手记', '📝'), nav('/ext/wifi', 'WiFi 密码', 'i:wifi')] })
    const homeIcon = w.find('.main-nav .nav-item .nav-icon')
    expect(homeIcon.find('svg.app-icon').exists()).toBe(true)
    const memoIcon = w.findAll('.main-nav .nav-item')[1].find('.nav-icon')
    expect(memoIcon.find('svg').exists()).toBe(false)
    expect(memoIcon.text()).toBe('📝')
    const wifiIcon = w.findAll('.main-nav .nav-item')[2].find('.nav-icon')
    expect(wifiIcon.find('svg.app-icon').exists()).toBe(true)
  })

  it('通知入口：unreadCount>0 显示徽标数字，0 隐藏；点击 emit toggle-drawer', async () => {
    const w = factory({ unreadCount: 3 })
    const badge = w.find('.nav-badge')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('3')
    const notifBtn = w.find('.notif-nav-btn')
    expect(notifBtn.find('.nav-text').text()).toBe('通知中心')
    expect(notifBtn.attributes('title')).toBe('通知中心')
    await notifBtn.trigger('click')
    expect(w.emitted('toggle-drawer')).toHaveLength(1)
    await w.setProps({ unreadCount: 0 })
    expect(w.find('.nav-badge').exists()).toBe(false)
  })

  it('主题钮三态文案与 title，点击 emit cycle-theme', async () => {
    const w = factory({ themeMode: 'system' })
    const btn = w.find('.theme-toggle')
    expect(btn.find('.nav-text').text()).toBe('跟随系统')
    expect(btn.attributes('title')).toBe('当前主题：跟随系统（点击循环切换）')
    expect(btn.find('.nav-icon svg').exists()).toBe(true)
    await w.setProps({ themeMode: 'light' })
    expect(btn.find('.nav-text').text()).toBe('浅色主题')
    expect(btn.attributes('title')).toBe('当前主题：浅色主题（点击循环切换）')
    await w.setProps({ themeMode: 'dark' })
    expect(btn.find('.nav-text').text()).toBe('深色主题')
    await btn.trigger('click')
    expect(w.emitted('cycle-theme')).toHaveLength(1)
  })

  it('状态条：backendReady=false 为加载态文案，true 为就绪态且圆点加 online', async () => {
    const w = factory({ backendReady: false })
    expect(w.find('.status-dot').classes()).not.toContain('online')
    expect(w.find('.status-text').text()).toBe('正在加载工作台…')
    await w.setProps({ backendReady: true })
    expect(w.find('.status-dot').classes()).toContain('online')
    expect(w.find('.status-text').text()).toBe('工作台已就绪')
  })
})
