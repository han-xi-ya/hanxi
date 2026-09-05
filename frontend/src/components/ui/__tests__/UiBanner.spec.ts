// UiBanner：tone 映射与 role=note 无障碍锚点。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiBanner from '../UiBanner.vue'

describe('UiBanner', () => {
  const cases: Array<[{ tone: 'ok' | 'info' | 'warn' | 'error' }, string]> = [
    [{ tone: 'ok' }, 'banner-ok'],
    [{ tone: 'info' }, 'banner-info'],
    [{ tone: 'warn' }, 'banner-warn'],
    [{ tone: 'error' }, 'banner-error'],
  ]
  for (const [{ tone }, cls] of cases) {
    it(`tone=${tone} → .${cls}`, () => {
      const w = mount(UiBanner, { props: { tone }, slots: { default: '外部实例运行中' } })
      const div = w.find('div')
      expect(div.classes()).toContain('banner')
      expect(div.classes()).toContain(cls)
      expect(div.attributes('role')).toBe('note')
      expect(div.text()).toContain('外部实例运行中')
    })
  }
})
