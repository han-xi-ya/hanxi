// SettingsModal 骨架契约测试：AiSection 向导与 SnapshotSection 预览共用此壳，
// 这里钉的是"壳"对两宿主承诺的外部可验证事实——三段插槽落位（head/body/actions）、
// ✕ 钮与遮罩两路 close 事件、卡内点击不冒泡成关闭（click.self 语义）、
// 宽度两档默认 640。文件头成文纪律「不含 Esc 关闭：两处宿主原本皆无该行为，
// 此处不添行为」——同样用行为断言钉死：Esc 键路静默，不产 close。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SettingsModal from '../SettingsModal.vue'

function mountModal(props: Record<string, unknown> = {}) {
  return mount(SettingsModal, {
    props,
    slots: {
      head: '<h3 class="s-head">版本 aaaabbbb · 09-17 14:30</h3>',
      default: '<p class="s-body">正文块</p>',
      actions: '<button class="s-action">关闭</button>',
    },
  })
}

describe('SettingsModal 骨架契约', () => {
  it('三段插槽各落各位：head 进 .modal-head、缺省进 .modal-body、actions 进 .modal-actions', () => {
    const w = mountModal()
    expect(w.find('.modal-head .s-head').exists()).toBe(true)
    expect(w.find('.modal-head .s-head').text()).toContain('版本 aaaabbbb')
    expect(w.find('.modal-body .s-body').exists()).toBe(true)
    expect(w.find('.modal-actions .s-action').exists()).toBe(true)
    // 宿主没给 actions 时动作条仍占位（骨架不缺段，宿主只管填）
    const bare = mount(SettingsModal, { slots: { head: '<i>h</i>' } })
    expect(bare.find('.modal-actions').exists()).toBe(true)
    expect(bare.find('.modal-actions').text()).toBe('')
  })

  it('✕ 钮存在且经 aria-label 可达，点击发且只发一次 close', async () => {
    const w = mountModal()
    const close = w.find('.btn-close')
    expect(close.exists()).toBe(true)
    expect(close.text()).toBe('✕')
    expect(close.attributes('aria-label')).toBe('关闭')
    await close.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('点遮罩空白关、点卡片内部不关（click.self 语义：误点正文不得吞掉整个弹窗）', async () => {
    const w = mountModal()
    await w.find('.modal-card').trigger('click')
    expect(w.emitted('close')).toBeUndefined() // 卡内点击冒泡到遮罩也不该触发
    await w.find('.s-body').trigger('click')
    expect(w.emitted('close')).toBeUndefined()
    await w.find('.modal-backdrop').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('成文纪律：不添 Esc 关闭——Esc 键路（组件与 document 两处）静默不产 close', async () => {
    const w = mountModal()
    await w.find('.modal-backdrop').trigger('keydown.esc')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    window.dispatchEvent(new KeyboardEvent('keyup', { key: 'Escape' }))
    expect(w.emitted('close')).toBeUndefined()
  })

  it('宽度两档：缺省 640（向导档），显式 680 覆盖（预览档），max-width 92vw 兜底在样式层', () => {
    const wizard = mountModal()
    expect(wizard.find('.modal-card').attributes('style')).toContain('width: 640px')
    const preview = mountModal({ width: 680 })
    expect(preview.find('.modal-card').attributes('style')).toContain('width: 680px')
  })
})
