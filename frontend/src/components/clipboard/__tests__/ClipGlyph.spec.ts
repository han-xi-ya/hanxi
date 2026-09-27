// ClipGlyph 独立契约测试：私有图标件的三件事——已知名出路径、未知名出空图不炸、
// 装饰性（aria-hidden）不外抛。皮肤基座（stroke=currentColor）随快照语义验证。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ClipGlyph from '../ClipGlyph.vue'

describe('ClipGlyph', () => {
  it('已知名按注册路径渲染（sticky 两条、pause 两条）', () => {
    expect(mount(ClipGlyph, { props: { name: 'sticky' } }).findAll('path')).toHaveLength(2)
    expect(mount(ClipGlyph, { props: { name: 'pause' } }).findAll('path')).toHaveLength(2)
    expect(mount(ClipGlyph, { props: { name: 'copy' } }).findAll('path')).toHaveLength(2)
  })

  it('未知名渲染空图不抛错（消费方 computed 兜底前的双保险）', () => {
    const w = mount(ClipGlyph, { props: { name: 'no-such' } })
    expect(w.findAll('path')).toHaveLength(0)
    expect(w.find('svg').exists()).toBe(true)
  })

  it('恒 aria-hidden + 尺寸 px 档', () => {
    const w = mount(ClipGlyph, { props: { name: 'text', size: 12 } })
    expect(w.attributes('aria-hidden')).toBe('true')
    expect(w.attributes('style')).toContain('width: 12px')
    expect(w.attributes('style')).toContain('height: 12px')
  })
})
