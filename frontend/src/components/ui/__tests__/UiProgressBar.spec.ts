// UiProgressBar：clamp/非法值归零 与 aria 进度契约。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiProgressBar from '../UiProgressBar.vue'

describe('UiProgressBar', () => {
  it('正常百分比映射宽度与 aria-valuenow', () => {
    const w = mount(UiProgressBar, { props: { percent: 50 } })
    expect(w.find('[role="progressbar"]').attributes('aria-valuenow')).toBe('50')
    expect(w.find('.ui-progress-inner').attributes('style')).toContain('width: 50%')
  })

  it('越界与非法值安全钳制（120→100 / -5→0 / NaN→0）', () => {
    expect(mount(UiProgressBar, { props: { percent: 120 } }).find('.ui-progress-inner').attributes('style')).toContain('width: 100%')
    expect(mount(UiProgressBar, { props: { percent: -5 } }).find('.ui-progress-inner').attributes('style')).toContain('width: 0%')
    const w = mount(UiProgressBar, { props: { percent: Number.NaN } })
    expect(w.find('[role="progressbar"]').attributes('aria-valuenow')).toBe('0')
  })

  it('静态 aria 边界固定 0..100', () => {
    const bar = mount(UiProgressBar, { props: { percent: 1 } }).find('[role="progressbar"]')
    expect(bar.attributes('aria-valuemin')).toBe('0')
    expect(bar.attributes('aria-valuemax')).toBe('100')
  })
})
