// PageContainer：变体 → 宽度原子类映射、默认档与 fluid 满宽例外契约。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PageContainer from '../PageContainer.vue'

describe('PageContainer', () => {
  it('默认为 standard 档，挂 .page 全局原子', () => {
    const w = mount(PageContainer)
    expect(w.element.tagName).toBe('DIV')
    expect(w.classes()).toEqual(['page'])
  })

  it('variant 映射到对应宽度原子类', () => {
    expect(mount(PageContainer, { props: { variant: 'workbench' } }).classes()).toEqual([
      'page-workbench',
    ])
    expect(mount(PageContainer, { props: { variant: 'wide' } }).classes()).toEqual([
      'page-wide',
    ])
  })

  it('fluid 命中时替换变体档为 .page-fluid 满宽例外', () => {
    const w = mount(PageContainer, { props: { variant: 'wide', fluid: true } })
    expect(w.classes()).toEqual(['page-fluid'])
  })

  it('slot 内容原样落在容器 div 内，不注入额外包裹', () => {
    const w = mount(PageContainer, {
      props: { variant: 'workbench' },
      slots: { default: '<h2 class="inner">内容</h2>' },
    })
    expect(w.find('div.page-workbench > h2.inner').exists()).toBe(true)
    expect(w.element.children).toHaveLength(1)
  })
})
