// 共享历史弹窗壳特征测试（N20 收尾）：锁定三条契约——Esc 在确认框在场时让位、
// 遮罩/关闭钮上抛 close、开窗焦点入 dialog 且关窗回位。类名/层阶是迁移关键。
import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import UiHistoryDialog from '../UiHistoryDialog.vue'
import { useConfirm } from '../../../composables/useConfirm'

vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => () => {} },
  Call: { ByID: () => Promise.resolve(undefined) },
}))

function factory(open = false) {
  return mount(UiHistoryDialog, {
    props: { open, title: '查杀历史', note: '最多保留最近 200 条' },
    slots: { default: '<div class="probe">面板占位</div>' },
    global: { stubs: { teleport: true } },
    attachTo: document.body,
  })
}

afterEach(() => document.body.innerHTML = '')

describe('UiHistoryDialog', () => {
  it('open=false 不渲染；翻真出遮罩+dialog+note 并焦点入窗（watch 语义=宿主切换）', async () => {
    const off = factory(false)
    expect(off.find('.hist-dialog').exists()).toBe(false)
    const w = factory(false)
    await w.setProps({ open: true })
    await flushPromises(); await nextTick()
    const d = w.find('.hist-dialog')
    expect(d.exists()).toBe(true)
    expect(d.attributes('aria-label')).toBe('查杀历史')
    expect(w.find('.hist-note').text()).toContain('200')
    expect(document.activeElement).toBe(d.element)
  })

  it('Esc：确认框在场时让位（不上抛 close），落定后再 Esc 才关', async () => {
    const { confirm, settleConfirm } = useConfirm()
    const w = factory(false)
    await w.setProps({ open: true })
    await flushPromises(); await nextTick()
    void confirm({ title: '清空本桶', description: 'x' })
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(w.emitted('close')).toBeFalsy() // 归确认框
    settleConfirm(false)
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('遮罩空白点击与关闭钮均上抛 close', async () => {
    const w = factory(false)
    await w.setProps({ open: true })
    await flushPromises(); await nextTick()
    await w.find('.hist-backdrop').trigger('click') // click.self：在遮罩元素本身
    expect(w.emitted('close')).toHaveLength(1)
    await w.find('.hist-head .btn').trigger('click')
    expect(w.emitted('close')).toHaveLength(2)
  })
})
