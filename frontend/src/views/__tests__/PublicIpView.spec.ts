// 特征测试（组 G2 迁移护航）：PublicIpView 三 Tab 状态机——网卡过滤口径、
// Ping/Traceroute 结果行、快捷联动跳转、复制。迁移注记：diag-tbl→全局 .tbl 原子、
// badge-*→chip（静态文本）、status-badge 保留（动态错误文本防 chip nowrap 截断）。
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PublicIpView from '../PublicIpView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  GetNetworkOverview: vi.fn(),
  PingTarget: vi.fn(),
  TraceRoute: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/publicip', () => ({ PublicIPService: svc }))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const overview = {
  publicIpv4: '1.2.3.4',
  publicIpv6: '2408:8000::5',
  sourceV4: 'ip.sb',
  sourceV6: 'ip.sb',
  fetchedAt: '2026-09-05 10:00:00',
  adapters: [
    { name: 'Loopback Pseudo-Interface 1', description: '', isPhysical: false, isLoopback: true, ipv4: ['127.0.0.1'], ipv6: [], ipv6Details: [], gateway: '', ipv6Gateway: '', dnsServers: [], mac: '' },
    { name: 'WLAN', description: 'Intel Wi-Fi', isPhysical: true, isLoopback: false, ipv4: ['192.168.1.66'], ipv6: ['fe80::1'], ipv6Details: [{ address: '2408:8000:1::66', type: 'Global', isTemporary: false }, { address: 'fd12::1', type: 'LinkLocal', isTemporary: false }, { address: '2408:8000:1::ab', type: 'Global', isTemporary: true }], gateway: '192.168.1.1', ipv6Gateway: 'fe80::网关', dnsServers: ['223.5.5.5'], mac: 'AA:BB:CC:00:11:22' },
    { name: 'NoIP Adapter', description: '', isPhysical: false, isLoopback: false, ipv4: [], ipv6: [], ipv6Details: [], gateway: '', ipv6Gateway: '', dnsServers: [], mac: '' },
  ],
}

function stubDefaults() {
  svc.GetNetworkOverview.mockResolvedValue(overview)
}

function mountView() {
  return mount(PublicIpView, { attachTo: document.body })
}

function stubClipboard() {
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn() }, configurable: true })
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('PublicIpView IP 查看 Tab', () => {
  it('挂载拉取概览：公网卡渲染、回环/无地址网卡被过滤', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    expect(svc.GetNetworkOverview).toHaveBeenCalledWith(false)
    expect(w.find('.ip-value code').text()).toBe('1.2.3.4')
    const cards = w.findAll('.adapter-card')
    expect(cards).toHaveLength(1)
    expect(cards[0].text()).toContain('WLAN')
    expect(cards[0].text()).toContain('Intel Wi-Fi')
    // 物理网卡徽标在头部标签区（卡内另有 v6「全局主地址」同为 chip-positive，注意作用域）
    expect(cards[0].findAll('.adapter-tags .chip-positive')).toHaveLength(1)
    w.unmount()
  })

  it('拉取失败进错误框', async () => {
    svc.GetNetworkOverview.mockRejectedValue(new Error('API 限流'))
    const w = mountView()
    await flushMicrotasks()
    expect(w.find('.error-box').text()).toContain('获取网络配置信息失败')
    expect(w.find('.error-box').text()).toContain('API 限流')
    w.unmount()
  })

  it('强制刷新以 force=true 重拉', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.find('.control-panel .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.GetNetworkOverview).toHaveBeenLastCalledWith(true)
    w.unmount()
  })

  it('IPv6 三类徽标：临时隐私/链路本地/全局主地址', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    const v6 = w.findAll('.v6-chip')
    expect(v6).toHaveLength(3)
    expect(v6[0].classes()).toEqual(['v6-chip']) // 全局主地址无修饰类
    expect(v6[1].classes()).toContain('is-linklocal')
    expect(v6[2].classes()).toContain('is-temp')
    expect(v6[2].text()).toContain('临时隐私地址')
    w.unmount()
  })

  it('复制 DNS 芯片：toast 携带标签与值', async () => {
    stubDefaults()
    stubClipboard()
    const w = mountView()
    await flushMicrotasks()
    // DNS 芯片的复制钮（DOM 顺序上 v6 地址组在 DNS 之后，不能用"最后一个"）
    await w.find('.dns-chip .chip-copy').trigger('click')
    await flushMicrotasks()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('223.5.5.5')
    expect(useToast().toastMsg.value).toBe('已复制 DNS: 223.5.5.5')
    w.unmount()
  })
})

describe('PublicIpView Ping Tab', () => {
  it('发起 Ping：默认目标与次数；结果行含成功/超时态', async () => {
    stubDefaults()
    svc.PingTarget.mockResolvedValue({
      target: '1.1.1.1', ip: '1.1.1.1', sent: 4, received: 3, lossRate: 25,
      minRtt: 10.2, avgRtt: 20.5, maxRtt: 40.8,
      results: [
        { seq: 1, ip: '1.1.1.1', success: true, rttMs: 10.2, ttl: 56, errorMsg: '' },
        { seq: 2, ip: '1.1.1.1', success: false, rttMs: 0, ttl: 0, errorMsg: '' },
      ],
    })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.tab-item')[1].trigger('click')
    await w.find('.btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.PingTarget).toHaveBeenCalledWith('1.1.1.1', 4)
    expect(w.find('.diag-summary-bar').text()).toContain('4 / 3')
    expect(w.find('.val-warn').text()).toContain('25.0%') // 丢包告警色
    const rows = w.findAll('.tbl tbody tr')
    expect(rows[0].find('.rtt-tag').classes()).toContain('fast')
    expect(rows[1].find('.status-badge').classes()).toContain('fail')
    expect(rows[1].text()).toContain('请求超时') // errorMsg 空时兜底文案
    w.unmount()
  })

  it('网卡芯片⚡快捷 Ping：切 Tab、回填目标并立即执行', async () => {
    stubDefaults()
    svc.PingTarget.mockResolvedValue({ target: '192.168.1.1', ip: '192.168.1.1', sent: 4, received: 4, lossRate: 0, minRtt: 1, avgRtt: 1, maxRtt: 1, results: [] })
    const w = mountView()
    await flushMicrotasks()
    const gatewayPingBtn = w.findAll('.chip-action').find((b) => b.attributes('title') === 'Ping 网关')!
    await gatewayPingBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.PingTarget).toHaveBeenCalledWith('192.168.1.1', 4)
    expect(w.findAll('.tab-item')[1].classes()).toContain('active')
    w.unmount()
  })

  it('空目标禁用发起按钮', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.tab-item')[1].trigger('click')
    await w.find('.text-input').setValue('  ')
    await nextTick()
    expect(w.find('.tool-panel .btn-primary').attributes('disabled')).toBeDefined()
    w.unmount()
  })
})

describe('PublicIpView Traceroute Tab', () => {
  it('追踪结果：* 跳与最终目标徽标', async () => {
    stubDefaults()
    svc.TraceRoute.mockResolvedValue({
      target: 'www.taobao.com', ip: '140.205.60.1', complete: true,
      hops: [
        { hop: 1, ip: '192.168.1.1', success: true, rttMs: 2 },
        { hop: 2, ip: '*', success: false, rttMs: 0 },
        { hop: 3, ip: '140.205.60.1', success: true, rttMs: 33 },
      ],
    })
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.tab-item')[2].trigger('click')
    await w.find('.btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.TraceRoute).toHaveBeenCalledWith('1.1.1.1', 20)
    const rows = w.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[1].text()).toContain('节点不响应 ICMP')
    expect(rows[2].find('.status-badge').classes()).toContain('target')
    expect(w.find('.diag-summary-bar').text()).toContain('3 跳')
    expect(w.find('.diag-summary-bar').text()).toContain('已到达目标主机')
    w.unmount()
  })

  it('追踪失败进错误框且不落结果卡', async () => {
    stubDefaults()
    svc.TraceRoute.mockRejectedValue(new Error('tracert 拒访'))
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.tab-item')[2].trigger('click')
    await w.find('.btn-primary').trigger('click')
    await flushMicrotasks()
    expect(w.find('.error-box').text()).toContain('路由追踪执行失败: tracert 拒访')
    expect(w.find('.diag-card').exists()).toBe(false)
    w.unmount()
  })
})
