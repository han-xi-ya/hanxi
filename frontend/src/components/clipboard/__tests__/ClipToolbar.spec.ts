// ClipToolbar 独立契约测试（轻工具条容器件、零后端桩、零定时器）：
//   ① 搜索框受控回显 + input 即时透传（防抖外置的反证——组件内不存在任何
//      setTimeout）；回车单发 search-enter，输入法组字中的回车豁免；
//   ② kind chips 五档受控点亮，点击只发档位值不产生任何过滤行为；
//   ③ 暂停开关：文字双通道表意（记录中/已暂停/切换中…）、aria-pressed、
//      pausing 禁用；
//   ④ 新片段/清空只发意图事件；清空禁用位受控；
//   ⑤ 结构锁：本体不挂 .panel/.card（"条"不是"卡"，MemoToolbar 同谱）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ClipToolbar from '../ClipToolbar.vue'

const bar = (props: Record<string, unknown> = {}) => mount(ClipToolbar, { props })
const chipByText = (w: ReturnType<typeof bar>, text: string) =>
  w.findAll('.ct-chip').find((c) => c.text().includes(text))

describe('搜索框（防抖外置）', () => {
  it('input 即时透传原值：连续两次输入立刻得到两个 update:search', async () => {
    const w = bar()
    const input = w.find('.ct-search-input')
    await input.setValue('gi')
    await input.setValue('git')
    expect(w.emitted('update:search')).toEqual([['gi'], ['git']])
  })

  it('受控回显 + 清空钮仅有词在场，点击发空串', async () => {
    expect(bar().find('.ct-clear').exists()).toBe(false)
    const w = bar({ search: 'x' })
    expect((w.find('.ct-search-input').element as HTMLInputElement).value).toBe('x')
    await w.find('.ct-clear').trigger('click')
    expect(w.emitted('update:search')).toEqual([['']])
  })

  it('裸回车发 search-enter；输入法组字中的回车豁免', async () => {
    const w = bar()
    await w.find('.ct-search-input').trigger('keydown', { key: 'Enter' })
    expect(w.emitted('search-enter')).toHaveLength(1)
    const el = w.find('.ct-search-input').element
    const ev = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
    Object.defineProperty(ev, 'isComposing', { value: true })
    el.dispatchEvent(ev)
    expect(w.emitted('search-enter')).toHaveLength(1) // 组字回车不追加
  })
})

describe('kind 过滤 chips', () => {
  it('五档齐备（全部/文本/图片/文件/片段），当前档 aria-pressed 且高亮', () => {
    const w = bar({ kind: 'manual' })
    expect(w.findAll('.ct-chip')).toHaveLength(5)
    expect(chipByText(w, '片段')!.classes()).toContain('active')
    expect(chipByText(w, '片段')!.attributes('aria-pressed')).toBe('true')
    expect(chipByText(w, '全部')!.attributes('aria-pressed')).toBe('false')
  })

  it('点击某档只发 update:kind 档位值（过滤逻辑不归组件）', async () => {
    const w = bar()
    await chipByText(w, '图片')!.trigger('click')
    await chipByText(w, '片段')!.trigger('click')
    expect(w.emitted('update:kind')).toEqual([['image'], ['manual']])
  })
})

describe('暂停开关', () => {
  it('常态：绿点活体脉冲 + 「记录中」文字（不用颜色单表意）', () => {
    const w = bar()
    const btn = w.find('.ct-pause')
    expect(btn.text()).toContain('记录中')
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(w.find('.ct-dot-live').classes()).toContain('live-pulse')
  })

  it('暂停态：文字翻「已暂停记录」、点不脉冲；pausing 在途禁用并显示切换中', () => {
    const w = bar({ paused: true })
    expect(w.find('.ct-pause').text()).toContain('已暂停记录')
    expect(w.find('.ct-pause').attributes('aria-pressed')).toBe('true')
    expect(w.find('.ct-dot-paused').exists()).toBe(true)
    const w2 = bar({ paused: true, pausing: true })
    expect((w2.find('.ct-pause').element as HTMLButtonElement).disabled).toBe(true)
    expect(w2.find('.ct-pause').text()).toContain('切换中')
  })

  it('点击只发 toggle-paused 意图（SetPaused 调用与错误播报归视图）', async () => {
    const w = bar()
    await w.find('.ct-pause').trigger('click')
    expect(w.emitted('toggle-paused')).toHaveLength(1)
  })
})

describe('新片段 / 清空与结构锁', () => {
  it('两钮各发意图事件；clearDisabled 受控禁用', async () => {
    const w = bar({ clearDisabled: true })
    await w.find('.ct-create').trigger('click')
    expect(w.emitted('create-snippet')).toHaveLength(1)
    expect((w.find('.ct-clear-all').element as HTMLButtonElement).disabled).toBe(true)
    const w2 = bar({ clearDisabled: false })
    await w2.find('.ct-clear-all').trigger('click')
    expect(w2.emitted('clear-all')).toHaveLength(1)
  })

  it('本体不挂 .panel/.card——"条"不是"卡"', () => {
    expect(bar().classes()).not.toContain('panel')
    expect(bar().classes()).not.toContain('card')
  })
})
