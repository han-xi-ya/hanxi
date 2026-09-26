// 悬浮速记卡（#memosheet 独立窗视图）行为规格（N16 B 批 + 牌面重做批）：
// 回车即存走既有 Create 链路（空正文标题、blue 配色、标签自动补 #）、保存成功
// 清行不关窗（连续速记）、空行回车 = 收起、Esc = 收起、输入法组字中的回车豁免、
// opening 事件清残稿、失败保稿给卡内轻提示（独立窗无工作台 toast 宿主）。
// 重做批追加：Ctrl+Enter 存并收起（失败不收）、飞走残影与 is-enter 入场钩子、
// opening 清残影、提示行常驻占位（错误小字不闪大字）、多行粘贴并条计数提示。
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

describe('QuickMemoSheet 牌面重做批（动效触发与快捷件）', () => {
  it('提示行常驻占位：闲置即空槽存在，出现不顶动版面；错误走 tip-err 小字色调档', async () => {
    const w = mountSheet()
    await flushMicrotasks()
    const tip = w.find('.sheet-tip')
    expect(tip.exists()).toBe(true) // v-if 撤了：槽位常开是防跳动的前提
    expect(tip.text()).toBe('')
    expect(tip.classes()).not.toContain('tip-err')
    expect(tip.attributes('role')).toBe('status')
    w.unmount()

    svc.Create.mockRejectedValue(new Error('x'))
    const w2 = mountSheet()
    await flushMicrotasks()
    await w2.find('.sheet-input').setValue('一行')
    await w2.find('.sheet-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(w2.find('.sheet-tip').classes()).toContain('tip-err')
    w2.unmount()
  })

  it('Ctrl+Enter 存并收起：Create 成功才 HideQuickSheet，成功路径不再演残影', async () => {
    svc.Create.mockResolvedValue({ id: 'm1' })
    svc.HideQuickSheet.mockResolvedValue(undefined)
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').setValue('速完就走')
    await w.find('.sheet-tags').setValue('fast')
    await w.find('.sheet-input').trigger('keydown.enter.ctrl')
    await flushMicrotasks()
    await nextTick()
    expect(svc.Create).toHaveBeenCalledWith('', '速完就走', ['#fast'], 'blue')
    expect(svc.HideQuickSheet).toHaveBeenCalledTimes(1)
    expect(w.find('.sheet-ghost').exists()).toBe(false)
    w.unmount()
  })

  it('Ctrl+Enter 保存失败：保稿保窗不收起（绝不吞数据）', async () => {
    svc.Create.mockRejectedValue(new Error('db locked'))
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').setValue('要紧一行')
    await w.find('.sheet-input').trigger('keydown.enter.ctrl')
    await flushMicrotasks()
    expect((w.find('.sheet-input').element as HTMLInputElement).value).toBe('要紧一行')
    expect(svc.HideQuickSheet).not.toHaveBeenCalled()
    expect(w.find('.sheet-tip').text()).toContain('保存失败')
    w.unmount()
  })

  it('空行 Ctrl+Enter 同样 = 收起（没内容可存，与空行回车同义）', async () => {
    svc.HideQuickSheet.mockResolvedValue(undefined)
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').trigger('keydown.enter.ctrl')
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    expect(svc.HideQuickSheet).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('保存成功：原句化作飞走残影，动画窗口后自散；再存一条遇 opening 按残稿一并清', async () => {
    svc.Create.mockResolvedValue({ id: 'm1' })
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').setValue('curl ifconfig.me')
    await w.find('.sheet-input').trigger('keydown.enter')
    await flushMicrotasks()
    await nextTick()
    expect(w.find('.sheet-ghost').text()).toBe('curl ifconfig.me')
    expect(w.find('.sheet-ghost').attributes('aria-hidden')).toBe('true') // 纯装饰不进朗读序列
    await new Promise((r) => setTimeout(r, 320))
    await nextTick()
    expect(w.find('.sheet-ghost').exists()).toBe(false)

    await w.find('.sheet-input').setValue('第二条还在飞')
    await w.find('.sheet-input').trigger('keydown.enter')
    await flushMicrotasks()
    await nextTick()
    expect(w.find('.sheet-ghost').exists()).toBe(true)
    runtime.handlers['memo:quicksheet:opening']({})
    await nextTick()
    expect(w.find('.sheet-ghost').exists()).toBe(false) // 残影也是残稿
    w.unmount()
  })

  it('opening 重播入场钩子 is-enter（动画由唤出事件驱动，挂载首窗还藏着不白烧）', async () => {
    const w = mountSheet()
    await flushMicrotasks()
    await nextTick()
    expect(w.find('.sheet-card').classes()).not.toContain('is-enter')
    runtime.handlers['memo:quicksheet:opening']({})
    await flushMicrotasks()
    await nextTick()
    expect(w.find('.sheet-card').classes()).toContain('is-enter')
    await new Promise((r) => setTimeout(r, 320))
    await nextTick()
    expect(w.find('.sheet-card').classes()).not.toContain('is-enter')
    w.unmount()
  })

  it('粘贴多行：自动并成一条并轻提示行数；单行粘贴零干预不吭声', async () => {
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').trigger('paste', {
      clipboardData: { getData: (fmt: string) => (fmt === 'text/plain' ? '行一\n行二\n \n行三' : '') },
    })
    await nextTick()
    expect((w.find('.sheet-input').element as HTMLInputElement).value).toBe('行一 行二 行三')
    expect(w.find('.sheet-tip').text()).toContain('3 行')
    expect(w.find('.sheet-tip').classes()).toContain('tip-ok')
    w.unmount()

    const w2 = mountSheet()
    await flushMicrotasks()
    await w2.find('.sheet-input').trigger('paste', {
      clipboardData: { getData: () => '本就一行' },
    })
    await nextTick()
    expect(w2.find('.sheet-tip').text()).toBe('') // 放行原生插入路径
    w2.unmount()
  })

  it('粘贴多行不清空已有草稿（并到尾部，回车一并入库）', async () => {
    const w = mountSheet()
    await flushMicrotasks()
    await w.find('.sheet-input').setValue('打头草稿')
    await w.find('.sheet-input').trigger('paste', {
      clipboardData: { getData: () => '甲\n乙' },
    })
    await nextTick()
    expect((w.find('.sheet-input').element as HTMLInputElement).value).toBe('打头草稿 甲 乙')
    w.unmount()
  })
})
