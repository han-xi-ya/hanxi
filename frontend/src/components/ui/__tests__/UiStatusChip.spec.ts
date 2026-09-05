// UiStatusChip：tone → .chip-{tone} 类名映射（五值全举防漂移）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiStatusChip from '../UiStatusChip.vue'

describe('UiStatusChip', () => {
  const tones = ['positive', 'information', 'warning', 'danger', 'neutral'] as const
  for (const tone of tones) {
    it(`tone=${tone} 挂对应原子类`, () => {
      const w = mount(UiStatusChip, { props: { tone }, slots: { default: '状态' } })
      const span = w.find('span')
      expect(span.classes()).toEqual(['chip', `chip-${tone}`])
      expect(span.text()).toBe('状态')
    })
  }
})
