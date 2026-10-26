// utils/qr 公共正源首测：两个历史调用点（WifiView 220/#1f2328、
// useFileShareServer 140/默认 #1e293b）依赖的公共形制锁——type=svg、
// width 如实透传、纯白底强制、墨色参数定制与默认档。断外部可验证事实
// （真实 qrcode 库产物的字符串形状），不镜像实现调用参数。
import { describe, expect, it } from 'vitest'
import { makeQrSvg } from '../qr'

describe('makeQrSvg', () => {
  it('产出 svg 文档且 width 如实透传（文件共享页 140 实拍档）', async () => {
    const svg = await makeQrSvg('http://10.0.0.1:8384', 140)
    expect(svg).toContain('<svg')
    expect(svg).toContain('width="140"')
    expect(svg).toContain('height="140"')
  })

  it('扫码器硬对比契约：白底强制 #ffffff，缺省墨色 #1e293b', async () => {
    const svg = await makeQrSvg('SSID', 120)
    expect(svg).toContain('fill="#ffffff"') // 背景路径恒纯白，不随主题/token
    expect(svg).toContain('stroke="#1e293b"') // 默认墨色 = 现文件共享页在用档
  })

  it('墨色可定制（Wifi 页 #1f2328 档），且与默认档互不串台', async () => {
    const svg = await makeQrSvg('PASSPHRASE', 220, '#1f2328')
    expect(svg).toContain('stroke="#1f2328"')
    expect(svg).not.toContain('#1e293b')
    expect(svg).toContain('width="220"')
  })

  it('同内容确定性产物、异内容产物分歧（编码非随机，可直接作 v-html 缓存值）', async () => {
    const a = await makeQrSvg('same-payload', 140)
    const b = await makeQrSvg('same-payload', 140)
    const c = await makeQrSvg('other-payload', 140)
    expect(a).toBe(b)
    expect(a).not.toBe(c)
  })
})
