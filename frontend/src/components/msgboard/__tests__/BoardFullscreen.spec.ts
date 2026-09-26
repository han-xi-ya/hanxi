// BoardFullscreen 独立契约测试（不依赖 MsgBoardView）：Teleport 落体、
// 角标/提示常驻、Esc 与点击都只 emit('close')（开合态归宿主 v-if）、
// 监听生命周期三斩——unmount 摘干净、KeepAlive deactivate 摘、activate
// 补挂（审查 #20 纪律的组件化形态）。零后端链路：本 spec 不 mock 任何
// API——组件若敢 import 后端绑定，这里直接红。
import { flushPromises, mount } from '@vue/test-utils'
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import BoardFullscreen from '../BoardFullscreen.vue'

const PROPS = { text: '☕ 去茶水间了\n5 分钟内回来', fontSize: 96 }

const layer = () => document.body.querySelector<HTMLElement>('.mbp-full')

function mountLayer(props = PROPS) {
  return mount(BoardFullscreen, { props, attachTo: document.body })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('BoardFullscreen 浮层本体', () => {
  it('挂载即 Teleport 到 body：同源 BoardCard 吃 props 定标，角标与提示常驻', () => {
    const w = mountLayer()
    expect(layer()).not.toBeNull()
    expect(layer()!.querySelector('.bc-title')?.textContent).toBe('☕ 去茶水间了')
    expect(layer()!.querySelector('.bc-sub')?.textContent).toBe('5 分钟内回来')
    expect(layer()!.querySelector('.bc-card')?.getAttribute('style')).toContain('font-size: 96px')
    expect(layer()!.querySelector('.mbp-full-badge')?.textContent).toContain('预览浮层 · 非真实挂牌')
    expect(layer()!.querySelector('.mbp-full-hint')?.textContent).toContain('Esc')
    expect(layer()!.getAttribute('role')).toBe('dialog')
    w.unmount()
    expect(layer()).toBeNull()
  })

  it('点击浮层任意处只 emit close——组件不自杀，收层归宿主', () => {
    const w = mountLayer()
    layer()!.click()
    expect(w.emitted('close')).toHaveLength(1)
    // 宿主没收 v-if 之前浮层仍在（自杀式关闭会劫持开合真相）
    expect(layer()).not.toBeNull()
    w.unmount()
  })

  it('按 Esc emit close；非 Esc 键不误触', () => {
    const w = mountLayer()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    expect(w.emitted('close')).toBeUndefined()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })

  it('props 联动：草稿热更即时反映到牌面（预览联动入参而非缓存快照）', async () => {
    const w = mountLayer()
    await w.setProps({ text: '🤝 开会中' })
    expect(layer()!.querySelector('.bc-title')?.textContent).toBe('🤝 开会中')
    expect(layer()!.querySelector('.bc-sub')).toBeNull()
    w.unmount()
  })
})

describe('Esc 监听生命周期（unmount/deactivate/activate）', () => {
  it('卸载后监听随浮层一起收干净，再按 Esc 不产生幽灵 close', () => {
    const w = mountLayer()
    w.unmount()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(w.emitted('close')).toBeUndefined()
  })

  it('KeepAlive 切页：deactivate 摘监听（异页按 Esc 不误触），回到本页补挂即复活', async () => {
    const show = ref(true)
    let closes = 0
    const Host = defineComponent({
      render: () =>
        h(
          KeepAlive,
          null,
          show.value
            ? h(BoardFullscreen, { ...PROPS, onClose: () => (closes += 1) })
            : h('div', { class: 'other-page' }, '别的工作台页'),
        ),
    })
    const wrapper = mount(Host, { attachTo: document.body })
    await flushPromises()
    const pressEsc = () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))

    // 在场：Esc 即 close
    pressEsc()
    expect(closes).toBe(1)

    // 切页：浮层随宿主 deactivate（KeepAlive 缓存不卸载），监听必须已摘——
    // 异页按 Esc 不该惊动隐藏预览（审查 #20 的病灶本体）
    show.value = false
    await nextTick()
    pressEsc()
    expect(closes).toBe(1)

    // 回到本页：activate 补挂，Esc 复活
    show.value = true
    await nextTick()
    pressEsc()
    expect(closes).toBe(2)
    wrapper.unmount()
  })
})
