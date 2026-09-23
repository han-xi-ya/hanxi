// SnipCardView 特征测试：Acrylic 小窗里的结果卡契约——
// 事件推送渲染全文/元信息、复制按钮走 Go 代理、失败保卡给提示、
// 空文本走"未识别"态、元信息条拖拽、Esc 收起。绑定与事件按仓库统一打桩范式。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnipCardView from '../SnipCardView.vue'
import { useToast } from '../../composables/useToast'

const api = vi.hoisted(() => ({
  GetSnipResult: vi.fn(),
  SnipCopyText: vi.fn(),
  SnipCardDismiss: vi.fn(),
  CardDragStart: vi.fn(),
  CardDragEnd: vi.fn(),
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

  it('拖拽把手：元信息条 mousedown 起跟手，window mouseup 结束', async () => {
    api.CardDragStart.mockResolvedValue(undefined)
    api.CardDragEnd.mockResolvedValue(undefined)
    const wrapper = await mountView()
    await wrapper.find('.snip-grip').trigger('mousedown', { button: 0 })
    expect(api.CardDragStart).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new MouseEvent('mouseup', { button: 0 }))
    await flushPromises()
    expect(api.CardDragEnd).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('Esc 收起卡片', async () => {
    const wrapper = await mountView()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(api.SnipCardDismiss).toHaveBeenCalled()
    wrapper.unmount()
  })

  describe('字号缩放（N42③）', () => {
    afterEach(() => localStorage.removeItem('hanxi.snipcard.fs'))

    it('未调档默认 14px（与 --text-md 同值，视觉零变化）', async () => {
      const wrapper = await mountView()
      expect(wrapper.find('.snip-card').attributes('style')).toContain('--snip-fs: 14px')
      wrapper.unmount()
    })

    it('A+/A− 步进 2px 并钳制边界，档位持久化', async () => {
      const wrapper = await mountView()
      const fsBtns = () => wrapper.findAll('.snip-fs-btn')
      await fsBtns()[1].trigger('click') // A+
      expect(wrapper.find('.snip-card').attributes('style')).toContain('--snip-fs: 16px')
      expect(localStorage.getItem('hanxi.snipcard.fs')).toBe('16')
      await fsBtns()[0].trigger('click') // A−
      await fsBtns()[0].trigger('click')
      expect(wrapper.find('.snip-card').attributes('style')).toContain('--snip-fs: 12px')
      expect(fsBtns()[0].attributes('disabled')).toBeDefined() // 下界禁用，上界仍可用
      expect(fsBtns()[1].attributes('disabled')).toBeUndefined()
      wrapper.unmount()
    })

    it('Ctrl+滚轮上滚放大/下滚缩小；无修饰滚轮不碰字号', async () => {
      const wrapper = await mountView()
      await wrapper.find('.snip-card').trigger('wheel', { ctrlKey: true, deltaY: -100 })
      expect(wrapper.find('.snip-card').attributes('style')).toContain('--snip-fs: 16px')
      await wrapper.find('.snip-card').trigger('wheel', { ctrlKey: true, deltaY: 120 })
      expect(wrapper.find('.snip-card').attributes('style')).toContain('--snip-fs: 14px')
      await wrapper.find('.snip-card').trigger('wheel', { deltaY: -100 }) // 无 Ctrl
      expect(wrapper.find('.snip-card').attributes('style')).toContain('--snip-fs: 14px')
      wrapper.unmount()
    })

    it('档位记忆跨挂载恢复，越界坏值装载即钳制', async () => {
      localStorage.setItem('hanxi.snipcard.fs', '99')
      const wrapper = await mountView()
      expect(wrapper.find('.snip-card').attributes('style')).toContain('--snip-fs: 32px')
      wrapper.unmount()
      localStorage.setItem('hanxi.snipcard.fs', 'abc')
      const w2 = await mountView()
      expect(w2.find('.snip-card').attributes('style')).toContain('--snip-fs: 14px') // 坏值回落默认
      w2.unmount()
    })
  })
})
