// 轮盘实时预览（WheelPreview）特征测试：几何同源扇区数、分组角标、点格 emit
// pick、activeIndex 高亮、空态。预览与实盘共享 wheelGeometry，测试只锁预览层。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { MenuItem } from '../../../../bindings/hanxi/internal/modules/quickmenu/models'
import WheelPreview from '../WheelPreview.vue'

const leaf = (index: number, label: string, type: string, children: MenuItem[] | null = null): MenuItem =>
  ({ index, label, type, hint: '', icon: '', children })

const three: MenuItem[] = [leaf(0, 'A', 'exe'), leaf(1, 'B', 'command'), leaf(2, 'C', 'route')]
const withGroup: MenuItem[] = [leaf(0, 'A', 'exe'), leaf(1, '组', 'group', [leaf(0, 'x', 'exe')])]

describe('WheelPreview', () => {
  it('扇区数与条目数一致（主环 path + 槽位按钮各一枚）', () => {
    const w = mount(WheelPreview, { props: { items: three } })
    expect(w.findAll('.wp-sector')).toHaveLength(3)
    expect(w.findAll('.wp-slot')).toHaveLength(3)
    expect(w.findAll('.wp-name').map(n => n.text())).toEqual(['A', 'B', 'C'])
    w.unmount()
  })

  it('分组槽带 ▸ 角标，叶子槽不带', () => {
    const w = mount(WheelPreview, { props: { items: withGroup } })
    const carets = w.findAll('.wp-caret')
    expect(carets).toHaveLength(1)
    expect(carets[0].text()).toBe('▸')
    w.unmount()
  })

  it('点击扇区/槽位 emit pick(下标)，activeIndex 回灌高亮', async () => {
    const w = mount(WheelPreview, { props: { items: three, activeIndex: 1 } })
    expect(w.findAll('.wp-sector')[1].classes()).toContain('is-active')
    expect(w.findAll('.wp-slot')[1].classes()).toContain('is-active')
    await w.findAll('.wp-slot')[2].trigger('click')
    expect(w.emitted('pick')?.[0]).toEqual([2])
    w.unmount()
  })

  it('空列表：虚线幽灵环 + 占位文案，零扇区', () => {
    const w = mount(WheelPreview, { props: { items: [] } })
    expect(w.findAll('.wp-sector')).toHaveLength(0)
    expect(w.find('.wp-ghost').exists()).toBe(true)
    expect(w.find('.wp-empty').text()).toContain('还没有条目')
    w.unmount()
  })
})
