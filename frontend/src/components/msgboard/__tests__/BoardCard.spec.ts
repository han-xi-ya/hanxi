// BoardCard 独立契约测试（v4 收编）：不依赖任何视图/后端桩，纯画法断言。
// 锁定对外契约——props 只有 text/fontSize，版式拆分、内联定标链
// （fontSize 直用、maxWidth 吃 ×13 系数、副行 ×0.42 托底 12）、胶带条与
// role=presentation 装饰语义；MsgBoardPopup/MsgBoardView 的特征测试继续
// 从宿主侧钉同一批数值，双保险不互替。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BoardCard from '../BoardCard.vue'

const card = (text: string, fontSize: number) => mount(BoardCard, { props: { text, fontSize } })

describe('BoardCard 牌面本体', () => {
  it('对外 props 契约：text/fontSize；主题=首行、副行=其余行', () => {
    const w = card('☕ 去茶水间了\n20 分钟内回来', 96)
    expect(w.find('.bc-title').text()).toBe('☕ 去茶水间了')
    expect(w.find('.bc-sub').text()).toBe('20 分钟内回来')
    expect(w.attributes('role')).toBe('presentation')
    expect(w.classes()).toContain('bc-card')
  })

  it('定标链内联样式：fontSize 直用 + 卡宽上限 min(round(×13)px, 88vw)', () => {
    const style = card('测试', 96).find('.bc-card').attributes('style')!
    expect(style).toContain('font-size: 96px')
    expect(style).toContain('max-width: min(1248px, 88vw)')
  })

  it('副行字号 ×0.42 显式换算并托底 12px', () => {
    expect(card('T\n副', 96).find('.bc-sub').attributes('style')).toContain('font-size: 40px')
    // 24×0.42=10.08 → round 10 → 托底 12：小字号也不印成噪点
    expect(card('T\n副', 24).find('.bc-sub').attributes('style')).toContain('font-size: 12px')
  })

  it('单行/全空白副行不产生空气 bc-sub 节点', () => {
    expect(card('马上回来', 64).find('.bc-sub').exists()).toBe(false)
    expect(card('马上回来\n   \n', 64).find('.bc-sub').exists()).toBe(false)
  })

  it('胶带条常驻且纯装饰（aria-hidden），空正文牌体仍完整渲染', () => {
    const w = card('', 64)
    const tape = w.find('.bc-tape')
    expect(tape.exists()).toBe(true)
    expect(tape.attributes('aria-hidden')).toBe('true')
    expect(w.find('.bc-title').text()).toBe('')
    expect(w.find('.bc-sub').exists()).toBe(false)
  })
})
