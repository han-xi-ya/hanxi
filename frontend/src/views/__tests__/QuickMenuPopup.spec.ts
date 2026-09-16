// 快捷菜单轮盘特征测试：锁定独立 frameless 圆窗视图的条目渲染、状态机与键盘收起契约。
// 2026-09 列表版 → 轮盘版收编：条目为扇区上的真实 button（.sector-btn），类型标记
// 经 aria-label 与 hub 读数呈现，空态/错误态动作按钮统一为 hub 内 .hub-action。
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
  it('挂载即拉取条目，扇区按钮渲染名称与类型标记（aria-label）', async () => {
    const w = await mountReady()
    expect(svc.ListItems).toHaveBeenCalled()
    const sectors = w.findAll('.sector-btn')
    expect(sectors).toHaveLength(3)
    // ≤8 项时图标下方带短名
    expect(sectors[0].find('.sector-name').text()).toBe('Snipaste')
    // 类型标记收进可访问性标签（exe/command/route → 程序/命令/页面）
    expect(sectors[0].attributes('aria-label')).toBe('Snipaste（程序）')
    expect(sectors[1].attributes('aria-label')).toBe('启动 Everything（命令）')
    expect(sectors[2].attributes('aria-label')).toBe('局域网扫描（页面）')
    w.unmount()
  })

  it('点击扇区按钮按条目索引回调 Launch', async () => {
    const w = await mountReady()
    await w.findAll('.sector-btn')[1].trigger('click')
    expect(svc.Launch).toHaveBeenCalledWith(1)
    w.unmount()
  })

  it('空条目 hub 走引导态，"配置条目"打开设置页', async () => {
    const w = await mountReady([])
    expect(w.find('.hub').text()).toContain('暂无条目')
    await w.find('.hub-action').trigger('click')
    expect(svc.OpenSettings).toHaveBeenCalled()
    w.unmount()
  })

  it('加载失败 hub 显示错误与重试，重试成功后恢复轮盘', async () => {
    svc.ListItems.mockRejectedValueOnce(new Error('服务未就绪'))
    const w = mount(Host)
    await flushMicrotasks()
    expect(w.find('.hub-state-error').text()).toBe('加载失败')
    svc.ListItems.mockResolvedValue(items)
    await w.find('.hub-action').trigger('click')
    await flushMicrotasks()
    expect(w.findAll('.sector-btn')).toHaveLength(3)
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
