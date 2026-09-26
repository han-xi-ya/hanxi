// BoardStage 舞台契约测试（v4 牌桌口径）：给定容器宽/字号（可选实测牌宽）
// → min(cap, (盒宽−12)/牌面自然宽) 的自适应 scale。重点钉四条——
// ①cap 默认 1.0：短文案宽舞台必须原大呈现（1:1 是合法终态）；
// ②牌宽未测到回落 字号×13 兜底线、盒宽未测到回落基准 300——happy-dom
//   无 ResizeObserver 的口径与 A 路 v4 视图内联式逐位一致（64px/300 盒
//   ＝scale(0.34615384615384615)）；
// ③scale 事件 immediate 首发且随 props 联动——宿主 caption 文案与舞台实际缩放同源；
// ④盒高在场才反算注入 --bc-max-h。内层类名沿用 .mbp-scaled：视图选择器零迁移。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BoardStage from '../BoardStage.vue'

describe('BoardStage v4 自适应缩放（量实测、能 1:1 就 1:1）', () => {
  it('盒宽 300 + 字号 64 且牌宽测不到：回落 ×13 兜底线 scale(0.34615…) 逐位一致', () => {
    const w = mount(BoardStage, { props: { text: '🤝 开会中', fontSize: 64, containerWidth: 300 } })
    expect(w.find('.mbp-scaled').attributes('style')).toContain('scale(0.34615384615384615)')
    expect(w.find('.bc-title').text()).toBe('🤝 开会中')
    expect(w.emitted('scale')!.at(-1)).toEqual([0.34615384615384615])
  })

  it('实测牌宽在场优先于猜测：短牌 300px 自然宽进 1000px 舞台直接 1:1', () => {
    const w = mount(BoardStage, {
      props: { text: '短', fontSize: 24, containerWidth: 1000, cardWidth: 300 },
    })
    expect(w.find('.mbp-scaled').attributes('style')).toContain('scale(1)')
  })

  it('cap 默认 1.0 绝不越 1；显式传 0.8（旧页内小预览档）时 grow 越线被截', () => {
    const capped = mount(BoardStage, {
      props: { text: '宽舞台', fontSize: 64, containerWidth: 2000, cardWidth: 100, cap: 0.8 },
    })
    expect(capped.find('.mbp-scaled').attributes('style')).toContain('scale(0.8)')
    const full = mount(BoardStage, {
      props: { text: '宽舞台', fontSize: 64, containerWidth: 2000, cardWidth: 100 },
    })
    expect(full.find('.mbp-scaled').attributes('style')).toContain('scale(1)')
  })

  it('容器宽未传（0）回落基准常数 300——happy-dom/首帧同口径', () => {
    const w = mount(BoardStage, { props: { text: '盲测', fontSize: 64 } })
    expect(w.find('.mbp-scaled').attributes('style')).toContain('scale(0.34615384615384615)')
  })

  it('盒高在场才反算注入 --bc-max-h（560 高、缩 0.34615…→1583px）；缺席不注入', () => {
    const w = mount(BoardStage, {
      props: { text: '高盒', fontSize: 64, containerWidth: 300, containerHeight: 560 },
    })
    expect(w.find('.mbp-scaled').attributes('style')).toContain('--bc-max-h: 1583px')
    const bare = mount(BoardStage, { props: { text: '矮盒', fontSize: 64, containerWidth: 300 } })
    expect(bare.find('.mbp-scaled').attributes('style')).not.toContain('--bc-max-h')
  })

  it('props 联动：字号翻倍即时收缩并重发 scale；文案热更同源进牌面', async () => {
    const w = mount(BoardStage, { props: { text: 'T', fontSize: 64, containerWidth: 300 } })
    await w.setProps({ fontSize: 120 })
    expect(w.find('.mbp-scaled').attributes('style')).toContain('scale(0.18461538461538463)')
    expect(w.emitted('scale')!.at(-1)).toEqual([0.18461538461538463])
    await w.setProps({ text: '🍜 干饭去了\n预计 1 小时后回来' })
    expect(w.find('.bc-title').text()).toBe('🍜 干饭去了')
    expect(w.find('.bc-sub').text()).toBe('预计 1 小时后回来')
  })
})
