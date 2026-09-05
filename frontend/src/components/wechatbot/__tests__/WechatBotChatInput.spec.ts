// Phase 6 拆分新增：WechatBotChatInput 展示壳冒烟测试。
// 纯 props/emit 组件（无 bindings/事件依赖）：受控 v-model 回写、Enter/Shift+Enter
// 快捷键与四动作 emit 的独立接线锁定；业务链路仍由
// views/__tests__/WechatBotView.spec.ts 经视图整体锁定。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import WechatBotChatInput from '../WechatBotChatInput.vue'

function mountInput(props: Record<string, unknown> = {}) {
  return mount(WechatBotChatInput, { props: { modelValue: '', isSending: false, ...props } })
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

  it('工具条/发送按钮动作逐键 emit；isSending 时工具按钮与发送按钮禁用', async () => {
    const wrapper = mountInput({ modelValue: 'x' })
    await wrapper.findAll('.toolbar-btn')[0].trigger('click')
    await wrapper.findAll('.toolbar-btn')[1].trigger('click')
    await wrapper.find('.toolbar-btn.text-danger').trigger('click')
    await wrapper.find('.btn-send-message').trigger('click')
    expect(wrapper.emitted('send-image')).toHaveLength(1)
    expect(wrapper.emitted('send-file')).toHaveLength(1)
    expect(wrapper.emitted('clear')).toHaveLength(1)
    expect(wrapper.emitted('send-text')).toHaveLength(1)

    const busy = mountInput({ modelValue: 'x', isSending: true })
    expect(busy.findAll('.toolbar-btn')[0].attributes('disabled')).toBeDefined()
    expect(busy.find('.btn-send-message').attributes('disabled')).toBeDefined()
    expect(busy.find('.btn-send-message').text()).toBe('发送中…')
  })
})
