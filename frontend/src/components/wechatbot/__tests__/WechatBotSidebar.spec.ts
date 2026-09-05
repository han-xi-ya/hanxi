// Phase 6 拆分新增：WechatBotSidebar 展示壳冒烟测试。
// 纯 props/emit 组件（无 bindings/事件依赖），账号卡片整体契约由
// views/__tests__/WechatBotView.spec.ts 经视图整体锁定，此处只补组件独立接线。
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import WechatBotSidebar from '../WechatBotSidebar.vue'
import type { WechatAccountState } from '../../../../bindings/hanxi/internal/modules/wechat/models'

// 组件经 useWechatBot 传递 import bindings/qrcode/@wailsio/runtime（仅取 getAvatarColor
// 纯函数），测试环境无 Wails 原生层，按 §8 测试 seam 约定一律打桩。
vi.mock('../../../../bindings/hanxi/internal/modules/wechat', () => ({ WechatService: {} }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn() } }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: () => vi.fn() } }))

const acc = (id: string, name: string, extra: Partial<WechatAccountState> = {}): WechatAccountState =>
  ({
    id, remarkName: name, ilinkUserId: `il-${id}`, targetUserId: '', baseUrl: '',
    isListening: false, contextToken: '', ...extra,
  }) as WechatAccountState

function mountSidebar(props: Record<string, unknown> = {}) {
  return mount(WechatBotSidebar, {
    props: { accounts: [acc('a1', 'A'), acc('a2', 'B', { isListening: true })], currentAccountId: 'a1', collapsed: false, ...props },
  })
}

describe('WechatBotSidebar', () => {
  it('卡片渲染：选中态与监听/建联徽章逐字契约', () => {
    const wrapper = mountSidebar()
    const cards = wrapper.findAll('.account-item-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].classes()).toContain('active')
    expect(cards[1].find('.token-status-badge').text()).toBe('待激活')
    expect(cards[1].find('.status-dot-badge').classes()).toContain('online')
  })

  it('悬浮操作组与头部按钮均按原语义 emit，不触碰业务', async () => {
    const wrapper = mountSidebar()
    await wrapper.findAll('.action-icon-btn')[0].trigger('click')
    expect(wrapper.emitted('toggle-listener')?.[0][0]).toMatchObject({ id: 'a1' })
    await wrapper.findAll('.action-icon-btn')[1].trigger('click')
    expect(wrapper.emitted('rename')?.[0][0]).toMatchObject({ id: 'a1' })
    await wrapper.findAll('.action-icon-btn.danger')[0].trigger('click')
    expect(wrapper.emitted('delete')).toHaveLength(1)
    await wrapper.find('.btn-bind-account').trigger('click')
    expect(wrapper.emitted('bind')).toHaveLength(1)
    await wrapper.find('.btn-toggle-sidebar').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
  })

  it('折叠态：标题隐藏、卡片 is-mini、悬浮组不渲染', () => {
    const wrapper = mountSidebar({ collapsed: true })
    expect(wrapper.find('.sidebar-title').exists()).toBe(false)
    expect(wrapper.findAll('.account-item-card')[0].classes()).toContain('is-mini')
    expect(wrapper.find('.hover-actions-group').exists()).toBe(false)
  })
})
