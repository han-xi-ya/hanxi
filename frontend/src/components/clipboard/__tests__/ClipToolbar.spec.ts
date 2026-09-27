// ClipToolbar 独立契约测试（轻工具条容器件、零后端桩、零定时器）：
//   ① 搜索框受控回显 + input 即时透传（防抖外置的反证——组件内不存在任何
//      setTimeout）；回车单发 search-enter，输入法组字中的回车豁免；
//   ② kind chips 五档受控点亮，点击只发档位值不产生任何过滤行为；
//   ③ 暂停开关：文字双通道表意（记录中/已暂停/切换中…）、aria-pressed、
//      pausing 禁用；
//   ④ 新片段直发意图；清空收「更多」溢出菜单（危险动作离主动线一格）——
//      菜单开合纪律照 MemoToolbar 同谱：aria-expanded、点外收、Esc 收、
//      菜单内点击冒泡自动收；开合经 moreChange 如实外播（含卸载收合）；
//      clearDisabled 受控禁用菜单项，prompt/danger 确认链不归组件；
//   ⑤ 结构锁：本体不挂 .panel/.card（"条"不是"卡"，MemoToolbar 同谱）。
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import ClipToolbar from '../ClipToolbar.vue'

// 每案收摊卸载：溢出菜单的开合期会在 document/window 挂捕获监听，
// 不卸净则跨案串线（onBeforeUnmount 自摘是组件纪律，卸载即反证泄露防线）
const mounted: ReturnType<typeof mount>[] = []
const bar = (props: Record<string, unknown> = {}) => {
  const w = mount(ClipToolbar, { props })
  mounted.push(w)
  return w
}
afterEach(() => {
  // 容错双卸：某案已手工 unmount（验证卸载收合路径），再卸仅 Vue 侧告警不炸
  while (mounted.length) {
    const w = mounted.pop()
    try {
      w!.unmount()
    } catch {
      /* 已卸载 */
    }
  }
})
const chipByText = (w: ReturnType<typeof bar>, text: string) =>
  w.findAll('.ct-chip').find((c) => c.text().includes(text))
const clearItem = (w: ReturnType<typeof bar>) => w.find('.ct-menu-danger')

async function openMenu(w: ReturnType<typeof bar>) {
  await w.find('.ct-more-btn').trigger('click')
}

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

describe('新片段', () => {
  it('直发 create-snippet 意图（prompt 链归视图）', async () => {
    const w = bar()
    await w.find('.ct-create').trigger('click')
    expect(w.emitted('create-snippet')).toHaveLength(1)
  })
})

describe('「更多」溢出菜单与清空', () => {
  it('菜单起手收起；触发钮开合并播 moreChange(true)，aria-expanded 随动', async () => {
    const w = bar({ clearDisabled: false })
    expect(w.find('.ct-menu').exists()).toBe(false)
    expect(w.find('.ct-more-btn').attributes('aria-expanded')).toBe('false')
    await openMenu(w)
    expect(w.find('.ct-menu').exists()).toBe(true)
    expect(w.find('.ct-more-btn').attributes('aria-expanded')).toBe('true')
    expect(w.emitted('moreChange')).toEqual([[true]])
  })

  it('清空钮只在场于菜单内：点项发 clear-all 意图并冒泡自动收合（moreChange 收到开与收）', async () => {
    const w = bar({ clearDisabled: false })
    expect(w.text()).not.toContain('清空全部历史') // 未开菜单时危险动作不上屏
    await openMenu(w)
    await clearItem(w).trigger('click')
    expect(w.emitted('clear-all')).toHaveLength(1)
    expect(w.find('.ct-menu').exists()).toBe(false)
    expect(w.emitted('moreChange')).toEqual([[true], [false]])
  })

  it('clearDisabled 受控禁用菜单项（库空不给按）', async () => {
    const w = bar({ clearDisabled: true })
    await openMenu(w)
    expect((clearItem(w).element as HTMLButtonElement).disabled).toBe(true)
  })

  it('点外收：document click（捕获期）落条外即收合', async () => {
    const w = bar({ clearDisabled: false })
    await openMenu(w)
    document.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(w.find('.ct-menu').exists()).toBe(false)
    expect(w.emitted('moreChange')).toEqual([[true], [false]])
  })

  it('Esc 收菜单（自持），收合状态外播', async () => {
    const w = bar({ clearDisabled: false })
    await openMenu(w)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(w.find('.ct-menu').exists()).toBe(false)
    expect(w.emitted('moreChange')).toEqual([[true], [false]])
  })

  it('卸载即收合并播 moreChange(false)（监听不泄露）', async () => {
    const w = bar({ clearDisabled: false })
    await openMenu(w)
    expect(w.emitted('moreChange')).toEqual([[true]])
    w.unmount()
    // VTU 记录在卸载后只余当拍事件：开合两拍各自为证
    expect(w.emitted('moreChange')).toEqual([[false]])
  })
})

describe('结构锁', () => {
  it('本体不挂 .panel/.card——"条"不是"卡"', () => {
    expect(bar().classes()).not.toContain('panel')
    expect(bar().classes()).not.toContain('card')
  })
})
