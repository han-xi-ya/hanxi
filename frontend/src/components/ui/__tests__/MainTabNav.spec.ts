// MainTabNav：v-model 语义与 tab 无障碍状态。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MainTabNav from '../MainTabNav.vue'

const tabs = [
  { key: 'annotate', label: '标注开关' },
  { key: 'versions', label: '版本管理' },
]

describe('MainTabNav', () => {
  it('渲染全部 tab，当前项挂 active 与 aria-selected', () => {
    const w = mount(MainTabNav, { props: { tabs, modelValue: 'versions' } })
    const btns = w.findAll('[role="tab"]')
    expect(btns).toHaveLength(2)
    expect(btns[1].classes()).toContain('active')
    expect(btns[1].attributes('aria-selected')).toBe('true')
    expect(btns[0].attributes('aria-selected')).toBe('false')
  })

  it('点击未选中项 emit update:modelValue（v-model 收口）', async () => {
    const w = mount(MainTabNav, { props: { tabs, modelValue: 'annotate' } })
    await w.findAll('[role="tab"]')[1].trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['versions'])
  })

  it('容器具备 tablist 角色', () => {
    expect(mount(MainTabNav, { props: { tabs, modelValue: 'annotate' } }).find('[role="tablist"]').exists()).toBe(true)
  })
})
