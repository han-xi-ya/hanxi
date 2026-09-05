// PageHeader：标题/副标题/操作区的条件渲染契约。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PageHeader from '../PageHeader.vue'

describe('PageHeader', () => {
  it('必有 h1 标题，复用 .header-row 原子', () => {
    const w = mount(PageHeader, { props: { title: '端口查杀' } })
    expect(w.classes()).toContain('header-row')
    expect(w.find('h1').text()).toBe('端口查杀')
    expect(w.find('p.subtitle').exists()).toBe(false)
  })

  it('subtitle 存在时渲染说明行', () => {
    const w = mount(PageHeader, { props: { title: 'T', subtitle: '说明文案' } })
    expect(w.find('p.subtitle').text()).toBe('说明文案')
  })

  it('actions slot 落在标题右侧区', async () => {
    const w = mount(PageHeader, {
      props: { title: 'T' },
      slots: { actions: '<button class="op">扫描</button>' },
    })
    expect(w.find('.header-row > .op').exists()).toBe(true)
  })
})
