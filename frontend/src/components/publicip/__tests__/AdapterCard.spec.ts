// 拆分护航（Phase 6）：AdapterCard——网卡卡的 props→DOM 结构与快捷动作上抛口径。
// 类名/文案逐字沿用拆分前的 PublicIpView（特征测试 PublicIpView.spec.ts 为主基线，此处守组件边界）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AdapterCard from '../AdapterCard.vue'
import type { Adapter } from '../../../../bindings/hanxi/internal/platform/models'

const wlan = {
  index: 6,
  name: 'WLAN',
  description: 'Intel Wi-Fi',
  mac: 'AA:BB:CC:00:11:22',
  ipv4: ['192.168.1.66'],
  ipv6: ['fe80::1'],
  ipv6Details: [
    { address: '2408:8000:1::66', type: 'Global', isTemporary: false },
    { address: 'fd12::1', type: 'LinkLocal', isTemporary: false },
    { address: '2408:8000:1::ab', type: 'Temporary', isTemporary: true },
  ],
  gateway: '192.168.1.1',
  ipv6Gateway: 'fe80::1',
  dnsServers: ['223.5.5.5'],
  isPhysical: true,
  isLoopback: false,
  isUp: true,
} as unknown as Adapter

const bare = {
  index: 7,
  name: 'NoIP Adapter',
  description: '',
  mac: '',
  ipv4: [],
  ipv6: [],
  ipv6Details: [],
  gateway: '',
  ipv6Gateway: '',
  dnsServers: [],
  isPhysical: false,
  isLoopback: false,
  isUp: false,
} as unknown as Adapter

function factory(adapter: Adapter) {
  return mount(AdapterCard, { props: { adapter }, attachTo: document.body })
}

describe('AdapterCard 展示', () => {
  it('根节点即 .adapter-card，头部含名称/描述/物理徽标/MAC', () => {
    const w = factory(wlan)
    expect(w.element.className).toBe('adapter-card')
    expect(w.find('.adapter-name').text()).toBe('WLAN')
    expect(w.find('.adapter-desc').text()).toBe('Intel Wi-Fi')
    expect(w.find('.adapter-tags .chip-positive').text()).toBe('物理网卡')
    expect(w.find('.mac-text').text()).toBe('MAC: AA:BB:CC:00:11:22')
    w.unmount()
  })

  it('IPv6 三类徽标类名逐字：全局主地址无修饰类', () => {
    const v6 = factory(wlan).findAll('.v6-chip')
    expect(v6).toHaveLength(3)
    expect(v6[0].classes()).toEqual(['v6-chip'])
    expect(v6[1].classes()).toContain('is-linklocal')
    expect(v6[2].classes()).toContain('is-temp')
  })

  it('无地址/无网关/无 DNS 时保持占位文案，且不渲染 IPv6 组', () => {
    const w = factory(bare)
    expect(w.findAll('.muted-text').map(m => m.text())).toEqual(['无', '—', '—'])
    expect(w.find('.info-group.full-width').exists()).toBe(false)
    expect(w.find('.chip-neutral').text()).toBe('虚拟 / 隧道')
    w.unmount()
  })
})

describe('AdapterCard 事件上抛', () => {
  it('⚡ 上抛 quick-ping（局域网 IP / 网关 / DNS 各自取值）', async () => {
    const w = factory(wlan)
    await w.find('.ip-chip .chip-action').trigger('click')
    await w.find('.chip-action[title="Ping 网关"]').trigger('click')
    await w.find('.dns-chip .chip-action').trigger('click')
    expect(w.emitted('quick-ping')).toEqual([['192.168.1.66'], ['192.168.1.1'], ['223.5.5.5']])
    w.unmount()
  })

  it('⧉ 上抛 copy-text 且标签文案与拆分前逐字一致', async () => {
    const w = factory(wlan)
    await w.find('.dns-chip .chip-copy').trigger('click')
    await w.findAll('.ip-chip')[1].find('.chip-copy').trigger('click')
    expect(w.emitted('copy-text')).toEqual([['223.5.5.5', 'DNS'], ['192.168.1.1', '默认网关']])
    w.unmount()
  })
})
