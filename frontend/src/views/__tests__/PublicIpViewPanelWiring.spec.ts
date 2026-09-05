// 拆分护航（Phase 6）：PublicIpView ⇄ components/publicip/* 的跨组件接线。
// 特征测试 PublicIpView.spec.ts 是三 Tab 行为主基线；本文件只补拆分新增的接缝：
// v-model 双向写回（含 v-model:max-hops 的 kebab 形）、常用目标先回填后发起、双层事件透传后的复制 toast。
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
    { name: 'WLAN', description: 'Intel Wi-Fi', isPhysical: true, isLoopback: false, ipv4: ['192.168.1.66'], ipv6: ['fe80::1'], ipv6Details: [], gateway: '192.168.1.1', ipv6Gateway: '', dnsServers: ['223.5.5.5'], mac: 'AA:BB:CC:00:11:22' },
  ],
}

const pingOk = { target: '1.1.1.1', ip: '1.1.1.1', sent: 4, received: 4, lossRate: 0, minRtt: 1, avgRtt: 1, maxRtt: 1, results: [] }
const traceOk = { target: '1.1.1.1', ip: '1.1.1.1', complete: true, hops: [] }

function stubDefaults() {
  svc.GetNetworkOverview.mockResolvedValue(overview)
  svc.PingTarget.mockResolvedValue(pingOk)
  svc.TraceRoute.mockResolvedValue(traceOk)
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

describe('PublicIpView 面板双向链路', () => {
  it('Ping 次数下拉写回视图：发起时以新次数入参（v-model:count）', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.tab-item')[1].trigger('click')
    await w.find('.select-input').setValue('8')
    await w.find('.tool-panel .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.PingTarget).toHaveBeenCalledWith('1.1.1.1', 8)
    w.unmount()
  })

  it('Ping 目标改写后 Enter 直接发起，常用目标按钮同样带入新值', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.tab-item')[1].trigger('click')
    await w.find('.text-input').setValue(' 192.168.1.66 ')
    await w.find('.text-input').trigger('keyup', { key: 'Enter' })
    await flushMicrotasks()
    expect(svc.PingTarget).toHaveBeenLastCalledWith('192.168.1.66', 4)
    await w.findAll('.btn-quick')[3].trigger('click')
    await flushMicrotasks()
    expect(svc.PingTarget).toHaveBeenLastCalledWith('8.8.8.8', 4)
    w.unmount()
  })

  it('Traceroute 最大跳数写回视图（v-model:max-hops kebab 形）', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.tab-item')[2].trigger('click')
    await w.find('.select-input').setValue('15')
    await w.find('.tool-panel .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.TraceRoute).toHaveBeenCalledWith('1.1.1.1', 15)
    w.unmount()
  })

  it('公网 IPv6 卡复制：双层组件透传后 toast 文案与标签逐字', async () => {
    stubDefaults()
    stubClipboard()
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.ip-card')[1].find('.btn-copy').trigger('click')
    await flushMicrotasks()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('2408:8000::5')
    expect(useToast().toastMsg.value).toBe('已复制 公网 IPv6: 2408:8000::5')
    w.unmount()
  })
})
