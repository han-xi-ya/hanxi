// 全屏留言牌（MsgBoardPopup）特征测试：透明壳弹窗的拉取渲染、字号内联、
// 事件热更新、Esc 与点击撤牌契约。绑定与 Wails 运行时按仓库统一打桩范式。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MsgBoardPopup from '../MsgBoardPopup.vue'

const api = vi.hoisted(() => ({
  GetBoardContent: vi.fn(),
  Dismiss: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/msgboard', () => ({
  MsgBoardService: api,
}))

async function mountBoard(content = { text: '马上回来', fontSize: 96 }) {
  api.GetBoardContent.mockResolvedValue(content)
  const wrapper = mount(MsgBoardPopup, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

describe('MsgBoardPopup', () => {
  it('挂载拉取 GetBoardContent：正文渲染、字号进内联样式', async () => {
    const wrapper = await mountBoard()
    expect(api.GetBoardContent).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.board-text').text()).toBe('马上回来')
    expect(wrapper.find('.board-text').attributes('style')).toContain('font-size: 96px')
    wrapper.unmount()
  })

  it('msgboard:changed 事件触发重新拉取（改文案热更到在挂的牌）', async () => {
    const wrapper = await mountBoard()
    api.GetBoardContent.mockResolvedValue({ text: '会议中，请勿打扰', fontSize: 64 })
    expect(runtime.handlers['msgboard:changed']).toBeTruthy()
    runtime.handlers['msgboard:changed']({ data: undefined })
    await flushPromises()
    expect(api.GetBoardContent).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.board-text').text()).toBe('会议中，请勿打扰')
    wrapper.unmount()
  })

  it('Esc 与点击牌面都走 Dismiss 撤牌', async () => {
    const wrapper = await mountBoard()
    await wrapper.find('.board').trigger('click')
    expect(api.Dismiss).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(api.Dismiss).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('拉取失败不崩牌面（保持当前内容，等待窗口重建）', async () => {
    api.GetBoardContent.mockRejectedValue(new Error('boom'))
    const wrapper = mount(MsgBoardPopup, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.find('.board').exists()).toBe(false)
    wrapper.unmount()
  })
})
