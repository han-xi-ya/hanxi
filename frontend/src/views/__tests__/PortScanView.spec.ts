// 特征测试（组 G2 迁移护航）：PortScanView 表单校验/模式参数联动/流式增量去重排序/
// 代理防抖/终止链路。迁移注记：视图 .btn-primary 原硬编码 GitHub 蓝已由全局原子归一青绿，
// 类名选择器不受影响；error-banner 手搓横幅 → UiBanner(.banner.banner-error)。
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PortScanView from '../PortScanView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  GetPresets: vi.fn(),
  CheckEgressIP: vi.fn(),
  StartScan: vi.fn(),
  StopScan: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/portscan', () => ({ PortScanService: svc }))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const presets = [
  { key: 'web', name: 'Web开发', description: '常用前端端口', ports: '3000,5173,8080' },
  { key: 'top100', name: 'TOP 100', description: '高频端口', ports: '21,22,80,443' },
]

function stubDefaults() {
  svc.GetPresets.mockResolvedValue(presets)
  svc.CheckEgressIP.mockResolvedValue('1.2.3.4')
}

function mountView() {
  return mount(PortScanView, { attachTo: document.body })
}

function stubClipboard() {
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn() }, configurable: true })
}

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('PortScanView 装载与表单', () => {
  it('挂载拉预设与出网 IP，渲染预设胶囊', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    expect(svc.GetPresets).toHaveBeenCalled()
    expect(svc.CheckEgressIP).toHaveBeenCalled()
    expect(w.find('.indicator-ip').text()).toContain('1.2.3.4')
    const chips = w.findAll('.preset-chip')
    expect(chips.map((c) => c.text())).toEqual(['Web开发', 'TOP 100'])
    w.unmount()
  })

  it('点预设回填端口并 toast', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.findAll('.preset-chip')[1].trigger('click')
    expect((w.find('input[placeholder="如 80,443,3000-3010,8080"]').element as HTMLInputElement).value).toBe('21,22,80,443')
    expect(useToast().toastMsg.value).toBe('已应用预设：TOP 100')
    w.unmount()
  })

  it('模式切换联动参数：极速=200并发/400ms/微延隐藏', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    await w.find('.select-input').setValue('fast')
    await w.find('.select-input').trigger('change')
    const nums = w.findAll('.num-input')
    expect((nums[0].element as HTMLInputElement).value).toBe('400') // 超时
    expect((nums[1].element as HTMLInputElement).value).toBe('200') // 并发
    expect(nums.length).toBe(2) // 微延(rateLimitMs=0)不渲染
    w.unmount()
  })

  it('目标为空校验：横幅提示且不发请求（UiBanner 收编原 error-banner）', async () => {
    stubDefaults()
    svc.StartScan.mockResolvedValue(null)
    const w = mountView()
    await flushMicrotasks()
    await w.find('.target-input').setValue('   ')
    await w.findAll('.action-bar button')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.StartScan).not.toHaveBeenCalled()
    expect(w.find('.banner.banner-error').text()).toContain('请输入扫描目标 IP 或域名')
    w.unmount()
  })
})

describe('PortScanView 扫描链路', () => {
  it('完整扫描：summary 回填进度/耗时/结果行与条件打开按钮', async () => {
    stubDefaults()
    svc.StartScan.mockResolvedValue({
      openPorts: [
        { port: 443, status: 'open', service: 'https', banner: 'nginx', fingerprint: '', latencyMs: 12 },
        { port: 80, status: 'open', service: 'http', banner: '', fingerprint: 'Nmap:cpe', latencyMs: 9 },
      ],
      durationMs: 2333,
      totalPorts: 11,
    })
    const w = mountView()
    await flushMicrotasks()
    await w.find('.btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.StartScan).toHaveBeenCalledWith(
      expect.objectContaining({ target: '127.0.0.1', timeoutMs: 800, concurrency: 20, deepDetect: true }),
    )
    expect(w.find('.progress-info').text()).toContain('11 / 11 (100%)')
    expect(w.find('.duration-tag').text()).toContain('2333ms')
    const rows = w.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('nginx')
    // 打开按钮仅对 web 端口渲染（两行都是）
    expect(rows[0].find('.btn-xs').exists()).toBe(true)
    expect(rows[1].find('.fp-badge').text()).toBe('Nmap:cpe')
    w.unmount()
  })

  it('流式进度事件：增量收录、去重、按端口升序', async () => {
    stubDefaults()
    svc.StartScan.mockReturnValue(new Promise(() => {})) // 挂起保持扫描态
    const w = mountView()
    await flushMicrotasks()
    await w.find('.btn-primary').trigger('click')

    const port = (n: number) => ({ port: n, status: 'open', service: 'http', banner: '', fingerprint: '', latencyMs: 5 })
    runtime.handlers['portscan:progress']({ data: { taskId: 't7', scanned: 2, total: 10, percent: 20, latestPort: port(443) } })
    await nextTick()
    runtime.handlers['portscan:progress']({ data: { taskId: 't7', scanned: 3, total: 10, percent: 30, latestPort: port(22) } })
    await nextTick()
    runtime.handlers['portscan:progress']({ data: { taskId: 't7', scanned: 3, total: 10, percent: 30, latestPort: port(443) } }) // 重复
    await nextTick()
    const rows = w.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('22') // 升序：22 在前
    expect(w.find('.progress-info').text()).toContain('(30%)')
    w.unmount()
  })

  it('终止扫描：携带事件回传的 taskId 调 StopScan', async () => {
    stubDefaults()
    svc.StartScan.mockReturnValue(new Promise(() => {}))
    svc.StopScan.mockResolvedValue(undefined)
    const w = mountView()
    await flushMicrotasks()
    await w.find('.btn-primary').trigger('click')
    runtime.handlers['portscan:progress']({ data: { taskId: 'task-42', scanned: 1, total: 5, percent: 20 } })
    await nextTick()
    const stopBtn = w.findAll('.action-bar button').find((b) => b.text().includes('终止扫描'))!
    await stopBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.StopScan).toHaveBeenCalledWith('task-42')
    expect(useToast().toastMsg.value).toBe('已终止扫描')
    w.unmount()
  })

  it('出网 IP 探测失败显示「检测失败」', async () => {
    svc.GetPresets.mockResolvedValue([])
    svc.CheckEgressIP.mockRejectedValue(new Error('proxy down'))
    const w = mountView()
    await flushMicrotasks()
    expect(w.find('.indicator-ip').text()).toContain('检测失败')
    w.unmount()
  })
})

describe('PortScanView 胶水：复制与防抖', () => {
  it('复制全部端口：逗号连接 + 计数 toast', async () => {
    stubDefaults()
    stubClipboard()
    svc.StartScan.mockResolvedValue({
      openPorts: [
        { port: 80, status: 'open', service: 'http', banner: '', fingerprint: '', latencyMs: 1 },
        { port: 443, status: 'open', service: 'https', banner: '', fingerprint: '', latencyMs: 2 },
      ],
      durationMs: 10,
      totalPorts: 2,
    })
    const w = mountView()
    await flushMicrotasks()
    await w.find('.btn-primary').trigger('click')
    await flushMicrotasks()
    await w.findAll('.header-actions button')[0].trigger('click')
    await flushMicrotasks()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('80, 443')
    expect(useToast().toastMsg.value).toBe('已复制 2 个开放端口')
    w.unmount()
  })

  it('代理输入 600ms 防抖重测出网 IP；卸载后定时器不再触发', async () => {
    stubDefaults()
    vi.useFakeTimers()
    const w = mountView()
    await flushMicrotasks()
    const calls = svc.CheckEgressIP.mock.calls.length
    await w.find('.proxy-input').setValue('socks5://127.0.0.1:7890')
    await vi.advanceTimersByTimeAsync(599)
    expect(svc.CheckEgressIP.mock.calls.length).toBe(calls) // 未到期不触发
    await vi.advanceTimersByTimeAsync(1)
    expect(svc.CheckEgressIP.mock.calls.length).toBe(calls + 1)
    w.unmount()
    const after = svc.CheckEgressIP.mock.calls.length
    await w.vm.$nextTick()
    await vi.advanceTimersByTimeAsync(5000)
    expect(svc.CheckEgressIP.mock.calls.length).toBe(after)
  })

  it('卸载注销事件订阅', async () => {
    stubDefaults()
    const w = mountView()
    await flushMicrotasks()
    w.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(1)
  })
})
