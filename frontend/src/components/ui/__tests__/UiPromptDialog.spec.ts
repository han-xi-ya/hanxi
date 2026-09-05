// UiPromptDialog 契约：与 ConfirmDialog 同族的键盘/遮罩/焦点行为 + prompt 特有的
// 初值回填、Enter 提交、全选便于改写。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiPromptDialog from '../UiPromptDialog.vue'

function factory(props: Record<string, unknown> = {}) {
  return mount(UiPromptDialog, {
    props: { open: true, title: '设备备注', ...props },
    attachTo: document.body,
    global: { stubs: { teleport: true } },
  })
}

describe('UiPromptDialog', () => {
  it('open=false 不渲染', () => {
    const w = factory({ open: false })
    expect(w.find('.hx-prompt-backdrop').exists()).toBe(false)
  })

  it('渲染输入框与 label，initialValue 回填并全选', async () => {
    const w = factory({ label: '名称', initialValue: 'NAS-01' })
    const input = w.find('input')
    expect((input.element as HTMLInputElement).value).toBe('NAS-01')
    expect(w.find('label').text()).toContain('名称')
    expect(input.attributes('aria-invalid')).toBeUndefined()
  })

  it('以 open:false 起步再打开时焦点落输入框（watch 翻转路径，宿主真实用法）', async () => {
    const w = factory({ open: false })
    await w.setProps({ open: true })
    await new Promise((r) => setTimeout(r))
    expect(document.activeElement).toBe(w.find('input').element)
    w.unmount()
  })

  it('点击确认：submit 携带当前值并请求关闭', async () => {
    const w = factory({ initialValue: 'a' })
    await w.find('input').setValue('编辑后文本')
    await w.find('.hx-prompt-btn.primary').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual(['编辑后文本'])
    expect(w.emitted('update:open')?.[0]).toEqual([false])
  })

  it('输入框内按 Enter 提交；Esc 取消', async () => {
    const w = factory({ initialValue: 'x' })
    await w.find('input').trigger('keydown', { key: 'Enter' })
    expect(w.emitted('submit')?.[0]).toEqual(['x'])

    const w2 = factory({ open: false })
    await w2.setProps({ open: true })
    await new Promise((r) => setTimeout(r))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))
    expect(w2.emitted('cancel')).toHaveLength(1)
    w2.unmount()
  })

  it('点击遮罩空白取消；点击面板内部不取消', async () => {
    const w = factory()
    await w.find('.hx-prompt').trigger('mousedown')
    expect(w.emitted('cancel')).toBeUndefined()
    await w.find('.hx-prompt-backdrop').trigger('mousedown')
    expect(w.emitted('cancel')).toHaveLength(1)
  })
})
