// 模块中心视图特征测试：骨架/41 项渲染规模/筛选分桶/搜索/错误重试、
// 安装与卸载接线（builtin-logical 如实确认框）、open 直达、启停接线、
// 成功后经 ext:changed 防抖重拉（前端不手工 patch 投影）、
// 顶部 OperationBanner 与卡片在途徽标（Wave 4 统一操作呈现）。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Component } from 'vue'

const appSvc = vi.hoisted(() => ({
  ListCatalog: vi.fn(),
  ListModuleStates: vi.fn(),
  ListOperations: vi.fn(),
  DismissResumable: vi.fn(),
  SetModuleEnabled: vi.fn(),
  SetModuleInstalled: vi.fn(),
}))
const runtime = vi.hoisted(() => ({ handlers: {} as Record<string, () => void> }))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: () => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

type UseConfirm = typeof import('../../composables/useConfirm')['useConfirm']
type UseToast = typeof import('../../composables/useToast')['useToast']

let view: Component
let useConfirm: UseConfirm
let useToast: UseToast

function cat(id: string, name = `模块 ${id}`) {
  return {
    id, name, description: `这是 ${name} 的功能描述`,
    category: 'desktop', deliveryKind: 'builtin-logical',
    capabilities: [], entrypoints: ['rpc'],
    compatibility: { hostRange: '*', platform: ['windows'] },
    permissions: [], owner: 'hanxi',
  }
}
const INSTALLED = { delivery: 'installed', policy: 'enabled', runtime: 'active', health: 'current', primaryAction: 'open', summary: 'running' }
const NOT_INSTALLED = { delivery: 'absent', policy: 'disabled', runtime: 'inactive', health: 'current', primaryAction: 'install', summary: 'not-installed' }

function st(id: string, over: Record<string, unknown> = {}) {
  return { schema: 1, moduleId: id, ...INSTALLED, ...over }
}

/** 41 项混合目录：10 未安装、20 运行中、2 异常（blocked/faulted+corrupt/revoked）、9 已装未运行。 */
function fortyOne() {
  const catalog = Array.from({ length: 41 }, (_, i) => cat(`m${String(i).padStart(2, '0')}`))
  const states = catalog.map((c, i) => {
    if (i < 10) return st(c.id, NOT_INSTALLED)
    if (i < 30) return st(c.id)
    if (i === 30) return st(c.id, { policy: 'blocked', runtime: 'inactive', primaryAction: 'none', reason: '策略阻止', summary: 'blocked' })
    if (i === 31) return st(c.id, { health: 'corrupt', policy: 'disabled', runtime: 'inactive', primaryAction: 'retry', summary: 'faulted' })
    return st(c.id, { policy: 'disabled', runtime: 'inactive', health: 'current', primaryAction: 'enable', summary: 'installed-disabled' })
  })
  return { catalog, states }
}

async function setup() {
  vi.resetModules()
  vi.clearAllMocks()
  runtime.handlers = {}
  view = (await import('../ModuleCenterView.vue')).default
  useConfirm = (await import('../../composables/useConfirm')).useConfirm
  useToast = (await import('../../composables/useToast')).useToast
}

async function mountLoaded(operations: unknown[] = []) {
  const { catalog, states } = fortyOne()
  appSvc.ListCatalog.mockResolvedValue(catalog)
  appSvc.ListModuleStates.mockResolvedValue(states)
  appSvc.ListOperations.mockResolvedValue(operations)
  appSvc.SetModuleEnabled.mockResolvedValue(null)
  appSvc.SetModuleInstalled.mockResolvedValue(null)
  const w = mount(view, { attachTo: document.body })
  await flushPromises()
  return w
}

function primaryBtnOf(wrapper: ReturnType<typeof mount>, id: string) {
  const card = wrapper.findAll('.module-card').find((c) => c.text().includes(`模块 ${id}`))
  return card?.find('button.primary-btn')
}

afterEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('ModuleCenterView', () => {
  beforeEach(setup)

  it('首拉未返回时展示骨架屏、不渲染真实卡片', async () => {
    appSvc.ListCatalog.mockReturnValue(new Promise(() => {}))
    appSvc.ListModuleStates.mockReturnValue(new Promise(() => {}))
    const w = mount(view, { attachTo: document.body })
    await flushPromises()
    expect(w.findAll('.skeleton-card')).toHaveLength(6)
    expect(w.findAll('.module-card')).toHaveLength(0)
    w.unmount()
  })

  it('全量目录渲染 41 张卡片，页头计数摘要 = 共 41 · 运行中 20 · 未安装 10', async () => {
    const w = await mountLoaded()
    expect(w.findAll('.module-card')).toHaveLength(41)
    expect(w.find('.count-summary').text()).toBe('共 41 · 运行中 20 · 未安装 10')
    w.unmount()
  })

  it('筛选 tabs 分桶：可安装/异常/运行中/已安装', async () => {
    const w = await mountLoaded()
    const tabs = w.findAll('.main-tab-btn')
    const byLabel = (label: string) => tabs.find((t) => t.text() === label)!
    await byLabel('可安装').trigger('click')
    expect(w.findAll('.module-card')).toHaveLength(10)
    await byLabel('异常').trigger('click')
    expect(w.findAll('.module-card')).toHaveLength(2)
    await byLabel('运行中').trigger('click')
    expect(w.findAll('.module-card')).toHaveLength(20)
    await byLabel('已安装').trigger('click')
    expect(w.findAll('.module-card')).toHaveLength(31)
    await byLabel('全部').trigger('click')
    expect(w.findAll('.module-card')).toHaveLength(41)
    w.unmount()
  })

  it('健康维度筛选档「有可用更新 N」：读 state.health 分桶，卡片呈现词表健康徽标（W2b）', async () => {
    appSvc.ListCatalog.mockResolvedValue([cat('memo'), cat('wifi'), cat('frpc')])
    appSvc.ListModuleStates.mockResolvedValue([
      st('memo', { health: 'update-available', summary: 'running-update', remoteVersion: '1.2.3' }),
      st('wifi', { health: 'update-available', policy: 'disabled', runtime: 'inactive', primaryAction: 'enable', summary: 'installed-disabled' }),
      st('frpc'),
    ])
    appSvc.ListOperations.mockResolvedValue([])
    const w = mount(view, { attachTo: document.body })
    await flushPromises()
    // 标签带真实计数，词表锚定 HEALTH_META['update-available']
    const tabs = w.findAll('.main-tab-btn')
    const updateTab = tabs.find((t) => t.text().startsWith('有可用更新'))!
    expect(updateTab.text()).toBe('有可用更新 2')
    await updateTab.trigger('click')
    const cards = w.findAll('.module-card')
    expect(cards).toHaveLength(2)
    expect(cards.map((c) => c.text())).toEqual([
      expect.stringContaining('模块 memo'),
      expect.stringContaining('模块 wifi'),
    ])
    // healthBadge 自动呈现：非 current 健康值走词表徽标（不只靠颜色，文字承载）；
    // 投影带 remoteVersion 的卡片附版本短语，无版本只有词表原文（不编造）
    expect(cards[0].findAll('.badge-row .chip').map((c) => c.text())).toContain('有可用更新 → 1.2.3')
    expect(cards[1].findAll('.badge-row .chip').map((c) => c.text())).toContain('有可用更新')
    // memo summary=running-update → 摘要徽标词表短语
    expect(cards[0].find('.summary-chip').text()).toBe('运行中，有更新')
    w.unmount()
  })

  it('搜索命中名称/ID/描述，空结果给出清除筛选通道', async () => {
    const w = await mountLoaded()
    await w.find('input[type="search"]').setValue('m07')
    expect(w.findAll('.module-card')).toHaveLength(1)
    await w.find('input[type="search"]').setValue('功能描述')
    expect(w.findAll('.module-card')).toHaveLength(41)
    await w.find('input[type="search"]').setValue('查无此词')
    expect(w.findAll('.module-card')).toHaveLength(0)
    expect(w.text()).toContain('没有符合当前筛选或搜索条件的模块')
    await w.find('.empty-state button').trigger('click')
    expect(w.findAll('.module-card')).toHaveLength(41)
    w.unmount()
  })

  it('首拉失败：错误框 + 重试成功后渲染目录', async () => {
    appSvc.ListCatalog.mockRejectedValue(new Error('后端连接中断'))
    appSvc.ListModuleStates.mockResolvedValue([])
    const w = mount(view, { attachTo: document.body })
    await flushPromises()
    expect(w.find('.error-box').text()).toContain('后端连接中断')
    expect(w.findAll('.module-card')).toHaveLength(0)
    const { catalog, states } = fortyOne()
    appSvc.ListCatalog.mockResolvedValue(catalog)
    appSvc.ListModuleStates.mockResolvedValue(states)
    await w.findAll('.error-box button').at(-1)!.trigger('click')
    await flushPromises()
    expect(w.findAll('.module-card')).toHaveLength(41)
    w.unmount()
  })

  it('安装：SetModuleInstalled(id,true) → toast 如实"加入工作台" → ext:changed 防抖重拉', async () => {
    vi.useFakeTimers()
    const w = await mountLoaded()
    const btn = primaryBtnOf(w, 'm03')
    expect(btn!.text()).toBe('加入工作台')
    await btn!.trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    expect(appSvc.SetModuleInstalled).toHaveBeenCalledWith('m03', true)
    expect(useToast().toastMsg.value).toBe('已将「模块 m03」加入工作台')
    const before = appSvc.ListCatalog.mock.calls.length
    runtime.handlers['ext:changed']()
    await vi.advanceTimersByTimeAsync(300)
    expect(appSvc.ListCatalog.mock.calls.length).toBe(before + 1)
    w.unmount()
  })

  it('次操作卸载：builtin-logical 确认框列明三项如实口径，确认后走 SetModuleInstalled(false)', async () => {
    const w = await mountLoaded()
    const card = w.findAll('.module-card').find((c) => c.text().includes('模块 m15'))!
    const uninstallBtn = card.findAll('.secondary-row button').find((b) => b.text() === '卸载')!
    await uninstallBtn.trigger('click')
    const { confirmState, settleConfirm } = useConfirm()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('卸载「模块 m15」？')
    expect((confirmState.options.details ?? []).map((d) => d.value)).toEqual([
      '功能入口与安装凭据（receipt）',
      '默认保留，不随卸载删除',
      '内建代码随宿主发布，卸载后 hanxi.exe 体积不变',
    ])
    settleConfirm(true)
    await flushPromises()
    expect(appSvc.SetModuleInstalled).toHaveBeenCalledWith('m15', false)
    expect(useToast().toastMsg.value).toContain('已卸载「模块 m15」')
    w.unmount()
  })

  it('确认框取消：不发起卸载事务', async () => {
    const w = await mountLoaded()
    const card = w.findAll('.module-card').find((c) => c.text().includes('模块 m16'))!
    await card.findAll('.secondary-row button').find((b) => b.text() === '卸载')!.trigger('click')
    const { confirmState, settleConfirm } = useConfirm()
    settleConfirm(false)
    await flushPromises()
    expect(confirmState.open).toBe(false)
    expect(appSvc.SetModuleInstalled).not.toHaveBeenCalled()
    w.unmount()
  })

  it('次操作停用：SetModuleEnabled(id,false) + 回收资源 toast', async () => {
    const w = await mountLoaded()
    const card = w.findAll('.module-card').find((c) => c.text().includes('模块 m17'))!
    await card.findAll('.secondary-row button').find((b) => b.text() === '停用')!.trigger('click')
    await flushPromises()
    expect(appSvc.SetModuleEnabled).toHaveBeenCalledWith('m17', false)
    expect(useToast().toastMsg.value).toBe('已停用「模块 m17」，已回收运行时资源')
    w.unmount()
  })

  it('主操作 open：按 MODULE_PRESENTATION 首选路由上抛 navigate', async () => {
    // memo 在 MODULE_PRESENTATION 建档 → open 直达 /ext/memo
    appSvc.ListCatalog.mockResolvedValue([cat('memo')])
    appSvc.ListModuleStates.mockResolvedValue([st('memo')])
    const w = mount(view, { attachTo: document.body })
    await flushPromises()
    await primaryBtnOf(w, 'memo')!.trigger('click')
    expect(w.emitted('navigate')?.[0]).toEqual(['/ext/memo'])
    w.unmount()
  })

  it('未建档操作（update/retry 等 Wave 4+ 动作面）如实告知，不虚发请求', async () => {
    const w = await mountLoaded()
    const btn = primaryBtnOf(w, 'm31') // primaryAction=retry
    await btn!.trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('将在后续版本开放')
    expect(appSvc.SetModuleInstalled).not.toHaveBeenCalled()
    expect(appSvc.SetModuleEnabled).not.toHaveBeenCalled()
    w.unmount()
  })

  it('Wave 4：无在途/无残留时不渲染 OperationBanner（空态不占位）', async () => {
    const w = await mountLoaded()
    expect(w.find('.operation-banner').exists()).toBe(false)
    w.unmount()
  })

  it('Wave 4：resumable 回灌 → 顶部恢复条就位（错误框之后、筛选之前），前往重试上抛 navigate', async () => {
    appSvc.DismissResumable.mockResolvedValue(null)
    const w = await mountLoaded([
      {
        schema: 1, id: 'resumed-txn-1', moduleId: 'm05', kind: 'install', phase: 'unpack',
        status: 'failed', cancellable: false, startedAt: '2026-09-18T09:00:00+08:00',
        error: { code: 'resumable', message: '上次进程未收口的托管事务', recoverable: true },
      },
    ])
    const banner = w.find('.operation-banner')
    expect(banner.exists()).toBe(true)
    // 呈现位置：错误框之后、筛选工具条之前（modules-layout 子节点序）
    const children = Array.from(w.find('.modules-layout').element.children)
    expect(children.indexOf(banner.element)).toBeGreaterThan(-1)
    expect(children.indexOf(banner.element)).toBeLessThan(children.indexOf(w.find('.toolbar-row').element))
    expect(banner.text()).toContain('未收口的模块事务')
    const goto = banner.findAll('button').find((b) => b.text() === '前往重试')!
    await goto.trigger('click')
    expect(w.emitted('navigate')?.[0]).toEqual(['/ext/m05'])
    w.unmount()
  })

  it('Wave 4：在途 Operation → 卡片徽标呈现 phase+进度，且该卡动作钮禁用（不新发事务）', async () => {
    const w = await mountLoaded([
      {
        schema: 1, id: 'run-1', moduleId: 'm03', kind: 'install', phase: 'download',
        status: 'running', progress: 33, cancellable: true, startedAt: '2026-09-18T10:00:00+08:00',
      },
    ])
    const card = w.findAll('.module-card').find((c) => c.text().includes('模块 m03'))!
    const chips = card.findAll('.badge-row .chip').map((c) => c.text())
    expect(chips.some((t) => t.includes('下载') && t.includes('33%'))).toBe(true)
    // busy 并入：在途期间主按钮禁用，点击不发 SetModuleInstalled
    const btn = card.find('button.primary-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    await btn.trigger('click')
    expect(appSvc.SetModuleInstalled).not.toHaveBeenCalled()
    // 顶部在途条同时就位
    expect(w.find('.op-running').exists()).toBe(true)
    w.unmount()
  })
})
