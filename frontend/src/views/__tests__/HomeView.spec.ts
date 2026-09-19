// 工作台首页特征测试（Wave 2 重构，UI 专项 §7.2/§7.4 + ADR-0001 §1.8）：
// 锁死"数据铁律"——运行列表只来自 initialized∧enabled（不再渲染完整目录/启停钮）、
// 摘要计数为 ListModules 真实计算、常用入口=固定+最近经 navs 实时过滤（≤4）、
// 最近任务=历史服务三桶合并（无数据/取数失败整区隐藏）、待处理/可用更新分区不渲染样例。
import { KeepAlive, defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import HomeView from '../HomeView.vue'
import { useToast } from '../../composables/useToast'
import { FAV_MODULE_IDS, RECENT_ROUTES_KEY, moduleIdOfNav } from '../../components/shell/navGrouping'

const appSvc = vi.hoisted(() => ({
  ListModules: vi.fn(),
  GetNavs: vi.fn(),
  GetAppInfo: vi.fn(),
  ListOperations: vi.fn()
}))
const histSvc = vi.hoisted(() => ({
  List: vi.fn(),
  Delete: vi.fn(),
  Clear: vi.fn()
}))
const runtime = vi.hoisted(() => ({ handlers: {} as Record<string, (e: { data: unknown }) => void> }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (e: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    }
  }
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))
vi.mock('../../../bindings/hanxi/internal/app/appservice.js', () => ({ EnsureModuleActive: vi.fn() }))
vi.mock('../../../bindings/hanxi/internal/history/historyservice', () => histSvc)

const mod = (id: string, name: string, opts: { enabled?: boolean; initialized?: boolean } = {}) => ({
  id,
  name,
  description: `${name}描述`,
  version: '1.0.0',
  author: 'hanxi',
  level: 1,
  removable: false,
  enabled: opts.enabled ?? true,
  initialized: opts.initialized ?? false,
  installed: true
})

const nav = (id: string, route: string, title: string, icon = `i:${id === 'frpc' ? 'zap' : 'box'}`) => ({
  id,
  route,
  title,
  icon,
  section: 'ext',
  order: 1
})

const histRec = (id: number, funcType: string, summary = `记录 ${id}`) => ({
  id,
  funcType,
  summary,
  input: 'in',
  output: 'out',
  extra: '',
  createdAt: '2026-09-17T10:00:00+08:00'
})

function stubCore(modules: unknown[], navs: unknown[] = []) {
  appSvc.ListModules.mockResolvedValue(modules)
  appSvc.GetNavs.mockResolvedValue(navs)
  appSvc.GetAppInfo.mockResolvedValue({ version: '0.3.0', name: 'Hanxi' })
  // 统一 Operation 观察面默认空投影（需要 operation 行的用例在其后覆盖本桩）
  appSvc.ListOperations.mockResolvedValue([])
}

function stubTasks(byBucket: Record<string, unknown[]> | 'fail') {
  histSvc.List.mockImplementation((bucket: string) => {
    if (byBucket === 'fail') return Promise.reject(new Error('历史存储损坏'))
    return Promise.resolve(byBucket[bucket] ?? [])
  })
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(HomeView)) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.clearAllMocks()
  useToast().clearToast()
})

describe('HomeView（工作台首页）', () => {
  it('不再渲染完整模块目录：无卡片网格/启停钮/等级徽标，摘要为真实计数', async () => {
    stubCore(
      [
        mod('memo', '极客随手记', { initialized: true }),
        mod('wifi', 'WiFi 密码'),
        mod('ocr', '文字识别', { enabled: false })
      ],
      [nav('memo', '/ext/memo', '极客随手记')]
    )
    stubTasks('fail')
    const w = await mountView()
    // 旧目录痕迹全部消失
    expect(w.findAll('.enabled-card')).toHaveLength(0)
    expect(w.findAll('.disabled-card')).toHaveLength(0)
    expect(w.findAll('.btn-toggle-on')).toHaveLength(0)
    expect(w.findAll('.btn-toggle-off')).toHaveLength(0)
    expect(w.findAll('.level-badge')).toHaveLength(0)
    // 摘要三项 = ListModules 真实计算：运行 1 / 总数 3 / 启用 2
    expect(w.findAll('.summary-value').map((v) => v.text())).toEqual(['1', '3', '2'])
    w.unmount()
  })

  it('运行列表来自 initialized∧enabled：已启用未初始化模块不出现', async () => {
    stubCore(
      [mod('memo', '极客随手记', { initialized: true }), mod('wifi', 'WiFi 密码')],
      [nav('memo', '/ext/memo', '极客随手记'), nav('wifi', '/ext/wifi', 'WiFi 密码')]
    )
    stubTasks('fail')
    const w = await mountView()
    const rows = w.findAll('.running-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('极客随手记')
    expect(rows[0].text()).toContain('极客随手记描述') // 描述摘要
    expect(rows[0].find('svg.app-icon').exists()).toBe(true) // MODULE_PRESENTATION 图标
    w.unmount()
  })

  it('运行行直达：建档模块走首选路由，未知模块（回落 /）不发假跳转', async () => {
    stubCore(
      [mod('memo', '随手记', { initialized: true }), mod('latermod', '后加模块', { initialized: true })],
      [nav('memo', '/ext/memo', '随手记')]
    )
    stubTasks('fail')
    const w = await mountView()
    const home = w.findComponent(HomeView)
    await w.findAll('.running-row')[0].trigger('click')
    expect(home.emitted('navigate')?.[0]).toEqual(['/ext/memo'])
    // latermod 未建档且不在 navs：route 回落 '/'，被守卫拦下，不追加 emit
    await w.findAll('.running-row')[1].trigger('click')
    expect(home.emitted('navigate')).toHaveLength(1)
    w.unmount()
  })

  it('运行空态：文案 + 进入模块中心链接直达 /modules', async () => {
    stubCore([mod('memo', '随手记')], [nav('memo', '/ext/memo', '随手记')])
    stubTasks('fail')
    const w = await mountView()
    expect(w.find('.running-list').exists()).toBe(false)
    expect(w.text()).toContain('当前没有运行中的模块')
    const home = w.findComponent(HomeView)
    await w.find('.running-empty .btn').trigger('click')
    expect(home.emitted('navigate')?.[0]).toEqual(['/modules'])
    w.unmount()
  })

  it('常用入口：固定常用在前 + 最近使用补齐，去重、经 navs 可见性过滤、最多 4 个直达', async () => {
    localStorage.setItem(RECENT_ROUTES_KEY, JSON.stringify(['/ext/memo', '/ext/gone', '/ext/wifi', '/ext/ocr']))
    const navsFixture = [
      nav('frpc', '/frpc', 'frpc 联调'),
      nav('everything', '/ext/everything', 'Everything 搜索'),
      nav('memo', '/ext/memo', '极客随手记'),
      nav('wifi', '/ext/wifi', 'WiFi 密码')
      // snipaste 未启用（不在 navs）→ 固定项跳过；/ext/gone、/ext/ocr 无 nav → 实时过滤
    ]
    stubCore([mod('memo', '随手记')], navsFixture)
    stubTasks('fail')
    const w = await mountView()
    const items = w.findAll('.shortcut')
    // 固定常用部分的期望值从 navGrouping.FAV_MODULE_IDS 单一来源推导（特征测试：常用在前），不抄字面量；
    // 其后为最近使用补齐项（去重后可见两条），整体截断到 4。
    const favTitles = navsFixture
      .filter((n) => FAV_MODULE_IDS.includes(moduleIdOfNav(n)))
      .map((n) => n.title)
    const expected = [...favTitles, '极客随手记', 'WiFi 密码'].slice(0, 4)
    expect(items).toHaveLength(expected.length)
    expect(items.map((i) => i.find('.sc-name').text())).toEqual(expected)
    const home = w.findComponent(HomeView)
    await items[expected.indexOf('极客随手记')].trigger('click')
    expect(home.emitted('navigate')?.[0]).toEqual(['/ext/memo'])
    w.unmount()
  })

  it('最近任务：三桶合并按 id 降序取最近，行含图标+标题+时间', async () => {
    stubCore([], [])
    stubTasks({
      ocr: [histRec(3, 'ocr', '识别 c.png'), histRec(1, 'ocr', '识别 a.png')],
      portkill: [histRec(2, 'portkill', '查杀 8080')]
    })
    const w = await mountView()
    expect(histSvc.List).toHaveBeenCalledWith('ocr', '')
    expect(histSvc.List).toHaveBeenCalledWith('portkill', '')
    expect(histSvc.List).toHaveBeenCalledWith('envcheck', '')
    const rows = w.findAll('.task-row')
    expect(rows).toHaveLength(3)
    expect(rows.map((r) => r.find('.task-title').text())).toEqual(['识别 c.png', '查杀 8080', '识别 a.png'])
    expect(rows[0].find('svg.app-icon').exists()).toBe(true)
    expect(rows[0].find('time.task-time').text()).not.toBe('')
    w.unmount()
  })

  it('最近任务：无记录或取数失败时整区隐藏，不渲染占位样例', async () => {
    stubCore([mod('memo', '随手记', { initialized: true })], [nav('memo', '/ext/memo', '随手记')])
    stubTasks({})
    const w = await mountView()
    expect(w.find('.tasks-panel').exists()).toBe(false)
    w.unmount()

    stubTasks('fail')
    const w2 = await mountView()
    expect(w2.find('.tasks-panel').exists()).toBe(false)
    w2.unmount()
  })

  it('最近任务（Wave 4）：统一 Operation 终态与历史三桶按时间降序合并取 5，operation 行取词表文案', async () => {
    const { useOperations } = await import('../../composables/useOperations')
    stubCore([mod('markeron', 'MarkerOn 标注')], [])
    stubTasks({
      ocr: [histRec(3, 'ocr', '识别 c.png')], // 10:00
      envcheck: [{ ...histRec(4, 'envcheck', '环境检测'), createdAt: '2026-09-17T10:30:00+08:00' }],
      portkill: [{ ...histRec(2, 'portkill', '查杀 8080'), createdAt: '2026-09-17T09:00:00+08:00' }],
    })
    appSvc.ListOperations.mockResolvedValue([
      // 在途项不进最近任务列表（归模块中心在途条）
      { schema: 1, id: 'run-1', moduleId: 'markeron', kind: 'repair', phase: 'verify', status: 'running', progress: 50, cancellable: true, startedAt: '2026-09-17T13:00:00+08:00' },
      { schema: 1, id: 'ok-1', moduleId: 'markeron', kind: 'install', phase: 'done', status: 'succeeded', cancellable: false, startedAt: '2026-09-17T11:50:00+08:00', finishedAt: '2026-09-17T12:00:00+08:00' },
      { schema: 1, id: 'bad-1', moduleId: 'markeron', kind: 'update', phase: 'download', status: 'failed', cancellable: false, startedAt: '2026-09-17T11:30:00+08:00', finishedAt: '2026-09-17T11:45:00+08:00', error: { code: 'disk-full', message: '磁盘空间不足', recoverable: false } },
      { schema: 1, id: 'cxl-1', moduleId: 'markeron', kind: 'remove', phase: 'place', status: 'cancelled', cancellable: false, startedAt: '2026-09-17T08:00:00+08:00', finishedAt: '2026-09-17T08:05:00+08:00' },
    ])
    await useOperations().refresh() // 单例投影先行，模拟观察面已有现态
    const w = await mountView()
    const rows = w.findAll('.task-row')
    // 两源 7 条 → 降序截 5（最旧的 portkill 与 cancelled 出局）
    expect(rows).toHaveLength(5)
    expect(rows.map((r) => r.find('.task-title').text())).toEqual([
      'MarkerOn 标注 · 安装', // 12:00 succeeded
      'MarkerOn 标注 · 更新', // 11:45 failed
      '环境检测', // 10:30 history
      '识别 c.png', // 10:00 history
      '查杀 8080', // 09:00 history（08:05 的 cancelled 与 13:00 的 running 出局）
    ])
    // operation 行摘要：error.message 原文优先，无错误回落阶段短语
    expect(rows[1].find('.task-meta').text()).toBe('磁盘空间不足')
    expect(rows[0].find('.task-meta').text()).toBe('完成')
    expect(w.text()).not.toContain('修复') // running 项不在最近任务面出现
    // 图标仍走 AppIcon SVG（终态状态族：成功=对勾/失败=警示/取消=电源）
    expect(rows[0].find('.task-icon svg.app-icon').exists()).toBe(true)
    expect(rows[0].find('time.task-time').text()).not.toBe('')
    w.unmount()
    // 还原空投影：useOperations 是模块级单例，不清理会污染后续"整区隐藏"用例
    appSvc.ListOperations.mockResolvedValue([])
    await useOperations().refresh()
  })

  it('待处理/可用更新分区本期隐藏：页面不出现相关文案与样例数字', async () => {
    stubCore([mod('memo', '随手记', { initialized: true })], [nav('memo', '/ext/memo', '随手记')])
    stubTasks('fail')
    const w = await mountView()
    expect(w.text()).not.toContain('待处理')
    expect(w.text()).not.toContain('可用更新')
    expect(w.findAll('.summary-card')).toHaveLength(3)
    w.unmount()
  })

  it('页头「管理模块」过渡入口：emit navigate /modules', async () => {
    stubCore([], [])
    stubTasks('fail')
    const w = await mountView()
    const home = w.findComponent(HomeView)
    const btn = w.find('.modules-entry-btn')
    expect(btn.text()).toBe('管理模块')
    await btn.trigger('click')
    expect(home.emitted('navigate')?.[0]).toEqual(['/modules'])
    w.unmount()
  })

  it('ext:changed 事件触发热刷新（核心投影与最近任务同链路重拉）', async () => {
    stubCore([mod('memo', '随手记')], [nav('memo', '/ext/memo', '随手记')])
    stubTasks('fail')
    const w = await mountView()
    const before = appSvc.ListModules.mock.calls.length
    runtime.handlers['ext:changed']({ data: undefined })
    await flushPromises()
    expect(appSvc.ListModules.mock.calls.length).toBeGreaterThan(before)
    w.unmount()
  })

  it('核心数据失败：toast + 运行面板错误态可重试，不崩', async () => {
    appSvc.ListModules.mockRejectedValue(new Error('后端未就绪'))
    appSvc.GetNavs.mockRejectedValue(new Error('后端未就绪'))
    appSvc.GetAppInfo.mockRejectedValue(new Error('后端未就绪'))
    stubTasks('fail')
    const w = await mountView()
    expect(useToast().toastMsg.value).toContain('后端未就绪')
    expect(w.find('.wb-state.error').exists()).toBe(true)
    expect(w.find('.wb-state.error').text()).toContain('后端未就绪')
    w.unmount()
  })
})
