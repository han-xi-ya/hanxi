// ManagedVersionDot 契约测试（波 0）：锁定托管家族状态点标准形的 DOM 结构——
// span.ver-status 基形 + 状态档位类 + 消费方词面直传（组件不推断各态文案）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ManagedVersionDot from '../ManagedVersionDot.vue'

describe('ManagedVersionDot', () => {
  it.each([
    ['installed', '已安装'],
    ['downloading', '安装中'],
    ['error', '失败'],
    ['idle', '可安装'],
  ] as const)('%s 态：ver-status 基形 + 档位类 + 词面透传', (status, text) => {
    const w = mount(ManagedVersionDot, { props: { status, text } })
    const el = w.find('span')
    expect(el.classes()).toEqual(['ver-status', status])
    expect(el.text()).toBe(text)
  })

  it('词面由消费方决定：同态不同词（已装/已安装）如实呈现', () => {
    const w = mount(ManagedVersionDot, { props: { status: 'installed', text: '已装' } })
    expect(w.find('span').classes()).toContain('installed')
    expect(w.find('span').text()).toBe('已装')
  })
})
