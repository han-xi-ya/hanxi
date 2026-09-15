// AppNavRail 特征测试：一级图标轨道（双栏改造新增）。
// 锁契约：GROUP_META 驱动的六分类按钮（顺序/计数角标/运行绿点）、select-group 上抛、
// 首页与 rail-bottom 核心页 navigate 上抛、activeGroup/activeRoute 高亮与 aria-current、
// 未读徽标显隐、主题三态 title 与 cycle-theme 上抛。
// 依赖真实 constants/navigation.ts 的 GROUP_META（数据层已落地），断言从 GROUP_META 取，不抄字面量。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppNavRail from '../AppNavRail.vue'
import { GROUP_META, type NavGroup } from '../../../constants/navigation'
import { orderedRailGroups, type NavEntryWithGroup } from '../navGrouping'

const nav = (route: string, title: string, icon: string, group?: string): NavEntryWithGroup => ({
  id: `${route.split('/').pop()}-manager`,
  route,
  title,
  icon,
  section: 'ext',
  order: 0,
  group,
})

function factory(props: Partial<{
  activeGroup: NavGroup | 'other' | ''
  navs: NavEntryWithGroup[]
  activeRoute: string
  unreadCount: number
  themeMode: 'light' | 'dark' | 'system'
  runningIds: string[]
}> = {}) {
  return mount(AppNavRail, {
    props: {
      activeGroup: '',
      navs: [],
      activeRoute: '/',
      unreadCount: 0,
      themeMode: 'light',
      ...props,
    },
  })
}

describe('AppNavRail', () => {
  it('渲染首页钮、分隔线与 GROUP_META 六分类（order 升序），顶部无品牌标', () => {
    const w = factory()
    expect(w.find('.rail-logo').exists()).toBe(false)
    expect(w.find('.rail-home').exists()).toBe(true)
    expect(w.find('.rail-sep').exists()).toBe(true)
    const groups = w.findAll('.rail-group')
    expect(groups).toHaveLength(6)
    const expected = orderedRailGroups()
    expect(groups.map((b) => b.attributes('aria-label'))).toEqual(expected.map((k) => GROUP_META[k].title))
    // 六组图标全部走 AppIcon 内联 SVG（无 emoji 主图标）
    for (const g of groups) expect(g.find('svg.app-icon').exists()).toBe(true)
  })

  it('展开/收起：默认收起，点击 .rail-toggle 进入 expanded 并持久化，再点恢复', async () => {
    localStorage.removeItem('hanxi.railExpanded')
    const w = factory()
    expect(w.find('.rail').classes()).not.toContain('expanded')
    expect(w.find('.rail-toggle').attributes('aria-expanded')).toBe('false')

    await w.find('.rail-toggle').trigger('click')
    expect(w.find('.rail').classes()).toContain('expanded')
    expect(w.find('.rail-toggle').attributes('aria-expanded')).toBe('true')
    expect(localStorage.getItem('hanxi.railExpanded')).toBe('1')

    // 重新挂载读取持久化态：保持展开
    const w2 = factory()
    expect(w2.find('.rail').classes()).toContain('expanded')

    await w2.find('.rail-toggle').trigger('click')
    expect(localStorage.getItem('hanxi.railExpanded')).toBe('0')
  })

  it('每个导航按钮均携带 .rail-label 文字（收起态由 CSS 隐藏）', () => {
    const w = factory({ navs: [nav('/ext/memo', '随手记', 'i:sticky-note', 'efficiency')] })
    const labels = w.findAll('.rail-btn .rail-label')
    // 首页 + 6 分类 + 通知 + 日志/设置/关于 + 主题 = 12（展开钮保持仅图标）
    expect(labels).toHaveLength(12)
    expect(labels[0].text()).toBe('首页')
    expect(labels[1].text()).toBe(GROUP_META.network.title)
    expect(labels.at(-1)?.text()).toBe('浅色主题')
  })

  it('分类计数角标：按 nav.group 归组统计；无模块的组不显角标', () => {
    const w = factory({
      navs: [
        nav('/ext/memo', '随手记', 'i:sticky-note', 'efficiency'),
        nav('/ext/papertodo', '纸待办', 'i:clipboard-list', 'efficiency'),
        nav('/frpc', 'FRP 内网穿透', 'i:zap', 'network'),
      ],
    })
    const counts = w.findAll('.rail-group').map((b) => b.find('.rail-count').exists() ? b.find('.rail-count').text() : '0')
    // 顺序 network, system, desktop, efficiency, media, developer
    expect(counts).toEqual(['1', '0', '0', '2', '0', '0'])
  })

  it('无 group 字段的 nav 按 route 末段回落 MODULE_GROUP 归类计入角标', () => {
    const w = factory({ navs: [nav('/ext/memo', '随手记', 'i:sticky-note')] })
    const network = w.findAll('.rail-group')[0]
    const efficiency = w.findAll('.rail-group')[3]
    expect(efficiency.find('.rail-count').text()).toBe('1')
    expect(network.find('.rail-count').exists()).toBe(false)
  })

  it('组内有运行模块时左上绿点；runningIds 缺省无绿点', async () => {
    const w = factory({
      navs: [nav('/ext/memo', '随手记', 'i:sticky-note', 'efficiency')],
      runningIds: ['memo'],
    })
    const groups = w.findAll('.rail-group')
    expect(groups[3].find('.rail-dot').exists()).toBe(true)
    expect(groups[0].find('.rail-dot').exists()).toBe(false)
    await w.setProps({ runningIds: [] })
    expect(w.findAll('.rail-group')[3].find('.rail-dot').exists()).toBe(false)
  })

  it('activeGroup 高亮对应分类钮：surface-selected active 类 + aria-current，点击 emit select-group', async () => {
    const w = factory({ activeGroup: 'network' })
    const groups = w.findAll('.rail-group')
    expect(groups[0].classes()).toContain('active')
    expect(groups[0].attributes('aria-current')).toBe('page')
    expect(groups[1].classes()).not.toContain('active')
    await groups[2].trigger('click')
    expect(w.emitted('select-group')).toEqual([['desktop']])
  })

  it('activeGroup 为 other 或空时不点亮任何分类钮；首页钮按 activeRoute 高亮', () => {
    const w = factory({ activeGroup: 'other' })
    expect(w.findAll('.rail-group').filter((b) => b.classes().includes('active'))).toHaveLength(0)
    expect(w.find('.rail-home').classes()).toContain('active')
    expect(w.find('.rail-home').attributes('aria-current')).toBe('page')
  })

  it('rail-bottom：通知（徽标+toggle-drawer）、日志/设置/关于（navigate+高亮）、主题循环', async () => {
    const w = factory({ unreadCount: 5, activeRoute: '/settings' })
    const notif = w.find('.notif-nav-btn')
    expect(notif.find('.nav-badge').text()).toBe('5')
    await notif.trigger('click')
    expect(w.emitted('toggle-drawer')).toHaveLength(1)
    const cores = w.findAll('.rail-core')
    expect(cores.map((b) => b.attributes('title'))).toEqual(['日志', '设置', '关于'])
    expect(cores[1].classes()).toContain('active')
    await cores[0].trigger('click')
    await cores[2].trigger('click')
    expect(w.emitted('navigate')).toEqual([['/logs'], ['/about']])
  })

  it('unreadCount 为 0 隐藏徽标；>99 收敛为 99+', async () => {
    const w = factory()
    expect(w.find('.nav-badge').exists()).toBe(false)
    await w.setProps({ unreadCount: 120 })
    expect(w.find('.nav-badge').text()).toBe('99+')
  })

  it('主题钮三态 title/图标随 themeMode，点击 emit cycle-theme', async () => {
    const w = factory({ themeMode: 'light' })
    const btn = w.find('.theme-toggle')
    expect(btn.attributes('title')).toBe('当前主题：浅色主题（点击循环切换）')
    expect(btn.find('svg.app-icon').exists()).toBe(true)
    await w.setProps({ themeMode: 'dark' })
    expect(btn.attributes('title')).toBe('当前主题：深色主题（点击循环切换）')
    await w.setProps({ themeMode: 'system' })
    expect(btn.attributes('title')).toBe('当前主题：跟随系统（点击循环切换）')
    await btn.trigger('click')
    expect(w.emitted('cycle-theme')).toHaveLength(1)
  })

  it('首页钮点击 emit navigate("/")', async () => {
    const w = factory({ activeRoute: '/settings' })
    await w.find('.rail-home').trigger('click')
    expect(w.emitted('navigate')).toEqual([['/']])
  })
})
