// 特征测试：ExtPlaceholderView——占位状态容器契约（标题回退、建设中说明、
// 图标 + 「返回模块中心」下一步动作经 @navigate 上抛 /modules）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ExtPlaceholderView from '../ExtPlaceholderView.vue'

describe('ExtPlaceholderView', () => {
  it('传入 title 时渲染该标题，状态容器含建设中说明与图标', () => {
    const w = mount(ExtPlaceholderView, { props: { title: '某扩展' } })
    expect(w.find('h1').text()).toBe('某扩展')
    expect(w.text()).toContain('该模块前端视图尚未建档')
    expect(w.text()).toContain('此扩展页面正在建设中')
    // 状态容器走全局空态原子 + 装饰性图标（非裸标题）
    expect(w.find('.empty-state').exists()).toBe(true)
    expect(w.find('.empty-state svg.app-icon').exists()).toBe(true)
  })

  it('缺省 title 回退「扩展」', () => {
    const w = mount(ExtPlaceholderView)
    expect(w.find('h1').text()).toBe('扩展')
  })

  it('「返回模块中心」钮经 navigate 上抛 /modules', async () => {
    const w = mount(ExtPlaceholderView, { props: { title: '某扩展' } })
    const btn = w.findAll('button').find(b => b.text() === '返回模块中心')!
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(w.emitted('navigate')?.[0]).toEqual(['/modules'])
  })
})