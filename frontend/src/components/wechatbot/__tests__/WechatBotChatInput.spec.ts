// Phase 6 拆分新增：WechatBotChatInput 展示壳冒烟测试。
// 纯 props/emit 组件（无 bindings/事件依赖）：受控 v-model 回写、Enter/Shift+Enter
// 快捷键与四动作 emit 的独立接线锁定；业务链路仍由
// views/__tests__/WechatBotView.spec.ts 经视图整体锁定。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import WechatBotChatInput from '../WechatBotChatInput.vue'

function mountInput(props: Record<string, unknown> = {}) {
  return mount(WechatBotChatInput, {
    props: {
      modelValue: '',
      isSending: false,
      attachmentDraft: null,
      attachmentError: '',
      isPreparingAttachment: false,
      ...props,
    },
  })
}

describe('WechatBotChatInput', () => {
  it('文本经受控 v-model 回写父级；发送按钮空文本禁用', async () => {
    const wrapper = mountInput()
    expect(wrapper.find('.btn-send-message').attributes('disabled')).toBeDefined()
    await wrapper.find('.wechat-textarea').setValue('你好')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['你好'])
  })

  it('Enter emit send-text，Shift+Enter 不发送（保留换行）', async () => {
    const wrapper = mountInput({ modelValue: 'x' })
    await wrapper.find('.wechat-textarea').trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(wrapper.emitted('send-text')).toBeUndefined()
    await wrapper.find('.wechat-textarea').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('send-text')).toHaveLength(1)
  })

  it('统一附件入口与发送按钮动作接线；isSending 时全部发送动作禁用', async () => {
    const wrapper = mountInput({ modelValue: 'x' })
    await wrapper.find('.attachment-btn').trigger('click')
    await wrapper.find('.toolbar-btn.text-danger').trigger('click')
    await wrapper.find('.btn-send-message').trigger('click')
    expect(wrapper.emitted('choose-attachment')).toHaveLength(1)
    expect(wrapper.emitted('clear')).toHaveLength(1)
    expect(wrapper.emitted('send-text')).toHaveLength(1)

    const busy = mountInput({ modelValue: 'x', isSending: true })
    expect(busy.find('.attachment-btn').attributes('disabled')).toBeDefined()
    expect(busy.find('.btn-send-message').attributes('disabled')).toBeDefined()
    expect(busy.find('.btn-send-message').text()).toBe('发送中…')
  })

  it('拦截剪贴板文件附件并进入待确认，普通文本粘贴保持默认行为', async () => {
    const wrapper = mountInput()
    const textarea = wrapper.find('.wechat-textarea').element
    const file = new File(['report'], 'report.pdf', { type: 'application/pdf' })
    const fileEvent = new Event('paste', { bubbles: true, cancelable: true }) as ClipboardEvent
    Object.defineProperty(fileEvent, 'clipboardData', {
      value: { items: [{ kind: 'file', type: 'application/pdf', getAsFile: () => file }] },
    })
    textarea.dispatchEvent(fileEvent)
    await wrapper.vm.$nextTick()
    expect(fileEvent.defaultPrevented).toBe(true)
    expect(wrapper.emitted('paste-attachment')?.[0]).toEqual([file])

    const textEvent = new Event('paste', { bubbles: true, cancelable: true }) as ClipboardEvent
    Object.defineProperty(textEvent, 'clipboardData', {
      value: { items: [{ kind: 'string', type: 'text/plain', getAsFile: () => null }] },
    })
    textarea.dispatchEvent(textEvent)
    expect(textEvent.defaultPrevented).toBe(false)
  })

  it('附件先展示预览并需确认发送；移除不发送', async () => {
    const wrapper = mountInput({
      attachmentDraft: {
        path: 'D:\\pic\\cat.png', fileName: 'cat.png', fileSize: 2048,
        isImage: true, previewUrl: 'data:image/png;base64,OUT',
      },
    })
    expect(wrapper.find('.attachment-thumb').attributes('src')).toBe('data:image/png;base64,OUT')
    expect(wrapper.find('.attachment-meta').text()).toContain('cat.png')
    expect(wrapper.emitted('send-attachment')).toBeUndefined()
    await wrapper.find('.attachment-send').trigger('click')
    expect(wrapper.emitted('send-attachment')).toHaveLength(1)
    await wrapper.find('.attachment-remove').trigger('click')
    expect(wrapper.emitted('clear-attachment')).toHaveLength(1)
  })
})
