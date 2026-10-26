import { describe, expect, it, vi } from 'vitest'
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useWindowKeyListener } from '../useWindowKeyListener'

// 断外部事实（按键是否真的被处理），不断 addEventListener 调用形状。
// KeepAlive 宿主形制与 App.vue 工作台缓存同谱：切页只 deactivate 不卸载。

function buildHarness() {
  const handler = vi.fn()
  const show = ref(true)
  const Child = defineComponent({
    name: 'KeyableChild',
    setup() {
      useWindowKeyListener('keydown', handler)
      return () => h('div', 'child')
    },
  })
  const Host = defineComponent({
    setup: () => () => h(KeepAlive, { max: 10 }, show.value ? h(Child) : h('div', 'blank')),
  })
  return { handler, show, Host }
}

function pressEsc() {
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
}

describe('useWindowKeyListener（KeepAlive 四斩）', () => {
  it('挂载即收键；mount+activate 双挂幂等，一次按键只触发一次', async () => {
    const { handler, Host } = buildHarness()
    const wrapper = mount(Host)
    pressEsc()
    expect(handler).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('失活（切页）期间按键不触发——异页不得幽灵操作隐藏视图', async () => {
    const { handler, show, Host } = buildHarness()
    const wrapper = mount(Host)
    show.value = false
    await nextTick()
    handler.mockClear()
    pressEsc()
    expect(handler).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('回访 activate 重新挂上；unmount 后彻底摘净', async () => {
    const { handler, show, Host } = buildHarness()
    const wrapper = mount(Host)
    show.value = false
    await nextTick()
    show.value = true
    await nextTick()
    pressEsc()
    expect(handler).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    handler.mockClear()
    pressEsc()
    expect(handler).not.toHaveBeenCalled()
  })
})
