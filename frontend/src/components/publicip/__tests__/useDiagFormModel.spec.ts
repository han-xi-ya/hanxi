// useDiagFormModel 纯件直测：RTT 色档阈值的边界值本身（面板 DOM 用例只锚过单点 fast，
// 50/150 两条分界线归此钉死）。色档词表是 diagRtt.css 的挂点契约——档位错则颜色错。
import { describe, expect, it } from 'vitest'
import { DIAG_QUICK_TARGETS, rttTier } from '../useDiagFormModel'

describe('rttTier 分档阈值（<50 fast / <150 medium / ≥150 slow）', () => {
  it('档位表逐点钉死：49.9/50 与 149.9/150 两处台阶（阈值本身归下一档，严格小于）', () => {
    const table: Array<[number, 'fast' | 'medium' | 'slow']> = [
      [0, 'fast'],
      [49.9, 'fast'],
      [50, 'medium'],
      [149.9, 'medium'],
      [150, 'slow'],
      [9999, 'slow'],
    ]
    for (const [ms, tier] of table) {
      expect(rttTier(ms)).toBe(tier)
    }
  })

  it('快捷目标 label↔value 不错位：每个 DNS 名号配的都是它真实的公共地址', () => {
    // 外部事实：四家公共 DNS 的官方服务地址，配错名号即 ping 错目标
    expect(DIAG_QUICK_TARGETS.aliDns).toEqual({ label: '阿里 DNS', value: '223.5.5.5' })
    expect(DIAG_QUICK_TARGETS.tencentDns).toEqual({ label: '腾讯 DNS', value: '119.29.29.29' })
    expect(DIAG_QUICK_TARGETS.cloudflare).toEqual({ label: 'Cloudflare', value: '1.1.1.1' })
    expect(DIAG_QUICK_TARGETS.google).toEqual({ label: 'Google', value: '8.8.8.8' })
  })
})
