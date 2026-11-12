// SettingsModal 骨架契约测试：AiSection 向导与 SnapshotSection 预览共用此壳，
// 这里钉的是"壳"对两宿主承诺的外部可验证事实——三段插槽落位（head/body/actions）、
// ✕ 钮与遮罩两路 close 事件、卡内点击不冒泡成关闭（click.self 语义）、
// 宽度两档默认 640。文件头成文纪律「不含 Esc 关闭：两处宿主原本皆无该行为，
// 此处不添行为」——同样用行为断言钉死：Esc 键路静默，不产 close。
// 可访问性批新增钉死：role/aria-modal/tabindex/aria-label、开窗聚焦落根、
// Tab 环困窗（首尾回绕）、关窗焦点归还触发元素。
import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
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

// 本壳 mount 即挂 document 级 Tab 监听：所有用例统一 track，afterEach 兜底卸载，
// 防监听与探针节点串扰后续用例（幂等：已手动 unmount 的重复调用无副作用）
const mounted: VueWrapper[] = []
function track<T extends VueWrapper>(w: T): T {
  mounted.push(w)
  return w
}
afterEach(() => {
  while (mounted.length) mounted.pop()?.unmount()
  document.body.innerHTML = ''
})

describe('SettingsModal 骨架契约', () => {
  it('三段插槽各落各位：head 进 .modal-head、缺省进 .modal-body、actions 进 .modal-actions', () => {
    const w = track(mountModal())
    expect(w.find('.modal-head .s-head').exists()).toBe(true)
    expect(w.find('.modal-head .s-head').text()).toContain('版本 aaaabbbb')
    expect(w.find('.modal-body .s-body').exists()).toBe(true)
    expect(w.find('.modal-actions .s-action').exists()).toBe(true)
    // 宿主没给 actions 时动作条仍占位（骨架不缺段，宿主只管填）
    const bare = track(mount(SettingsModal, { slots: { head: '<i>h</i>' } }))
    expect(bare.find('.modal-actions').exists()).toBe(true)
    expect(bare.find('.modal-actions').text()).toBe('')
  })

  it('✕ 钮存在且经 aria-label 可达，点击发且只发一次 close', async () => {
    const w = track(mountModal())
    const close = w.find('.btn-close')
    expect(close.exists()).toBe(true)
    expect(close.text()).toBe('✕')
    expect(close.attributes('aria-label')).toBe('关闭')
    await close.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('点遮罩空白关、点卡片内部不关（click.self 语义：误点正文不得吞掉整个弹窗）', async () => {
    const w = track(mountModal())
    await w.find('.modal-card').trigger('click')
    expect(w.emitted('close')).toBeUndefined() // 卡内点击冒泡到遮罩也不该触发
    await w.find('.s-body').trigger('click')
    expect(w.emitted('close')).toBeUndefined()
    await w.find('.modal-backdrop').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('成文纪律：不添 Esc 关闭——Esc 键路（组件与 document 两处）静默不产 close', async () => {
    const w = track(mountModal())
    await w.find('.modal-backdrop').trigger('keydown.esc')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    window.dispatchEvent(new KeyboardEvent('keyup', { key: 'Escape' }))
    expect(w.emitted('close')).toBeUndefined()
  })

  it('对话框语义齐备：role=dialog、aria-modal、tabindex=-1，aria-label 缺省回落、title prop 覆盖', () => {
    const w = track(mountModal())
    const card = w.find('.modal-card')
    expect(card.attributes('role')).toBe('dialog')
    expect(card.attributes('aria-modal')).toBe('true')
    expect(card.attributes('tabindex')).toBe('-1')
    expect(card.attributes('aria-label')).toBe('设置弹窗')
    const named = track(mountModal({ title: '版本预览' }))
    expect(named.find('.modal-card').attributes('aria-label')).toBe('版本预览')
  })

  it('开窗聚焦落卡片根；关窗（卸载）焦点归还开窗前的元素', async () => {
    const probe = document.createElement('button')
    document.body.appendChild(probe)
    probe.focus()
    const w = track(mount(SettingsModal, { attachTo: document.body, slots: { head: '<h3>h</h3>' } }))
    expect(document.activeElement).toBe(w.find('.modal-card').element)
    w.unmount()
    expect(document.activeElement).toBe(probe)
    probe.remove()
  })

  it('Tab 环困窗：末位 Tab 回绕首位，首位 Shift+Tab 回绕末位（Esc 仍静默）', async () => {
    const w = track(mount(SettingsModal, {
      attachTo: document.body,
      slots: { head: '<h3>h</h3>', actions: '<button class="s-action">关闭</button>' },
    }))
    const close = w.find('.btn-close').element as HTMLElement
    const action = w.find('.s-action').element as HTMLElement
    action.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }))
    expect(document.activeElement).toBe(close)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true }))
    expect(document.activeElement).toBe(action)
    w.unmount()
  })

  it('宽度两档：缺省 640（向导档），显式 680 覆盖（预览档），max-width 92vw 兜底在样式层', () => {
    const wizard = track(mountModal())
    expect(wizard.find('.modal-card').attributes('style')).toContain('width: 640px')
    const preview = track(mountModal({ width: 680 }))
    expect(preview.find('.modal-card').attributes('style')).toContain('width: 680px')
  })
})
