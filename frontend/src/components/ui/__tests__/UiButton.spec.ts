// UiButton：.btn 原子家族包装的类名映射与 attrs 透传契约。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiButton from '../UiButton.vue'

describe('UiButton', () => {
  it('默认渲染为次要样式原生 button', () => {
    const w = mount(UiButton, { slots: { default: '执行' } })
    const btn = w.find('button')
    expect(btn.classes()).toContain('btn')
    expect(btn.classes()).toContain('btn-secondary')
    expect(btn.attributes('type')).toBe('button')
    expect(btn.text()).toBe('执行')
  })

  it('variant 映射原子类，danger 转描边红', () => {
    expect(mount(UiButton, { props: { variant: 'primary' } }).find('button').classes()).toContain('btn-primary')
    expect(mount(UiButton, { props: { variant: 'ghost' } }).find('button').classes()).toContain('btn-ghost')
    expect(mount(UiButton, { props: { variant: 'danger' } }).find('button').classes()).toContain('btn-danger-outline')
  })

  it('small/block 尺寸钩子', () => {
    const w = mount(UiButton, { props: { small: true, block: true } })
    expect(w.find('button').classes()).toContain('btn-small')
    expect(w.find('button').classes()).toContain('ui-btn-block')
  })

  it('disabled 等 attrs 透传到底钮', () => {
    const w = mount(UiButton, { attrs: { disabled: true, title: '提示' } })
    expect(w.find('button').attributes('disabled')).toBeDefined()
    expect(w.find('button').attributes('title')).toBe('提示')
  })
})
