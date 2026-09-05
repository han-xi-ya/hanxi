// 特征测试（组 G2 迁移护航）：LanScannerView 扫描流 + 进度事件 + 行内备注编辑 + 复制。
// 断言基于 DOM 文本/调用序列；卸载确认等交互机制未变（本视图无 confirm）。
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import LanScannerView from '../LanScannerView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  GetSubnets: vi.fn(),
  Scan: vi.fn(),
  Cancel: vi.fn(),
  SetRemark: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/lan', () => ({ LanService: svc }))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const subnets = [
  { name: 'WLAN', ip: '192.168.1.66', cidr: '192.168.1.0/24' },
  { name: 'Ethernet', ip: '10.0.0.5', cidr: '10.0.0.0/24' },
]

const devices = [
  { ip: '192.168.1.1', mac: 'AA:BB:CC:DD:EE:01', remark: '路由器', rttMs: 3, isSelf: false },
  { ip: '192.168.1.66', mac: 'AA:BB:CC:DD:EE:02', remark: '', rttMs: 0, isSelf: true },
  { ip: '192.168.1.99', mac: '', remark: '', rttMs: 888, isSelf: false },
]

function stubDefaults() {
  svc.GetSubnets.mockResolvedValue(subnets)
  svc.Scan.mockResolvedValue(devices)
  svc.Cancel.mockResolvedValue(undefined)
  svc.SetRemark.mockResolvedValue(undefined)
}

function mountView() {
  return mount(LanScannerView, { attachTo: document.body })
}

function stubClipboard(ok = true) {
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn(ok ? undefined : () => Promise.reject(new Error('denied'))) },
    configurable: true,
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('LanScannerView 装载与扫描', () => {
  it('挂载拉取网卡并自动选中首个网段', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    expect(svc.GetSubnets).toHaveBeenCalled()
    expect((w.find('input.input-box').element as HTMLInputElement).value).toBe('192.168.1.0/24')
    w.unmount()
  })

  it('网卡拉取失败进错误框', async () => {
    svc.GetSubnets.mockRejectedValue(new Error('IP Helper 不可用'))
    const w = mountView()
    await flushMicrotasks()
    expect(w.find('.error-box').text()).toContain('获取网卡子网失败')
    expect(w.find('.error-box').text()).toContain('IP Helper 不可用')
    w.unmount()
  })

  it('开始扫描：调用 Scan(cidr) 并渲染设备行', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Scan).toHaveBeenCalledWith('192.168.1.0/24')
    const rows = w.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('192.168.1.1')
    expect(rows[0].text()).toContain('AA:BB:CC:DD:EE:01')
    expect(rows[0].find('.chip').classes()).toContain('chip-neutral') // 在线设备
    expect(rows[1].find('.chip').classes()).toContain('chip-information') // 本机
    expect(rows[1].classes()).toContain('row-self')
    w.unmount()
  })

  it('空结果扫描给出无设备 toast', async () => {
    stubDefaults()
    svc.Scan.mockResolvedValue([])
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('扫描完成，未探测到在线设备')
    w.unmount()
  })

  it('扫描中显示停止按钮，点击调 Cancel 并恢复可扫态', async () => {
    stubDefaults()
    let resolveScan: (v: unknown) => void = () => {}
    svc.Scan.mockReturnValue(new Promise((r) => (resolveScan = r)))
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    await nextTick()
    const stopBtn = w.findAll('button').find((b) => b.text() === '停止扫描')!
    expect(stopBtn).toBeTruthy()
    svc.Cancel.mockResolvedValue(undefined)
    await stopBtn.trigger('click')
    expect(svc.Cancel).toHaveBeenCalled()
    resolveScan([])
    await flushMicrotasks()
    expect(w.findAll('button').some((b) => b.text() === '开始扫描')).toBe(true)
    w.unmount()
  })

  it('lan:progress 事件即时改写进度文本', async () => {
    stubDefaults()
    svc.Scan.mockReturnValue(new Promise(() => {})) // 永挂起：保持 scanning
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    runtime.handlers['lan:progress']({ data: { scanned: 120, total: 254, found: 7 } })
    await nextTick()
    expect(w.find('.progress-meta').text()).toContain('120 / 254')
    expect(w.find('.found-badge').text()).toContain('发现 7 台设备')
    w.unmount()
  })

  it('卸载注销进度订阅', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    w.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(1)
  })
})

describe('LanScannerView 交互：复制与行内备注', () => {
  it('复制 IP：clipboard 成功后 toast（迁移注记：原实现不 await，失败也谎报成功——useClipboard 收编后如实报失败）', async () => {
    stubDefaults()
    stubClipboard()
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click') // 先出数据
    await flushMicrotasks()
    await w.findAll('.btn-action')[0].trigger('click')
    await flushMicrotasks()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('192.168.1.1')
    expect(useToast().toastMsg.value).toBe('已复制 IP: 192.168.1.1')
    w.unmount()
  })

  it('剪贴板失败不再谎报：toast 为「复制失败」', async () => {
    stubDefaults()
    stubClipboard(false)
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    await flushMicrotasks()
    await w.findAll('.btn-action')[0].trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('复制失败')
    w.unmount()
  })

  it('行内编辑备注：有 MAC 以 MAC 为键回写并刷新行文本', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    await flushMicrotasks()
    await w.findAll('.remark-display')[1].trigger('click') // 第二行（本机，remark 空）
    await new Promise((r) => setTimeout(r)) // 等 nextTick 回调完成聚焦
    const input = w.find('.remark-edit-box input')
    // 组 G2 修复注记：v-for 内模板 ref 为数组、原实现 focus() 必抛（自动聚焦从未生效）；
    // 现按作者本意恢复——进入编辑即聚焦全选
    expect(document.activeElement).toBe(input.element)
    await input.setValue('开发机')
    await input.trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.SetRemark).toHaveBeenCalledWith('AA:BB:CC:DD:EE:02', '开发机')
    expect(w.findAll('.tbl tbody tr')[1].text()).toContain('开发机')
    expect(useToast().toastMsg.value).toBe('备注已保存')
    w.unmount()
  })

  it('无 MAC 设备以 IP 为键；清空备注走「备注已清除」', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    await flushMicrotasks()
    await w.findAll('.remark-display')[2].trigger('click') // 第三行 mac=''
    await nextTick()
    const input = w.find('.remark-edit-box input')
    await input.setValue('   ')
    await input.trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.SetRemark).toHaveBeenCalledWith('192.168.1.99', '')
    expect(useToast().toastMsg.value).toBe('备注已清除')
    w.unmount()
  })

  it('RTT 分级着色：<20 fast / <500 normal / 其余 slow', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.find('button.btn-primary').trigger('click')
    await flushMicrotasks()
    const tags = w.findAll('.rtt-tag')
    expect(tags[0].classes()).toContain('fast')
    expect(tags[1].classes()).toContain('fast')
    expect(tags[2].classes()).toContain('slow')
    w.unmount()
  })
})
