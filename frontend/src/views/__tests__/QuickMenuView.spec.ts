// 快捷菜单模块页特征测试：状态 chip、条目预览、空态与"前往设置"导航事件契约。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuickMenuView from '../QuickMenuView.vue'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListItems: vi.fn().mockResolvedValue([]),
}))

vi.mock('../../../bindings/hanxi/internal/modules/quickmenu', () => ({
  QuickMenuService: svc,
}))

const status = { trapActive: true, holdMs: 450, moveTol: 16, itemCount: 2 }
const items = [
  { index: 0, label: 'Snipaste', type: 'exe', hint: 'D:\\tools\\Snipaste.exe' },
  { index: 1, label: '启动 Everything', type: 'command', hint: 'everything/launch' },
]

function flushMicrotasks(times = 20) {
  return (async () => { for (let i = 0; i < times; i++) await Promise.resolve() })()
}

const Host = defineComponent({ render: () => h(QuickMenuView) })

async function mountReady(st = status, list = items) {
  svc.GetStatus.mockResolvedValue(st)
  svc.ListItems.mockResolvedValue(list)
  const w = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return w
}

afterEach(() => {
  vi.clearAllMocks()
  svc.GetStatus.mockResolvedValue(status)
  svc.ListItems.mockResolvedValue(items)
})

describe('QuickMenuView', () => {
  it('钩子在位显示正向 chip，并展示阈值参数', async () => {
    const w = await mountReady()
    expect(w.find('.chip').classes()).toContain('chip-positive')
    expect(w.find('.chip').text()).toBe('监听在位')
    expect(w.find('.subtitle').text()).toContain('450')
    expect(w.find('.subtitle').text()).toContain('16')
    w.unmount()
  })

  it('钩子未启用显示警示 chip（文字状态不靠颜色单传）', async () => {
    const w = await mountReady({ ...status, trapActive: false })
    expect(w.find('.chip').classes()).toContain('chip-warning')
    expect(w.find('.chip').text()).toBe('钩子未启用')
    w.unmount()
  })

  it('条目预览含名称、mono 提示与类型标记', async () => {
    const w = await mountReady()
    const rows = w.findAll('.item-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].find('.item-label').text()).toBe('Snipaste')
    expect(rows[0].find('.item-hint').text()).toBe('D:\\tools\\Snipaste.exe')
    expect(rows[1].find('.item-kind').text()).toBe('命令')
    w.unmount()
  })

  it('空条目走占位引导，"前往设置页配置"上抛 navigate(/settings)', async () => {
    const w = await mountReady(status, [])
    expect(w.find('.empty-state').text()).toContain('尚未配置任何条目')
    await w.find('.panel-foot .btn-primary').trigger('click')
    // Host 包裹渲染下 emit 挂在子组件 wrapper 上
    expect(w.findComponent(QuickMenuView).emitted('navigate')?.[0]).toEqual(['/settings'])
    w.unmount()
  })

  it('加载失败显示错误与重试按钮', async () => {
    svc.GetStatus.mockRejectedValue(new Error('模块未初始化'))
    const w = mount(Host)
    await flushMicrotasks()
    expect(w.find('.state-error').text()).toContain('模块未初始化')
    expect(w.find('.state-box .btn')).toBeTruthy()
    w.unmount()
  })
})
