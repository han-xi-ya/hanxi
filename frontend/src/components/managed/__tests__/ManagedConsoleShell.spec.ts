import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ManagedConsoleShell from '../ManagedConsoleShell.vue'
import type { ManagedModuleAdapter, ManagedSnapshot } from '../adapter'
import type { ManagedConsoleStore } from '../store'

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
