// 快捷菜单弹窗特征测试：锁定独立 frameless 窗口视图的条目渲染、状态机与键盘收起契约。
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuickMenuPopup from '../QuickMenuPopup.vue'

const svc = vi.hoisted(() => ({
  ListItems: vi.fn().mockResolvedValue([]),
  Launch: vi.fn().mockResolvedValue(undefined),
  Dismiss: vi.fn().mockResolvedValue(undefined),
  OpenSettings: vi.fn().mockResolvedValue(undefined),
}))
const useWailsEventMock = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/hanxi/internal/modules/quickmenu', () => ({
  QuickMenuService: svc,
}))
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: useWailsEventMock,
}))

const items = [
  { index: 0, label: 'Snipaste', type: 'exe', hint: 'D:\\tools\\Snipaste.exe' },
  { index: 1, label: '启动 Everything', type: 'command', hint: 'everything/launch' },
  { index: 2, label: '局域网扫描', type: 'route', hint: '/ext/lan' },
]

function flushMicrotasks(times = 20) {
  return (async () => { for (let i = 0; i < times; i++) await Promise.resolve() })()
}

const Host = defineComponent({ render: () => h(QuickMenuPopup) })

async function mountReady(list = items) {
  svc.ListItems.mockResolvedValue(list)
  const w = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return w
}

afterEach(() => {
  vi.clearAllMocks()
  svc.ListItems.mockResolvedValue([])
  svc.Launch.mockResolvedValue(undefined)
  svc.Dismiss.mockResolvedValue(undefined)
  svc.OpenSettings.mockResolvedValue(undefined)
})

describe('QuickMenuPopup', () => {
  it('挂载即拉取条目，渲染名称与文字类型标记', async () => {
    const w = await mountReady()
    expect(svc.ListItems).toHaveBeenCalled()
    const rows = w.findAll('.menu-row')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('Snipaste')
    expect(rows[0].find('.menu-kind').text()).toBe('程序')
    expect(rows[1].find('.menu-kind').text()).toBe('命令')
    expect(rows[2].find('.menu-kind').text()).toBe('页面')
    w.unmount()
  })

  it('点击条目按索引回调 Launch', async () => {
    const w = await mountReady()
    await w.findAll('.menu-row')[1].trigger('click')
    expect(svc.Launch).toHaveBeenCalledWith(1)
    w.unmount()
  })

  it('空条目走引导态，"配置条目"打开设置页', async () => {
    const w = await mountReady([])
    expect(w.find('.popup-state').text()).toContain('还没有条目')
    await w.find('.popup-state-action').trigger('click')
    expect(svc.OpenSettings).toHaveBeenCalled()
    w.unmount()
  })

  it('加载失败显示错误与重试，重试成功后恢复列表', async () => {
    svc.ListItems.mockRejectedValueOnce(new Error('服务未就绪'))
    const w = mount(Host)
    await flushMicrotasks()
    expect(w.find('.state-error').text()).toContain('服务未就绪')
    svc.ListItems.mockResolvedValue(items)
    await w.find('.popup-state-action').trigger('click')
    await flushMicrotasks()
    expect(w.findAll('.menu-row')).toHaveLength(3)
    w.unmount()
  })

  it('Esc 收起弹窗', async () => {
    const w = await mountReady()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(svc.Dismiss).toHaveBeenCalled()
    w.unmount()
  })

  it('订阅 quickmenu:opening：每次唤出重拉条目（配置热更新）', async () => {
    const w = await mountReady()
    const call = useWailsEventMock.mock.calls.find(([name]) => name === 'quickmenu:opening')
    expect(call).toBeTruthy()
    svc.ListItems.mockClear()
    ;(call![1] as () => void)()
    await flushMicrotasks()
    expect(svc.ListItems).toHaveBeenCalled()
    w.unmount()
  })
})
