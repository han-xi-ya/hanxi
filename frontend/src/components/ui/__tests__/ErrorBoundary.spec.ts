// ErrorBoundary 契约：捕获子树渲染异常→降级面板；重试与 resetKey 双复位路径。
// 注意：错误态翻转经响应式调度，断言前必须 nextTick 冲刷。
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ErrorBoundary from '../ErrorBoundary.vue'

function makeBoundaryChild(boom: { value: boolean }) {
  return defineComponent({
    render() {
      if (boom.value) throw new Error('渲染爆炸')
      return h('span', '界面正常')
    },
  })
}

describe('ErrorBoundary', () => {
  it('子树正常时原样渲染 slot', () => {
    const boom = ref(false)
    const w = mount(ErrorBoundary, { slots: { default: () => h(makeBoundaryChild(boom)) } })
    expect(w.text()).toContain('界面正常')
    w.unmount()
  })

  it('子树抛错：塌成降级面板并显示错误文案，应用不白屏', async () => {
    const boom = ref(true)
    const w = mount(ErrorBoundary, { slots: { default: () => h(makeBoundaryChild(boom)) } })
    await nextTick()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.text()).toContain('该模块渲染出错')
    expect(w.text()).toContain('渲染爆炸')
    w.unmount()
  })

  it('错误修复后点重试就地恢复；未修复则重试再次捕获不逃逸', async () => {
    const boom = ref(true)
    const w = mount(ErrorBoundary, { slots: { default: () => h(makeBoundaryChild(boom)) } })
    await nextTick() // 先冲刷首挂错误态，等面板就位
    // 未修复：重试 → 再抛 → 仍是面板（错误不外泄、不死循环）
    await w.find('button').trigger('click')
    expect(w.find('[role="alert"]').exists()).toBe(true)
    // 修复后：重试 → 正常渲染
    boom.value = false
    await w.find('button').trigger('click')
    expect(w.text()).toContain('界面正常')
    w.unmount()
  })

  it('resetKey 变化（如路由切换）自动清除错误态', async () => {
    const boom = ref(false)
    const w = mount(ErrorBoundary, {
      props: { resetKey: 'a' },
      slots: { default: () => h(makeBoundaryChild(boom)) },
    })
    boom.value = true
    await w.setProps({ resetKey: 'b' }) // 边界复位，但子树尚未重挂：先等 key 变化触发的 slot 重渲染
    // resetKey 复位后 slot 重新渲染；此时仍抛错 → 再次进入面板（证明复位生效且仍受控）
    expect(w.find('[role="alert"]').exists()).toBe(true)
    boom.value = false
    await w.setProps({ resetKey: 'c' })
    expect(w.text()).toContain('界面正常')
    w.unmount()
  })
})
