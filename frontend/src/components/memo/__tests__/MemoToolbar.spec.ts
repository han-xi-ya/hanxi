// MemoToolbar 独立契约测试（容器件、零后端桩）：
//   ① 搜索框受控回显 + 每次 input 即时发 update:search（防抖外置的反证——
//      连续两次输入立刻得到两个事件，组件内不存在任何定时器）；
//   ② 「更多」溢出菜单开合纪律：$slots.more 缺席不出触发钮、aria-expanded
//      同步、菜单内点击自动收、Esc 收、点外收、卸载摘净 document/window 监听；
//   ③ 三个插槽位（filters/tagfilter/more）透传渲染；
//   ④ 轻工具条结构锁：本体不挂 .panel（无大卡片容器感是契约不是玄学）。
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import MemoToolbar from '../MemoToolbar.vue'

const bar = (props: Record<string, unknown> = {}, slots: Record<string, string> = {}) =>
  mount(MemoToolbar, { props, slots })

describe('MemoToolbar 搜索框（防抖外置）', () => {
  it('input 即时透传原值：连续两次输入立刻得到两个 update:search', async () => {
    const w = bar()
    const input = w.find('input')
    await input.setValue('gu')
    await input.setValue('git')
    expect(w.emitted('update:search')).toEqual([['gu'], ['git']])
  })

  it('受控回显：value 跟 prop 走，组件不自持状态', () => {
    const w = bar({ search: '已过滤词' })
    expect(w.find('input').element.value).toBe('已过滤词')
  })

  it('清空钮仅有词时在场，点击发空串', async () => {
    const empty = bar()
    expect(empty.find('.mt-clear').exists()).toBe(false)
    const w = bar({ search: 'x' })
    await w.find('.mt-clear').trigger('click')
    expect(w.emitted('update:search')).toEqual([['']])
  })

  it('占位与无障碍名可覆盖（默认「搜索便签」）', () => {
    const w = bar({ placeholder: '搜点别的' })
    expect(w.find('input').attributes('placeholder')).toBe('搜点别的')
    expect(w.find('input').attributes('aria-label')).toBe('搜索便签')
  })
})

describe('MemoToolbar 插槽位', () => {
  it('filters 与 tagfilter 内容原位渲染（标签弹层槽自带定位锚点）', () => {
    const w = bar({}, {
      filters: '<span class="t-pin-chip">📌 只看置顶</span>',
      tagfilter: '<span class="t-pop">标签弹层</span>',
    })
    expect(w.find('.t-pin-chip').exists()).toBe(true)
    expect(w.find('.mt-tag-slot .t-pop').exists()).toBe(true)
  })

  it('more 插槽缺席时「更多」触发钮整个不出场', () => {
    expect(bar().find('.mt-more-btn').exists()).toBe(false)
    expect(bar({}, { more: '<button class="t-item">导出全库</button>' }).find('.mt-more-btn').exists()).toBe(true)
  })
})

describe('MemoToolbar 「更多」溢出菜单开合', () => {
  const withMore = () =>
    bar({}, { more: '<button type="button" class="t-item">一键全删</button>' })

  it('点击触发钮展开菜单，aria-expanded 随开合翻转', async () => {
    const w = withMore()
    const btn = w.find('.mt-more-btn')
    expect(btn.attributes('aria-expanded')).toBe('false')
    expect(w.find('.mt-menu').exists()).toBe(false)
    await btn.trigger('click')
    expect(w.find('.mt-menu .t-item').exists()).toBe(true)
    expect(w.find('.mt-more-btn').attributes('aria-expanded')).toBe('true')
  })

  it('菜单内点击（宿主按钮自己的 click 之后）自动收菜单', async () => {
    const w = withMore()
    await w.find('.mt-more-btn').trigger('click')
    await w.find('.t-item').trigger('click')
    expect(w.find('.mt-menu').exists()).toBe(false)
  })

  it('再点一次触发钮收起（toggle 语义）', async () => {
    const w = withMore()
    const btn = () => w.find('.mt-more-btn')
    await btn().trigger('click')
    await btn().trigger('click')
    expect(w.find('.mt-menu').exists()).toBe(false)
  })

  it('Esc 收起；收起后再按不再响应（监听摘净）', async () => {
    const w = withMore()
    await w.find('.mt-more-btn').trigger('click')
    // 原生派发不过 VTU 包装：状态同步改完，DOM 收口要等一个渲染 tick
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(w.find('.mt-menu').exists()).toBe(false)
    expect(() =>
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })),
    ).not.toThrow()
  })

  it('点条外区域收起、条内空白不误收', async () => {
    const w = withMore()
    await w.find('.mt-more-btn').trigger('click')
    document.dispatchEvent(new MouseEvent('click'))
    await nextTick()
    expect(w.find('.mt-menu').exists()).toBe(false)
  })

  it('条内其它区域点击不误收（点外判定以容器边界为界）', async () => {
    const w = withMore()
    await w.find('.mt-more-btn').trigger('click')
    await w.find('.mt-search').trigger('click')
    expect(w.find('.mt-menu').exists()).toBe(true)
  })

  it('菜单开着卸载：监听摘净不留孤儿（后续全局事件不炸）', async () => {
    const w = withMore()
    await w.find('.mt-more-btn').trigger('click')
    w.unmount()
    expect(() => {
      document.dispatchEvent(new MouseEvent('click'))
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    }).not.toThrow()
  })
})

describe('MemoToolbar 轻容器结构锁', () => {
  it('本体不挂 .panel/.card——"条"不是"卡"', () => {
    const w = bar()
    expect(w.classes()).not.toContain('panel')
    expect(w.classes()).not.toContain('card')
  })
})
