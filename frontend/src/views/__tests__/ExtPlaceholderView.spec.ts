// 组 G1 特征测试：ExtPlaceholderView——最小占位契约（标题回退与建设中提示）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ExtPlaceholderView from '../ExtPlaceholderView.vue'

describe('ExtPlaceholderView', () => {
  it('传入 title 时渲染该标题', () => {
    const w = mount(ExtPlaceholderView, { props: { title: '某扩展' } })
    expect(w.find('h1').text()).toBe('某扩展')
    expect(w.text()).toContain('此扩展页面正在建设中')
  })

  it('缺省 title 回退「扩展」', () => {
    const w = mount(ExtPlaceholderView)
    expect(w.find('h1').text()).toBe('扩展')
  })
})
