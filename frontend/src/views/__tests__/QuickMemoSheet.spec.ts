// 悬浮速记卡（#memosheet 独立窗视图）行为规格（N16 B 批）：
// 回车即存走既有 Create 链路（空正文标题、blue 配色、标签自动补 #）、保存成功
// 清行不关窗（连续速记）、空行回车 = 收起、Esc = 收起、输入法组字中的回车豁免、
// opening 事件清残稿、失败保稿给卡内轻提示（独立窗无工作台 toast 宿主）。
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuickMemoSheet from '../QuickMemoSheet.vue'

const svc = vi.hoisted(() => ({
  Create: vi.fn(),
  HideQuickSheet: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data?: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data?: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/memo', () => ({
  MemoService: svc,
}))

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function mountSheet() {
  return mount(defineComponent({ render: () => h(QuickMemoSheet) }), { attachTo: document.body })
}

afterEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

describe('QuickMemoSheet 速记动线', () => {
  it('回车即存：Create 收口（空标题/blue 配色/标签补 #），成功后清行不关窗并轻提示', async () => {
    svc.Create.mockResolvedValue({ id: 'm1' })
    const w = mountSheet()
    await flushMicrotasks()

    await w.find('.sheet-input').setValue('  grep -rn foo  ')
    await w.find('.sheet-tags').setValue('shell grep')
    await w.find('.sheet-input').trigger('keydown.enter')
    await flushMicrotasks()
    await nextTick()

    expect(svc.Create).toHaveBeenCalledWith('', 'grep -rn foo', ['#shell', '#grep'], 'blue')
    expect(svc.HideQuickSheet).not.toHaveBeenCalled()
    expect((w.find('.sheet-input').element as HTMLInputElement).value).toBe('')
    expect(w.find('.sheet-tip').text()).toContain('已记下')
    w.unmount()
  })

  it('输入法组字中的回车只结束选词，不触发保存', async () => {
    const w = mountSheet()
    await flushMicrotasks()
    const inputWrapper = w.find('.sheet-input')
    await inputWrapper.setValue('还在组字')
    const ev = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
    Object.defineProperty(ev, 'isComposing', { value: true })
    inputWrapper.element.dispatchEvent(ev)
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    w.unmount()
  })

  it('空正文回车 = 收起（没什么可记时就关卡）', async () => {
    svc.HideQuickSheet.mockResolvedValue(undefined)
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    expect(svc.HideQuickSheet).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('Esc 收起；HideQuickSheet 报错时静默（Go 侧出口照旧）', async () => {
    svc.HideQuickSheet.mockResolvedValue(undefined)
    const w = mountSheet()
    await flushMicrotasks()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushMicrotasks()
    expect(svc.HideQuickSheet).toHaveBeenCalledTimes(1)
    w.unmount()

    const w2 = mountSheet()
    await flushMicrotasks()
    svc.HideQuickSheet.mockRejectedValue(new Error('gate refused'))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushMicrotasks()
    expect(w2.find('.sheet').exists()).toBe(true) // 不收页面自己——收不收由 Go 裁决
    w2.unmount()
  })

  it('opening 事件清残稿与轻提示（每次唤出都是新速记）', async () => {
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').setValue('上次没存的残稿')
    await w.find('.sheet-tags').setValue('#draft')
    expect(runtime.handlers['memo:quicksheet:opening']).toBeDefined()
    runtime.handlers['memo:quicksheet:opening']({})
    await flushMicrotasks()
    await nextTick()
    expect((w.find('.sheet-input').element as HTMLInputElement).value).toBe('')
    expect((w.find('.sheet-tags').element as HTMLInputElement).value).toBe('')
    w.unmount()
  })

  it('Markdown 结构输入给 MD 轻提示（复用 utils/markdown 判定，仅提示不改数据）', async () => {
    const w = mountSheet()
    await flushMicrotasks()
    expect(w.find('.sheet-md').exists()).toBe(false)
    await w.find('.sheet-input').setValue('## 标题与列表')
    await nextTick()
    expect(w.find('.sheet-md').exists()).toBe(true)
    w.unmount()
  })

  it('保存失败保稿并卡内报错（修一修还能回车重试）', async () => {
    svc.Create.mockRejectedValue(new Error('disk full'))
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').setValue('重要一行')
    await w.find('.sheet-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect((w.find('.sheet-input').element as HTMLInputElement).value).toBe('重要一行')
    expect(w.find('.sheet-tip').text()).toContain('保存失败: disk full')
    w.unmount()
  })
})
