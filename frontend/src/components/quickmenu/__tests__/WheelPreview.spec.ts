// 轮盘实时预览（WheelPreview）特征测试：几何同源扇区数、分组角标、点格 emit
// pick、activeIndex 高亮、空态。预览与实盘共享 wheelGeometry，测试只锁预览层。
// 皮肤批：skin prop 缺省兼容（不传 = 默认素瓷）与预设类下发。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { MenuItem } from '../../../../bindings/hanxi/internal/modules/quickmenu/models'
import WheelPreview from '../WheelPreview.vue'
import { DEFAULT_WHEEL_SKIN } from '../wheelSkin'

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

  it('skin 缺省 = 默认素瓷皮肤，根带 skin-frost 类与 --wf-face-a 变量', () => {
    const w = mount(WheelPreview, { props: { items: three } })
    expect(w.find('.wp').classes()).toContain('skin-frost')
    expect(w.find('.wp').attributes('style')).toContain('--wf-face-a: 1')
    w.unmount()
  })

  it('v3 观感零变化锁：缺省 viewBox "0 0 512 512"；trim viewBox "82 82 348 348"', () => {
    const full = mount(WheelPreview, { props: { items: three } })
    expect(full.find('.wp-svg').attributes('viewBox')).toBe('0 0 512 512')
    full.unmount()
    const trim = mount(WheelPreview, { props: { items: three, trim: true } })
    expect(trim.find('.wp-svg').attributes('viewBox')).toBe('82 82 348 348')
    trim.unmount()
  })

  it('trim 槽位锚点 = 全窗锚点按 [82,430]→[0,100]% 折算（v3 手量算式互证）', () => {
    const four: MenuItem[] = [leaf(0, 'A', 'exe'), leaf(1, 'B', 'exe'), leaf(2, 'C', 'exe'), leaf(3, 'D', 'exe')]
    const w = mount(WheelPreview, { props: { items: four, trim: true } })
    // 第 0 枚（45°方向，扇区带中线半径 118）：全窗 x = 256 + 118/√2
    const toWindow = (px: number) => ((px - 82) / (512 - 82 * 2)) * 100
    const x = 256 + 118 / Math.SQRT2
    const y = 256 - 118 / Math.SQRT2
    const style = w.findAll('.wp-slot')[0].attributes('style') ?? ''
    expect(parseFloat(/left:\s*([\d.]+)%/.exec(style)![1])).toBeCloseTo(toWindow(x), 4)
    expect(parseFloat(/top:\s*([\d.]+)%/.exec(style)![1])).toBeCloseTo(toWindow(y), 4)
    w.unmount()
  })

  it('传 skin 切预设与强度：根类名换、CSS 变量随 stroke 下发、扇区带类型色类', () => {
    const w = mount(WheelPreview, {
      props: { items: three, skin: { ...DEFAULT_WHEEL_SKIN, preset: 'ink', stroke: 0, faceAlpha: 0.5 } },
    })
    const root = w.find('.wp')
    expect(root.classes()).toContain('skin-ink')
    expect(root.attributes('style')).toContain('--wf-face-a: 0.5')
    expect(root.attributes('style')).toContain('--wf-edge: 10%')
    // 类型色轨：exe→t-exe（叶图标解析为 box 属矢量轨，不吃像素预算但仍带类型类）
    expect(w.findAll('.wp-sector')[0].classes()).toContain('t-exe')
    w.unmount()
  })
})
