import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ManagedConsoleShell from '../ManagedConsoleShell.vue'
import type { ManagedModuleAdapter, ManagedReleaseRecord, ManagedSnapshot } from '../adapter'
import type { ManagedConsoleStore } from '../store'
import { useManagedConsole } from '../store'

const runningSnap: ManagedSnapshot = {
  state: 'running',
  version: 'v3.2.1',
  pid: 321,
  error: '',
  startedAt: '2026-09-19T00:00:00Z'
}

function fakeAdapter(overrides: Partial<ManagedModuleAdapter> = {}): ManagedModuleAdapter {
  return {
    getStatus: vi.fn(async () => ({ ...runningSnap })) as never,
    subscribeInstanceState: vi.fn(),
    subscribeProgress: vi.fn(),
    versions: {
      listInstalled: vi.fn(async () => []),
      listReleases: vi.fn(async () => []),
      download: vi.fn(async () => ({})),
      remove: vi.fn(async () => ({})),
      openDir: vi.fn(async () => ({}))
    },
    ...overrides
  }
}

const childStubs = {
  ManagedControlBar: defineComponent({
    name: 'ManagedControlBar',
    template: '<div class="control-bar-stub" />'
  }),
  ManagedChannelRow: defineComponent({
    name: 'ManagedChannelRow',
    template: '<div class="channel-row-stub" />'
  }),
  ManagedVersionPanel: defineComponent({
    name: 'ManagedVersionPanel',
    template: '<div class="version-panel-stub" />'
  }),
  ManagedExtrasCard: defineComponent({
    name: 'ManagedExtrasCard',
    template: '<div class="extras-card-stub" />'
  })
}

type ShellSlotScope = {
  snap: ManagedSnapshot | null
  state: string
  busy: boolean
  store: ManagedConsoleStore
}

type ShellNavigationSlotScope = ShellSlotScope & {
  selectTab: (key: string) => void
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ManagedConsoleShell 增强槽位契约', () => {
  it('header-badge 收到 snap/state/busy，且渲染在页签之前', async () => {
    const dangerBusy = ref(true)
    const adapter = fakeAdapter({ dangerBusy })
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter, title: '示例工具' },
      slots: {
        'header-badge': ({ snap, state, busy }: Omit<ShellSlotScope, 'store'>) =>
          h('span', {
            class: 'header-badge-probe',
            'data-version': snap?.version ?? '',
            'data-state': state,
            'data-busy': String(busy)
          })
      },
      global: { stubs: childStubs }
    })
    await flushPromises()

    const badge = wrapper.find('.header-badge-probe')
    expect(badge.attributes('data-version')).toBe('v3.2.1')
    expect(badge.attributes('data-state')).toBe('running')
    expect(badge.attributes('data-busy')).toBe('true')

    const actions = wrapper.find('.shell-header-actions').element
    expect(actions.children[0]).toBe(badge.element)
    expect(actions.children[1].classList.contains('main-tab-nav')).toBe(true)
    wrapper.unmount()
  })

  it('默认渲染 ManagedControlBar，#control-bar 可完整替换并通过 selectTab 切换版本页', async () => {
    const defaultWrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具' },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(defaultWrapper.find('.control-bar-stub').exists()).toBe(true)
    defaultWrapper.unmount()

    let controlScope: ShellNavigationSlotScope | undefined
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具' },
      slots: {
        'control-bar': (scope: ShellNavigationSlotScope) => {
          controlScope = scope
          return h(
            'button',
            {
              class: 'control-bar-probe',
              onClick: () => scope.selectTab('versions')
            },
            '前往版本管理'
          )
        }
      },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(wrapper.find('.control-bar-stub').exists()).toBe(false)
    expect(controlScope?.snap?.version).toBe('v3.2.1')
    expect(controlScope?.state).toBe('running')
    expect(controlScope?.busy).toBe(false)
    expect(controlScope?.store.snap).toBe(controlScope?.snap)
    expect(controlScope?.selectTab).toBeTypeOf('function')

    const panels = wrapper.findAll('.tab-body')
    expect(panels[0].attributes('style') ?? '').not.toContain('display: none')
    expect(panels[1].attributes('style') ?? '').toContain('display: none')
    await wrapper.find('.control-bar-probe').trigger('click')
    expect(panels[0].attributes('style') ?? '').toContain('display: none')
    expect(panels[1].attributes('style') ?? '').not.toContain('display: none')
    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('#icon 透传到 PageHeader 标题组', async () => {
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具' },
      slots: {
        icon: () => h('span', { class: 'shell-icon-probe', 'aria-hidden': 'true' }, 'S')
      },
      global: { stubs: childStubs }
    })
    await flushPromises()

    const heading = wrapper.find('.heading-group')
    expect(heading.find('.shell-icon-probe').exists()).toBe(true)
    expect(heading.element.children[0].classList.contains('shell-icon-probe')).toBe(true)
    expect(heading.find('h1').text()).toBe('示例工具')
    wrapper.unmount()
  })

  it('tabIdPrefix 为 tab 与 panel 建立双向 ARIA 接线，缺省不生成 id', async () => {
    const defaultWrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具' },
      global: { stubs: childStubs }
    })
    await flushPromises()
    expect(defaultWrapper.findAll('[role="tab"]')[0].attributes('id')).toBeUndefined()
    expect(defaultWrapper.findAll('.tab-body')[0].attributes('id')).toBeUndefined()
    defaultWrapper.unmount()

    const wrapper = mount(ManagedConsoleShell, {
      props: {
        adapter: fakeAdapter(),
        title: '示例工具',
        consoleTabKey: 'annotate',
        tabIdPrefix: 'managed-probe',
        tabLabel: '示例工具区域'
      },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(wrapper.find('[role="tablist"]').attributes('aria-label')).toBe('示例工具区域')
    const tabs = wrapper.findAll('[role="tab"]')
    const panels = wrapper.findAll('.tab-body')
    expect(tabs.map((tab) => [tab.attributes('id'), tab.attributes('aria-controls')])).toEqual([
      ['managed-probe-annotate-tab', 'managed-probe-annotate-panel'],
      ['managed-probe-versions-tab', 'managed-probe-versions-panel']
    ])
    expect(panels.map((panel) => [panel.attributes('id'), panel.attributes('aria-labelledby')])).toEqual([
      ['managed-probe-annotate-panel', 'managed-probe-annotate-tab'],
      ['managed-probe-versions-panel', 'managed-probe-versions-tab']
    ])
    wrapper.unmount()
  })

  it('versionsTabCount=3 时版本页签追加计数', async () => {
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具', versionsTabCount: 3 },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(wrapper.findAll('.main-tab-btn')[1].text()).toBe('📦 版本管理 3')
    wrapper.unmount()
  })

  it('versions-body 收到 snap/state/busy/store，并完整替换默认版本区', async () => {
    const dangerBusy = ref(true)
    let slotScope: ShellSlotScope | undefined
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter({ dangerBusy }), title: '示例工具' },
      slots: {
        'versions-body': (scope: ShellSlotScope) => {
          slotScope = scope
          return h('div', { class: 'versions-body-probe' }, scope.store === undefined ? 'missing' : 'custom')
        }
      },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(wrapper.find('.versions-body-probe').text()).toBe('custom')
    expect(slotScope?.snap?.version).toBe('v3.2.1')
    expect(slotScope?.state).toBe('running')
    expect(slotScope?.busy).toBe(true)
    expect(slotScope?.store.snap).toBe(slotScope?.snap)
    expect(wrapper.find('.version-panel-stub').exists()).toBe(false)
    expect(wrapper.find('.channel-row-stub').exists()).toBe(false)
    wrapper.unmount()
  })

  it('默认 versions body 在 adapter.channel 存在时渲染 ManagedChannelRow', async () => {
    const adapter = fakeAdapter({
      channel: {
        options: [{ value: 'stable', label: '稳定版' }],
        get: vi.fn(async () => 'stable'),
        set: vi.fn(async () => ({}))
      }
    })
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter, title: '示例工具' },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(wrapper.find('.channel-row-stub').exists()).toBe(true)
    expect(wrapper.find('.version-panel-stub').exists()).toBe(true)
    wrapper.unmount()
  })
})

// ---------- 波 2A 地基：store 注入 / versions-prepend / 单页签形态 ----------
describe('ManagedConsoleShell 波 2A 地基', () => {
  it('外部注入 store 时壳不自建：共享件同一句柄、订阅与轮询不双份', async () => {
    const adapter = fakeAdapter()
    let external!: ManagedConsoleStore
    let scopeStore: ManagedConsoleStore | undefined
    const Probe = defineComponent({
      setup() {
        // 视图 setup 期自建（Recordly/Paseo 迁移形的公共层替身）
        external = useManagedConsole(adapter)
        return () =>
          h(ManagedConsoleShell, { adapter, title: '示例工具', store: external }, {
            'control-bar': (scope: ShellNavigationSlotScope) => {
              scopeStore = scope.store
              return h('span', { class: 'injected-probe' })
            }
          })
      }
    })
    const wrapper = mount(Probe, { global: { stubs: childStubs } })
    await flushPromises()

    // 双建防线：一次外部 store 的接线 = 各订阅恰一次调用（壳再建即 ×2）
    expect(adapter.subscribeInstanceState).toHaveBeenCalledTimes(1)
    expect(adapter.subscribeProgress).toHaveBeenCalledTimes(1)
    // 作用域槽与注入句柄同源（壳内不再另立真相）
    expect(scopeStore).toBe(external)
    // 未安装态直落版本页的接线钩子写在注入实例上
    expect(external.goVersions).toBeTypeOf('function')
    external.goVersions?.()
    await wrapper.vm.$nextTick()
    const panels = wrapper.findAll('.tab-body')
    expect(panels[1].attributes('style') ?? '').not.toContain('display: none')
    wrapper.unmount()
  })

  it('#versions-prepend 渲染于 channel 块与版本面板之前（papertodo 变体卡位）', async () => {
    let prependScope: ShellSlotScope | undefined
    const adapter = fakeAdapter({
      channel: {
        options: [{ value: 'stable', label: '稳定版' }],
        get: vi.fn(async () => 'stable'),
        set: vi.fn(async () => ({}))
      }
    })
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter, title: '示例工具' },
      slots: {
        'versions-prepend': (scope: ShellSlotScope) => {
          prependScope = scope
          return h('div', { class: 'versions-prepend-probe' }, '变体卡')
        }
      },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(prependScope?.snap?.version).toBe('v3.2.1')
    expect(prependScope?.state).toBe('running')
    expect(prependScope?.store).toBeDefined()

    const versionsBody = wrapper.findAll('.tab-body')[1]
    const childClasses = Array.from(versionsBody.element.children).map((el) => el.className)
    expect(childClasses).toEqual([
      'versions-prepend-probe',
      'channel-row-stub',
      'version-panel-stub'
    ])
    wrapper.unmount()
  })

  it('showConsoleTab=false 单页签形态：页签钮与控制台体整段缺席，版本体常显且摘 tabpanel 身份', async () => {
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具', showConsoleTab: false, tabIdPrefix: 'dz' },
      slots: {
        'header-badge': () => h('span', { class: 'single-tab-badge-probe' }, '徽标'),
        default: () => h('span', { class: 'console-body-probe' }, '控制台主体')
      },
      global: { stubs: childStubs }
    })
    await flushPromises()

    // douzy 现状形制：无 MainTabNav、无任何 role=tab 钮
    expect(wrapper.find('.main-tab-nav').exists()).toBe(false)
    expect(wrapper.findAll('[role="tab"]')).toHaveLength(0)
    // 徽标透传位照常（display:contents 容器仅剩徽标）
    expect(wrapper.find('.single-tab-badge-probe').exists()).toBe(true)
    // 控制台 tab-body 整段 v-if 缺席：仅剩版本体，且常显、无 tabpanel ARIA 身份
    const panels = wrapper.findAll('.tab-body')
    expect(panels).toHaveLength(1)
    expect(panels[0].attributes('style') ?? '').not.toContain('display: none')
    expect(panels[0].attributes('role')).toBeUndefined()
    expect(panels[0].attributes('aria-labelledby')).toBeUndefined()
    expect(panels[0].attributes('id')).toBeUndefined()
    // 控制台体槽位（默认槽/#control-bar 域）不实例化
    expect(wrapper.find('.console-body-probe').exists()).toBe(false)
    expect(wrapper.find('.control-bar-stub').exists()).toBe(false)
    // 版本默认体照常可注入（列槽/面板承接位不受单页签影响）
    expect(wrapper.find('.version-panel-stub').exists()).toBe(true)
    wrapper.unmount()
  })

  it('showConsoleTab 缺省 true：双页签形制逐字不变（回归护栏）', async () => {
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具' },
      global: { stubs: childStubs }
    })
    await flushPromises()

    expect(wrapper.findAll('.main-tab-btn').map((b) => b.text())).toEqual(['🔀 控制台', '📦 版本管理'])
    const panels = wrapper.findAll('.tab-body')
    expect(panels).toHaveLength(2)
    expect(panels[1].attributes('role')).toBe('tabpanel')
    wrapper.unmount()
  })
})

// ---------- 波 5：#release-actions 壳转发位 / #console-extra 作用域含 store ----------

describe('ManagedConsoleShell release-actions 转发位', () => {
  /** 只桩控制条/联动卡，面板用真实件——槽转发链路整体过一遍（对照 #primary-action 走 ControlBar 的既有形制）。 */
  const stubsMinusPanel = {
    ManagedControlBar: childStubs.ManagedControlBar,
    ManagedExtrasCard: childStubs.ManagedExtrasCard,
  }

  const releases = [
    { version: 'v2.0.0', published: '2026-09-01T00:00:00Z', size: 2048 },
    { version: 'v1.9.0', published: '2026-08-01T00:00:00Z', size: 1024, isPre: true, form: 'portable' },
  ]

  function probeAdapter() {
    return fakeAdapter({
      versions: {
        listInstalled: vi.fn(async () => []),
        listReleases: vi.fn(async () => releases),
        download: vi.fn(async () => ({})),
        remove: vi.fn(async () => ({})),
        openDir: vi.fn(async () => ({})),
      },
    })
  }

  it('作用域逐行携带完整 release 记录，且渲染于该操作位末端（标准钮之后）', async () => {
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: probeAdapter(), title: '示例工具' },
      slots: {
        'release-actions': ({ release }: { release: ManagedReleaseRecord }) =>
          h(
            'button',
            {
              class: 'release-probe',
              'data-version': release.version,
              'data-size': String(release.size),
              'data-form': release.form ?? '',
            },
            `🌐 发布页 ${release.version}`,
          ),
      },
      global: { stubs: stubsMinusPanel },
    })
    await flushPromises()

    const probes = wrapper.findAll('.release-probe')
    expect(probes).toHaveLength(2)
    // 逐行配对：第一行 v2.0.0，第二行 v1.9.0（预发布行作用域仍拿完整记录）
    expect(probes[0].attributes('data-version')).toBe('v2.0.0')
    expect(probes[0].attributes('data-size')).toBe('2048')
    expect(probes[0].text()).toBe('🌐 发布页 v2.0.0')
    expect(probes[1].attributes('data-version')).toBe('v1.9.0')
    expect(probes[1].attributes('data-form')).toBe('portable')

    // 位序契约：转发内容落进远程行「操作」列，且排在标准动作钮（下载安装）之后
    const rows = wrapper.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(2)
    for (const [i, row] of rows.entries()) {
      const actionCell = row.findAll('td').at(-1)!
      expect(actionCell.element.children).toHaveLength(2)
      expect(actionCell.element.children[0].textContent).toContain('下载安装')
      expect(actionCell.element.lastElementChild).toBe(probes[i].element)
    }
    wrapper.unmount()
  })

  it('#versions-body 在场时默认体整体让位，本转发位不生效（契约不变）', async () => {
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: probeAdapter(), title: '示例工具' },
      slots: {
        'versions-body': () => h('div', { class: 'versions-body-probe' }, '方言版本区'),
        'release-actions': ({ release }: { release: ManagedReleaseRecord }) =>
          h('button', { class: 'release-probe' }, `发布页 ${release.version}`),
      },
      global: { stubs: stubsMinusPanel },
    })
    await flushPromises()

    expect(wrapper.find('.versions-body-probe').text()).toBe('方言版本区')
    expect(wrapper.findAll('.release-probe')).toHaveLength(0)
    wrapper.unmount()
  })
})

describe('ManagedConsoleShell console-extra 位', () => {
  it('#console-extra 作用域携带 store 句柄：与快照同一真相、busy 随 dangerBusy 联动，且落位状态头之后主体之前', async () => {
    const dangerBusy = ref(false)
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter({ dangerBusy }), title: '示例工具' },
      slots: {
        'console-extra': (scope: ShellSlotScope) =>
          h(
            'div',
            {
              class: 'console-extra-probe',
              'data-state': scope.state,
              'data-busy': String(scope.busy),
              'data-store-snap': String(scope.store?.snap === scope.snap),
            },
            '提权选择条',
          ),
        default: () => h('div', { class: 'console-body-probe' }),
      },
      global: { stubs: childStubs },
    })
    await flushPromises()

    const probe = wrapper.find('.console-extra-probe')
    expect(probe.attributes('data-state')).toBe('running')
    expect(probe.attributes('data-store-snap')).toBe('true') // 作用域 store 与快照同源
    expect(probe.attributes('data-busy')).toBe('false')

    // 位序：状态头（control-bar）之后、控制台主体之前
    const consoleBody = wrapper.findAll('.tab-body')[0]
    expect(Array.from(consoleBody.element.children).map((el) => el.className)).toEqual([
      'control-bar-stub',
      'console-extra-probe',
      'console-body-probe',
    ])

    dangerBusy.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.console-extra-probe').attributes('data-busy')).toBe('true')
    wrapper.unmount()
  })

  it('单页签形态（showConsoleTab=false）：#console-extra 位不实例化', async () => {
    const wrapper = mount(ManagedConsoleShell, {
      props: { adapter: fakeAdapter(), title: '示例工具', showConsoleTab: false },
      slots: {
        'console-extra': () => h('div', { class: 'console-extra-probe' }),
      },
      global: { stubs: childStubs },
    })
    await flushPromises()
    expect(wrapper.find('.console-extra-probe').exists()).toBe(false)
    wrapper.unmount()
  })
})
