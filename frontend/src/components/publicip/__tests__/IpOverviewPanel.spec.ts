// 拆分护航（Phase 6）：IpOverviewPanel——IP 查看区块的三态展示与事件透传（含 AdapterCard 二级上抛）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import IpOverviewPanel from '../IpOverviewPanel.vue'
import type { NetworkOverview } from '../../../../bindings/hanxi/internal/modules/publicip/models'
import type { Adapter } from '../../../../bindings/hanxi/internal/platform/models'

const adapter = {
  index: 6,
  name: 'WLAN',
  description: 'Intel Wi-Fi',
  mac: 'AA:BB:CC:00:11:22',
  ipv4: ['192.168.1.66'],
  ipv6: [],
  ipv6Details: [],
  gateway: '192.168.1.1',
  ipv6Gateway: '',
  dnsServers: ['223.5.5.5'],
  isPhysical: true,
  isLoopback: false,
  isUp: true,
} as unknown as Adapter

const overview = {
  publicIpv4: '1.2.3.4',
  publicIpv6: '',
  sourceV4: 'ip.sb',
  sourceV6: '',
  fetchedAt: '2026-09-05 10:00:00',
  adapters: [adapter],
} as unknown as NetworkOverview

function factory(over: Partial<{
  overview: NetworkOverview | null
  loading: boolean
  error: string
  adapters: Adapter[]
}> = {}) {
  return mount(IpOverviewPanel, {
    props: { overview, loading: false, error: '', adapters: [adapter], ...over },
    attachTo: document.body,
  })
}

describe('IpOverviewPanel 状态栏与卡片三态', () => {
  it('已有概览：显示探测时间与来源标注，IPv6 未开通落空态文案', () => {
    const w = factory()
    expect(w.find('.meta-info').text()).toBe('最后探测时间: 2026-09-05 10:00:00')
    expect(w.findAll('.provider-label').map(l => l.text())).toEqual(['来源: ip.sb'])
    expect(w.findAll('.ip-value code').map(c => c.text())).toEqual(['1.2.3.4'])
    expect(w.findAll('.ip-empty').map(e => e.text())).toEqual([
      '未检测到公网 IPv6（当前网络可能未开通或被防火墙拦截）',
    ])
    w.unmount()
  })

  it('首帧加载：等待探测 + 双占位文案，且不落网卡空态', () => {
    const w = factory({ overview: null, loading: true, adapters: [] })
    expect(w.find('.meta-info').text()).toBe('等待探测…')
    expect(w.findAll('.ip-placeholder').map(p => p.text())).toEqual([
      '正在探测公网 IPv4…',
      '正在探测公网 IPv6…',
    ])
    expect(w.find('.empty-hint').exists()).toBe(false)
    w.unmount()
  })

  it('拉取成功后无网卡：落空态提示', () => {
    const w = factory({ overview: null, adapters: [] })
    expect(w.find('.empty-hint').text()).toBe('未检测到活跃网卡网络信息')
    w.unmount()
  })

  it('error 经 .error-box 呈现（全局原子类名不变）', () => {
    const w = factory({ overview: null, error: '获取网络配置信息失败: API 限流' })
    expect(w.find('.error-box').text()).toBe('获取网络配置信息失败: API 限流')
    w.unmount()
  })
})

describe('IpOverviewPanel 事件上抛', () => {
  it('强制刷新按钮：点击上抛 refresh，加载中禁用且文案切为探测中…', async () => {
    const idle = factory()
    expect(idle.find('.control-panel .btn-primary').text()).toBe('强制刷新')
    await idle.find('.control-panel .btn-primary').trigger('click')
    expect(idle.emitted('refresh')).toHaveLength(1)
    idle.unmount()

    const busy = factory({ loading: true })
    expect(busy.find('.control-panel .btn-primary').attributes('disabled')).toBeDefined()
    expect(busy.find('.control-panel .btn-primary').text()).toBe('探测中…')
    busy.unmount()
  })

  it('公网 IPv4 卡：Ping 测试上抛出口 IP，复制上抛带标签；无值时两钮禁用', async () => {
    const w = factory()
    const [pingBtn, copyBtn] = w.findAll('.ip-card')[0].findAll('.btn-copy')
    await pingBtn.trigger('click')
    await copyBtn.trigger('click')
    expect(w.emitted('quick-ping')).toEqual([['1.2.3.4']])
    expect(w.emitted('copy-text')).toEqual([['1.2.3.4', '公网 IPv4']])
    w.unmount()

    const empty = factory({ overview: null })
    empty.findAll('.ip-card').forEach(card => {
      card.findAll('.btn-copy').forEach(btn => {
        expect(btn.attributes('disabled')).toBeDefined()
      })
    })
    empty.unmount()
  })

  it('网卡卡事件经面板原样透传（不吞不篡改标签）', async () => {
    const w = factory()
    await w.find('.chip-action[title="Ping DNS"]').trigger('click')
    await w.find('.dns-chip .chip-copy').trigger('click')
    expect(w.emitted('quick-ping')).toEqual([['223.5.5.5']])
    expect(w.emitted('copy-text')).toEqual([['223.5.5.5', 'DNS']])
    w.unmount()
  })
})
