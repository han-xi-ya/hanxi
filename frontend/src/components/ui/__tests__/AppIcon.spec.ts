// AppIcon 特征测试（§8 图标纪律基建，阶段1）：注册表完整性与无障碍语义。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppIcon from '../AppIcon.vue'
import { ICON_NAMES, ICON_PATHS, type IconName } from '../../../constants/icons'

describe('AppIcon 注册表', () => {
  it('每个图标名均有非空 path 列表，d 以合法命令字母开头', () => {
    for (const name of ICON_NAMES) {
      const paths = ICON_PATHS[name]
      expect(paths.length, name).toBeGreaterThan(0)
      for (const d of paths) expect(d, `${name}:${d.slice(0, 12)}`).toMatch(/^[MmLlHhVvAaCcSsQqTt]/)
    }
  })

  it('键集合与 ICON_NAMES 一致（防散写漏登记）', () => {
    expect(new Set(Object.keys(ICON_PATHS))).toEqual(new Set(ICON_NAMES))
  })
})

describe('AppIcon 渲染', () => {
  it('缺省为装饰性：aria-hidden 且 path 数与注册表一致', () => {
    const w = mount(AppIcon, { props: { name: 'bell' as IconName } })
    const svg = w.find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('role')).toBeUndefined()
    expect(svg.findAll('path')).toHaveLength(ICON_PATHS.bell.length)
  })

  it('传 label 升格为语义图标：role=img + aria-label，不再隐藏', () => {
    const w = mount(AppIcon, { props: { name: 'gear' as IconName, label: '设置' } })
    const svg = w.find('svg')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toBe('设置')
    expect(svg.attributes('aria-hidden')).toBeUndefined()
  })

  it('size 数字转 px，字符串原样', () => {
    expect(mount(AppIcon, { props: { name: 'sun' as IconName, size: 16 } }).find('svg').attributes('style')).toContain('width: 16px')
    expect(mount(AppIcon, { props: { name: 'sun' as IconName, size: '1.2em' } }).find('svg').attributes('style')).toContain('width: 1.2em')
  })

  it('stroke 走 currentColor（双主题自动随文字色）', () => {
    const svg = mount(AppIcon, { props: { name: 'moon' as IconName } }).find('svg')
    expect(svg.attributes('stroke')).toBe('currentColor')
    expect(svg.attributes('fill')).toBe('none')
  })
})
