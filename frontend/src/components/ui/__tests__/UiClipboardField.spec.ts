// UiClipboardField：粘贴填入（追加/替换策略）、复制钮显隐与回执话术。
// 走真实 useClipboard，仅桩 navigator.clipboard（与 useClipboard.spec 同一手法）。
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UiClipboardField from '../UiClipboardField.vue'
import { useToast } from '../../../composables/useToast'

function stubClipboard(impl: { readText?: () => Promise<string>; writeText?: (s: string) => Promise<void> } | undefined) {
  Object.defineProperty(navigator, 'clipboard', { value: impl, configurable: true })
}
Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })

const originalClipboard = navigator.clipboard
const originalExecCommand = document.execCommand
afterEach(() => {
  stubClipboard(originalClipboard)
  Object.defineProperty(document, 'execCommand', { configurable: true, value: originalExecCommand })
})
beforeEach(() => {
  useToast().toastMsg.value = ''
})

function textarea(w: ReturnType<typeof mount>) {
  return w.find('textarea')
}
function pasteBtn(w: ReturnType<typeof mount>) {
  return w.findAll('button').find((b) => b.text().includes('粘贴'))
}
function copyBtn(w: ReturnType<typeof mount>) {
  return w.findAll('button').find((b) => b.text().includes('复制'))
}

describe('UiClipboardField', () => {
  it('输入态默认见粘贴钮、无复制钮；textarea 可编辑并透传 v-model', async () => {
    stubClipboard({})
    const w = mount(UiClipboardField, { props: { modelValue: '初始', rows: 4, placeholder: '贴这里' } })
    expect(pasteBtn(w)).toBeTruthy()
    expect(copyBtn(w)).toBeFalsy()
    expect(textarea(w).attributes('rows')).toBe('4')
    expect(textarea(w).attributes('placeholder')).toBe('贴这里')
    await textarea(w).setValue('改成我')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual(['改成我'])
  })

  it('空框点粘贴：readText 内容直接填入并 toast', async () => {
    stubClipboard({ readText: async () => '剪贴板文本' })
    const w = mount(UiClipboardField, { props: { modelValue: '' } })
    await pasteBtn(w)!.trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual(['剪贴板文本'])
    expect(useToast().toastMsg.value).toBe('已粘贴剪贴板内容')
  })

  it('已有内容默认追加（不无声覆盖，行界自动补换行）', async () => {
    stubClipboard({ readText: async () => '第二行' })
    const w = mount(UiClipboardField, { props: { modelValue: '第一行' } })
    await pasteBtn(w)!.trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual(['第一行\n第二行'])
  })

  it('pasteMode=replace：整体替换', async () => {
    stubClipboard({ readText: async () => '新内容' })
    const w = mount(UiClipboardField, { props: { modelValue: '旧内容', pasteMode: 'replace' } })
    await pasteBtn(w)!.trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual(['新内容'])
  })

  it('readText 不可用：降级提示手动 Ctrl+V，不发 emit', async () => {
    stubClipboard({ readText: () => Promise.reject(new Error('NotAllowedError')) })
    const w = mount(UiClipboardField, { props: { modelValue: '' } })
    await pasteBtn(w)!.trigger('click')
    expect(w.emitted('update:modelValue')).toBeUndefined()
    expect(useToast().toastMsg.value).toContain('Ctrl+V')
  })

  it('剪贴板空白文本：提示无文本，不填入', async () => {
    stubClipboard({ readText: async () => '  \n ' })
    const w = mount(UiClipboardField, { props: { modelValue: '' } })
    await pasteBtn(w)!.trigger('click')
    expect(w.emitted('update:modelValue')).toBeUndefined()
    expect(useToast().toastMsg.value).toBe('剪贴板中没有文本')
  })

  it('只读态：textarea readonly、默认见复制钮，点击复制全文', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    stubClipboard({ writeText })
    const w = mount(UiClipboardField, { props: { modelValue: '结果全文', readonly: true } })
    expect(textarea(w).attributes('readonly')).toBeDefined()
    expect(pasteBtn(w)).toBeFalsy()
    await copyBtn(w)!.trigger('click')
    expect(writeText).toHaveBeenCalledWith('结果全文')
    expect(useToast().toastMsg.value).toBe('已复制全文')
  })

  it('复制失败话术统一为「复制失败」', async () => {
    stubClipboard({ writeText: () => Promise.reject(new Error('denied')) })
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => false })
    const w = mount(UiClipboardField, { props: { modelValue: 'x', readonly: true } })
    await copyBtn(w)!.trigger('click')
    expect(useToast().toastMsg.value).toBe('复制失败')
  })
})
