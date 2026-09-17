// SnipCardView 特征测试：透明壳小窗里的结果卡契约——
// 事件推送渲染全文/元信息、复制按钮走 Go 代理、失败保卡给提示、
// 空文本走"未识别"态、Esc 收起。绑定与事件按仓库统一打桩范式。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnipCardView from '../SnipCardView.vue'
import { useToast } from '../../composables/useToast'

const api = vi.hoisted(() => ({
  GetSnipResult: vi.fn(),
  SnipCopyText: vi.fn(),
  SnipCardDismiss: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/ocr/ocrservice', () => api)

const done = { ok: true, text: '第一行\n第二行', lineCount: 2, elapsedMs: 88, error: '', copied: true, cancelled: false }

async function mountView() {
  api.GetSnipResult.mockResolvedValue([done, true])
  const wrapper = mount(SnipCardView, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('SnipCardView', () => {
  it('挂载拉取 GetSnipResult：全文/行数/耗时/已复制 全渲染', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('.snip-text').text()).toContain('第一行')
    expect(wrapper.find('.snip-meta').text()).toContain('2 行')
    expect(wrapper.find('.snip-meta').text()).toContain('88 ms')
    expect(wrapper.find('.snip-copied').text()).toContain('已复制')
    wrapper.unmount()
  })

  it('事件推送覆盖渲染；取消帧与空帧不顶掉内容', async () => {
    const wrapper = await mountView()
    runtime.handlers['ocr:snip-result']({ data: { ...done, text: '新内容', copied: false } })
    await flushPromises()
    expect(wrapper.find('.snip-text').text()).toContain('新内容')
    runtime.handlers['ocr:snip-result']({ data: { ...done, ok: false, text: '', cancelled: true } })
    runtime.handlers['ocr:snip-result']({ data: null })
    await flushPromises()
    expect(wrapper.find('.snip-text').text()).toContain('新内容') // 保持不被顶空
    wrapper.unmount()
  })

  it('复制按钮：Go 代理成功即收起；失败保卡提示', async () => {
    const wrapper = await mountView()
    api.SnipCopyText.mockResolvedValue(undefined)
    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()
    expect(api.SnipCopyText).toHaveBeenCalledTimes(1)

    api.SnipCopyText.mockRejectedValueOnce(new Error('剪贴板被占用'))
    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()
    expect(wrapper.find('.snip-tip').text()).toContain('剪贴板')
    expect(wrapper.find('.snip-text').exists()).toBe(true) // 卡仍在，可手动选字
    wrapper.unmount()
  })

  it('空文本成功态：显示未识别文案，复制钮禁用', async () => {
    api.GetSnipResult.mockResolvedValue([{ ...done, text: '', lineCount: 0, copied: false }, true])
    const wrapper = mount(SnipCardView, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.find('.snip-state').text()).toContain('未识别到文字')
    expect(wrapper.find('.btn-primary').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('Esc 收起卡片', async () => {
    const wrapper = await mountView()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(api.SnipCardDismiss).toHaveBeenCalled()
    wrapper.unmount()
  })
})
