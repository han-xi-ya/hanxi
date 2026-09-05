// UiEmptyState：原子容器 + slot 透传。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiEmptyState from '../UiEmptyState.vue'

describe('UiEmptyState', () => {
  it('渲染 .empty-state 并承载任意 slot 内容', () => {
    const w = mount(UiEmptyState, { slots: { default: '<p class="tip">还没有安装任何版本</p>' } })
    expect(w.find('.empty-state').exists()).toBe(true)
    expect(w.find('.tip').text()).toContain('还没有安装任何版本')
  })
})
