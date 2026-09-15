// 特征测试：AppSidebar 双栏容器（一级 rail + 二级分组面板）。
// 锁 props/emits 契约（navs/activeRoute/unreadCount/backendReady + navigate/toggle-drawer，
// 主题切换已随「rail 收敛」迁至设置页）、新增可选 activeGroup/runningIds、
// 二级面板折叠（hanxi.navPanelCollapsed 持久化 + rail 回展钮）、
// 分组渲染规则（后端 group 优先 → route 末段回落 → 'other' 兜底）、
// 首页态「常用 + 最近使用」（localStorage hanxi.recentRoutes）、页脚计数、
// 状态条、窄屏降级 DOM（把手/遮罩/rail-open 类）。
// 旧的 outerHTML 逐字节基线（AppSidebar.dom.baseline.html）随单栏结构退役：
// 双栏改造即为打破该结构，机检目标转为"根节点单元素 aside.sidebar + rail/panel 并排"。
// 依赖真实 constants/navigation.ts 契约导出（数据层已落地）。
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AppSidebar from '../AppSidebar.vue'
import { GROUP_META } from '../../../constants/navigation'
import { RECENT_ROUTES_KEY, type NavEntryWithGroup } from '../navGrouping'

const nav = (route: string, title: string, icon: string, group?: string): NavEntryWithGroup => ({
  id: `${route.split('/').pop()}-manager`,
  route,
  title,
  icon,
  section: 'ext',
  order: 0,
  group,
})

const MEMO = nav('/ext/memo', '随手记', 'i:sticky-note')
const FRPC = nav('/frpc', 'FRP 内网穿透', 'i:zap')
const EVERYTHING = nav('/ext/everything', 'Everything 搜索', 'i:search-code')
const SNIPASTE = nav('/ext/snipaste', 'Snipaste 截图', 'i:scissors')

function factory(props: Partial<{
  navs: NavEntryWithGroup[]
  activeRoute: string
  unreadCount: number
  backendReady: boolean
  activeGroup: 'network' | 'system' | 'desktop' | 'efficiency' | 'media' | 'developer' | 'other' | ''
  runningIds: string[]
}> = {}) {
  return mount(AppSidebar, {
    props: {
      navs: [],
      activeRoute: '/',
      unreadCount: 0,
      backendReady: true,
      ...props,
    },
  })
}

beforeEach(() => {
  localStorage.clear()
})

describe('AppSidebar 双栏骨架', () => {
  it('根节点保持单元素 aside.sidebar，内部并排 rail 与二级面板', () => {
    const w = factory()
    expect(w.element.tagName).toBe('ASIDE')
    expect(w.classes()).toContain('sidebar')
    expect(w.find('.rail').exists()).toBe(true)
    expect(w.find('.nav-panel').exists()).toBe(true)
  })

  it('窄屏降级 DOM 常备：把手钮存在，点击切换 rail-open，遮罩随开合显隐', async () => {
    const w = factory()
    const handle = w.find('.rail-handle')
    expect(handle.exists()).toBe(true)
    expect(handle.attributes('aria-label')).toBe('打开导航')
    expect(w.find('.rail-mask').exists()).toBe(false)
    await handle.trigger('click')
    expect(w.classes()).toContain('rail-open')
    expect(handle.attributes('aria-expanded')).toBe('true')
    expect(w.find('.rail-mask').exists()).toBe(true)
    await w.find('.rail-mask').trigger('click')
    expect(w.classes()).not.toContain('rail-open')
    expect(w.find('.rail-mask').exists()).toBe(false)
  })
})

describe('分组渲染', () => {
  it('activeRoute 命中分组模块 → 面板头显示 GROUP_META 标题与描述', () => {
    const w = factory({ navs: [MEMO], activeRoute: '/ext/memo' })
    expect(w.find('.panel-title').text()).toBe(GROUP_META.efficiency.title)
    expect(w.find('.panel-sub').text()).toBe(GROUP_META.efficiency.desc)
    expect(w.findAll('.mod').map((b) => b.find('.mod-name').text())).toEqual(['随手记'])
  })

  it('后端 group 字段优先于 route 末段回落', () => {
    // route 末段 wsl 属 developer，但后端显式标 network → 归 network 面板
    const w = factory({ navs: [nav('/ext/wsl', 'WSL 运维', 'i:terminal', 'network')], activeRoute: '/ext/wsl' })
    expect(w.find('.panel-title').text()).toBe(GROUP_META.network.title)
  })

  it('未知模块落入「其他」兜底面板：不进 rail（rail 仍 6 组），面板正常呈现', () => {
    const w = factory({ navs: [nav('/ext/newtool', '新工具', 'i:box')], activeRoute: '/ext/newtool' })
    expect(w.findAll('.rail-group')).toHaveLength(6)
    expect(w.find('.panel-title').text()).toBe('其他')
    expect(w.findAll('.mod').map((b) => b.find('.mod-name').text())).toEqual(['新工具'])
  })

  it('rail 点组仅切换面板分类（select-group 不上抛 navigate），路由变化后复位', async () => {
    const w = factory({ navs: [MEMO, FRPC], activeRoute: '/ext/memo' })
    await w.findAll('.rail-group')[0].trigger('click') // network
    expect(w.find('.panel-title').text()).toBe(GROUP_META.network.title)
    expect(w.findAll('.mod').map((b) => b.find('.mod-name').text())).toEqual(['FRP 内网穿透'])
    expect(w.emitted('navigate')).toBeUndefined()
    // 路由一旦变化，临时预览复位（先真切到首页态再切回，触发 activeRoute watch）
    await w.setProps({ activeRoute: '/' })
    expect(w.find('.panel-title').text()).toBe('工作台')
    await w.setProps({ activeRoute: '/ext/memo' })
    expect(w.find('.panel-title').text()).toBe(GROUP_META.efficiency.title)
  })

  it('activeGroup prop 受控优先于本地推导', async () => {
    const w = factory({ navs: [MEMO], activeRoute: '/ext/memo', activeGroup: 'system' })
    expect(w.find('.panel-title').text()).toBe(GROUP_META.system.title)
    expect(w.findAll('.mod')).toHaveLength(0)
    // 空分类呈现引导性空态
    expect(w.find('.panel-empty').text()).toContain('暂无已启用模块')
  })

  it('分类下无模块且后端未就绪 → 加载态文案', () => {
    const w = factory({ navs: [], activeGroup: 'media', backendReady: false })
    expect(w.find('.panel-empty').text()).toContain('正在加载模块清单')
  })
})

describe('面板模块行', () => {
  it('active 高亮跟随 activeRoute；点击行 emit navigate', async () => {
    const w = factory({ navs: [MEMO, FRPC], activeGroup: 'efficiency', activeRoute: '/ext/memo' })
    const items = w.findAll('.mod')
    expect(items[0].classes()).toContain('active')
    expect(items[0].attributes('aria-current')).toBe('page')
    expect(items[0].attributes('title')).toBe('随手记')
    await items[0].trigger('click')
    expect(w.emitted('navigate')).toEqual([['/ext/memo']])
  })

  it('图标双轨：`i:` 前缀渲染 AppIcon SVG，裸 emoji 文本回退', () => {
    const w = factory({ navs: [MEMO], activeGroup: 'efficiency' })
    expect(w.find('.mod-icon svg.app-icon').exists()).toBe(true)
    const w2 = factory({ navs: [nav('/ext/wifi', 'WiFi 密码', '📶')], activeGroup: 'network' })
    expect(w2.find('.mod-icon svg').exists()).toBe(false)
    expect(w2.find('.mod-icon').text()).toBe('📶')
  })

  it('runningIds 点亮行内绿点并入页脚「M 运行中」（缺省空数组零绿点）', async () => {
    const w = factory({ navs: [MEMO], activeRoute: '/ext/memo', runningIds: ['memo'] })
    expect(w.find('.mod-run').exists()).toBe(true)
    expect(w.find('.foot-run b').text()).toBe('1')
    await w.setProps({ runningIds: [] })
    expect(w.find('.mod-run').exists()).toBe(false)
    expect(w.find('.foot-run b').text()).toBe('0')
  })

  it('页脚统计当前分类：N 个模块', () => {
    const w = factory({ navs: [MEMO, nav('/ext/papertodo', '纸待办', 'i:clipboard-list')], activeGroup: 'efficiency' })
    expect(w.find('.foot-counts span').text()).toBe('2 个模块')
  })
})

describe('首页态：常用与最近使用', () => {
  it('activeRoute="/" 面板显示首页态：常用固定三项（存在才显示）+ 最近使用空提示', () => {
    const w = factory({ navs: [MEMO, FRPC, EVERYTHING, SNIPASTE] })
    expect(w.find('.panel-title').text()).toBe('工作台')
    const labels = w.findAll('.panel-section-label').map((l) => l.text())
    expect(labels).toEqual(['常用', '最近使用'])
    expect(w.findAll('.mod').map((b) => b.find('.mod-name').text())).toEqual([
      'FRP 内网穿透', 'Everything 搜索', 'Snipaste 截图',
    ])
    expect(w.find('.panel-hint').text()).toContain('自动记录最近使用')
  })

  it('未启用的常用模块跳过不渲染', () => {
    const w = factory({ navs: [FRPC] })
    expect(w.findAll('.mod')).toHaveLength(1)
  })

  it('导航到模块后写入 hanxi.recentRoutes（去重置顶、≤6 条），重挂载后可在面板点击直达', async () => {
    const navs = [MEMO, FRPC, EVERYTHING, SNIPASTE]
    const w = factory({ navs, activeGroup: 'efficiency' })
    await w.findAll('.mod')[0].trigger('click') // /ext/memo
    await w.setProps({ activeGroup: 'network' })
    const frpcBtn = w.findAll('.mod').find((b) => b.find('.mod-name').text() === 'FRP 内网穿透')!
    await frpcBtn.trigger('click')
    const stored = JSON.parse(localStorage.getItem(RECENT_ROUTES_KEY)!)
    expect(stored).toEqual(['/frpc', '/ext/memo'])
    w.unmount()
    // 重挂载（新组件实例从 localStorage 恢复），首页态最近使用可点击直达
    const w2 = factory({ navs })
    expect(w2.findAll('.panel-hint')).toHaveLength(0)
    const recents = w2.findAll('.mod') // 常用 3 + 最近 2
    expect(recents.map((b) => b.find('.mod-name').text()).slice(3)).toEqual(['FRP 内网穿透', '随手记'])
    await recents[3].trigger('click')
    expect(w2.emitted('navigate')).toEqual([['/frpc']])
  })

  it('最近使用截断 6 条且脏数据不炸外壳', () => {
    localStorage.setItem(RECENT_ROUTES_KEY, '{bad json')
    const w = factory({ navs: [MEMO] })
    expect(w.find('.panel-hint').exists()).toBe(true)
    localStorage.setItem(RECENT_ROUTES_KEY, JSON.stringify(['/ext/nope', '/ext/memo']))
    const w2 = factory({ navs: [MEMO] })
    // 下架路由 /ext/nope 被过滤，仅渲染仍存在的「随手记」（其 icon 为 i: 前缀走 SVG）
    expect(w2.findAll('.mod').map((b) => b.find('.mod-name').text())).toEqual(['随手记'])
  })
})

describe('props/emits 向后兼容通道', () => {
  it('通知入口经 rail 上抛 toggle-drawer，未读徽标显隐', async () => {
    const w = factory({ unreadCount: 3 })
    expect(w.find('.nav-badge').text()).toBe('3')
    await w.find('.notif-nav-btn').trigger('click')
    expect(w.emitted('toggle-drawer')).toHaveLength(1)
    await w.setProps({ unreadCount: 0 })
    expect(w.find('.nav-badge').exists()).toBe(false)
  })

  it('面板折叠：.panel-collapse-btn 切换根节点 panel-collapsed 类并持久化，rail 回展钮可复原', async () => {
    const w = factory()
    expect(w.classes()).not.toContain('panel-collapsed')
    expect(w.find('.panel-toggle').exists()).toBe(false)

    await w.find('.panel-collapse-btn').trigger('click')
    expect(w.classes()).toContain('panel-collapsed')
    expect(localStorage.getItem('hanxi.navPanelCollapsed')).toBe('1')
    // 折叠态 rail 浮出唯一回展入口
    const panelBtn = w.find('.panel-toggle')
    expect(panelBtn.exists()).toBe(true)
    await panelBtn.trigger('click')
    expect(w.classes()).not.toContain('panel-collapsed')
    expect(localStorage.getItem('hanxi.navPanelCollapsed')).toBe('0')
  })

  it('折叠态重新挂载读取持久化记忆', async () => {
    localStorage.setItem('hanxi.navPanelCollapsed', '1')
    const w = factory()
    expect(w.classes()).toContain('panel-collapsed')
  })

  describe('窄屏（≤1100px）flyout', () => {
    /** stub matchMedia：仅 1100px 断点按参数命中，其余（prefers-color-scheme 等）恒 false。 */
    function mockNarrow(narrow: boolean) {
      vi.spyOn(window, 'matchMedia').mockImplementation(
        (query: string) =>
          ({
            matches: query.includes('1100') ? narrow : false,
            media: query,
            onchange: null,
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
            addListener: vi.fn(),
            removeListener: vi.fn(),
            dispatchEvent: vi.fn(),
          }) as unknown as MediaQueryList,
      )
    }

    afterEach(() => vi.restoreAllMocks())

    it('点 rail 分类钮：面板以 flyout 弹出（含收回遮罩），点遮罩即收', async () => {
      mockNarrow(true)
      const w = factory()
      expect(w.find('.flyout-mask').exists()).toBe(false)

      await w.findAll('.rail-group')[0].trigger('click')
      expect(w.classes()).toContain('panel-flyout')
      expect(w.find('.flyout-mask').exists()).toBe(true)

      await w.find('.flyout-mask').trigger('click')
      expect(w.classes()).not.toContain('panel-flyout')
    })

    it('flyout 内导航：上抛 navigate 且浮层即点即收', async () => {
      mockNarrow(true)
      const memo = nav('/ext/memo', '随手记', 'i:sticky-note', 'efficiency')
      const w = factory({ navs: [memo] })
      await w.findAll('.rail-group')[3].trigger('click') // efficiency 组
      expect(w.classes()).toContain('panel-flyout')
      await w.find('.nav-panel .mod').trigger('click')
      expect(w.emitted('navigate')).toEqual([['/ext/memo']])
      expect(w.classes()).not.toContain('panel-flyout')
    })

    it('宽屏（matchMedia 不命中）点分类不触发 flyout', async () => {
      mockNarrow(false)
      const w = factory()
      await w.findAll('.rail-group')[0].trigger('click')
      expect(w.classes()).not.toContain('panel-flyout')
      expect(w.find('.flyout-mask').exists()).toBe(false)
    })
  })

  it('rail 核心页导航同样上抛 navigate，窄屏抽屉内导航后自动收回', async () => {
    const w = factory()
    await w.find('.rail-handle').trigger('click')
    const settingsBtn = w.findAll('.rail-core').find((b) => b.attributes('title') === '设置')!
    await settingsBtn.trigger('click')
    expect(w.emitted('navigate')).toEqual([['/settings']])
    expect(w.classes()).not.toContain('rail-open')
  })

  it('状态条：backendReady 两态文案与 online 类', async () => {
    const w = factory({ backendReady: false })
    expect(w.find('.status-dot').classes()).not.toContain('online')
    expect(w.find('.status-text').text()).toBe('正在加载工作台…')
    await w.setProps({ backendReady: true })
    expect(w.find('.status-dot').classes()).toContain('online')
    expect(w.find('.status-text').text()).toBe('工作台已就绪')
  })
})
