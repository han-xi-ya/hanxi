// 特征测试（组 G3 / Phase 5）：FileShareView 迁移前基线锁定。
// 巨型孤本第三方言视图：本次只机械收编胶水（事件/轮询/剪贴板），
// 模板骨架与业务调用（FileShareService.*、qrcode、投递箱、审计）契约不变。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FileShareView from '../FileShareView.vue'
import { useToast } from '../../composables/useToast'

const srv = vi.hoisted(() => ({
  GetServerStatus: vi.fn(),
  GetConfig: vi.fn(),
  GetNetworkEndpoints: vi.fn(),
  GetDropInbox: vi.fn(),
  SaveConfig: vi.fn(),
  StartServer: vi.fn(),
  StopServer: vi.fn(),
  ChooseDirectory: vi.fn(),
  DeleteDropItem: vi.fn(),
  ClearDropInbox: vi.fn(),
}))

const app = vi.hoisted(() => ({ OpenPath: vi.fn() }))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data?: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data?: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/fileshare', () => ({ FileShareService: srv }))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: app }))

const qr = vi.hoisted(() => ({ toString: vi.fn() }))
vi.mock('qrcode', () => ({ default: qr }))

function statusOf(over: Record<string, unknown> = {}) {
  return {
    isRunning: false, port: 80, sharePath: 'D:\\share', activeConnections: 0,
    uploadCount: 0, downloadCount: 0, uploadBytes: 0, downloadBytes: 0,
    uploadRate: 0, downloadRate: 0, allowUpload: true, allowTextDrop: true,
    autoSaveToMemo: true, activeUrls: [], startedAt: '', ...over,
  }
}

const CFG = { port: 8080, sharePath: 'D:\\share', allowUpload: true, allowTextDrop: true, autoSaveToMemo: true, maxUploadSizeMB: 0, authToken: '' }
const EPS = [
  { interfaceName: 'Wi-Fi', ip: '192.168.1.8', url: 'http://192.168.1.8:8080', isDefault: true },
  { interfaceName: '以太网', ip: '10.10.0.5', url: 'http://10.10.0.5:8080', isDefault: false },
]

function stubHappy(status = statusOf()) {
  srv.GetServerStatus.mockResolvedValue(status as never)
  srv.GetConfig.mockResolvedValue({ ...CFG } as never)
  srv.GetNetworkEndpoints.mockResolvedValue(EPS as never)
  srv.GetDropInbox.mockResolvedValue([] as never)
  srv.SaveConfig.mockResolvedValue(undefined)
  srv.StopServer.mockResolvedValue(undefined)
  srv.StartServer.mockResolvedValue(statusOf({ isRunning: true, port: 8080 }) as never)
  srv.DeleteDropItem.mockResolvedValue(undefined)
  srv.ClearDropInbox.mockResolvedValue(undefined)
  qr.toString.mockImplementation(async (url: string) => `<svg data-qr="${url}"></svg>`)
}

async function flushMicrotasks(times = 30) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(FileShareView)) : h('div')),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushMicrotasks()
  return { wrapper, show }
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('FileShareView 装载与二维码', () => {
  it('首屏四路并发 + 每端点生成 SVG 二维码并上屏', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    const { wrapper } = await mountInKeepAlive()
    for (const fn of [srv.GetServerStatus, srv.GetConfig, srv.GetNetworkEndpoints, srv.GetDropInbox]) expect(fn).toHaveBeenCalledTimes(1)
    expect(qr.toString).toHaveBeenCalledTimes(2)
    const html = wrapper.html()
    expect(html).toContain('data-qr="http://192.168.1.8:8080"')
    expect(wrapper.text()).toContain('服务运行中')
    wrapper.unmount()
  })

  it('加载失败 → 可关闭的 alert 横幅（✕ 清空错误）', async () => {
    srv.GetServerStatus.mockRejectedValue(new Error('端口被占用'))
    srv.GetConfig.mockRejectedValue(new Error('x'))
    srv.GetNetworkEndpoints.mockRejectedValue(new Error('x'))
    srv.GetDropInbox.mockRejectedValue(new Error('x'))
    qr.toString.mockResolvedValue('')
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.text()).toContain('加载文件快传数据失败: 端口被占用')
    await wrapper.find('.alert-banner button').trigger('click')
    await nextTick()
    expect(wrapper.find('.alert-banner').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('FileShareView 服务启停与配置', () => {
  it('停止态点启动：先 SaveConfig 后 StartServer，成功 toast 含端口', async () => {
    stubHappy()
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.hero-action').trigger('click')
    await flushMicrotasks()
    expect(srv.SaveConfig).toHaveBeenCalledTimes(1)
    expect(srv.StartServer).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toContain('已成功启动于端口 :8080')
    wrapper.unmount()
  })

  it('运行态点停止：StopServer + toast，且按钮类切换', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    const { wrapper } = await mountInKeepAlive()
    const btn = wrapper.find('.hero-action')
    expect(btn.classes()).toContain('btn-danger')
    await btn.trigger('click')
    await flushMicrotasks()
    expect(srv.StopServer).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('局域网文件快传服务已停止')
    wrapper.unmount()
  })

  it('打开目录门禁：与已保存路径一致可点；改动未保存禁用；保存后恢复并可开目录', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    app.OpenPath.mockResolvedValue(undefined)
    const { wrapper } = await mountInKeepAlive()
    const openBtn = wrapper.findAll('.path-action').find(b => b.text() === '打开目录')!
    // 现状：初始表单与已保存路径一致 → 可用
    expect(openBtn.attributes('disabled')).toBeUndefined()
    await wrapper.find('.input-control#share-path').setValue('D:\\other')
    await nextTick()
    expect(openBtn.attributes('disabled')).toBeDefined() // 未保存改动 → 禁用
    await wrapper.findAll('.btn-secondary').find(b => b.text() === '保存共享规则')!.trigger('click')
    await flushMicrotasks()
    expect(srv.SaveConfig.mock.calls.at(-1)![0].sharePath).toBe('D:\\other')
    await nextTick()
    expect(openBtn.attributes('disabled')).toBeUndefined()
    await openBtn.trigger('click')
    expect(app.OpenPath).toHaveBeenCalledWith('D:\\other')
    wrapper.unmount()
  })

  it('运行中选择目录后自动热保存（SaveConfig 追加一次）', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    srv.ChooseDirectory.mockResolvedValue('E:\\pick' as never)
    const { wrapper } = await mountInKeepAlive()
    await wrapper.findAll('.path-action').find(b => b.text() === '选择目录')!.trigger('click')
    await flushMicrotasks()
    expect(srv.SaveConfig).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toContain('热应用到运行中的快传服务')
    wrapper.unmount()
  })

  it('快捷端口芯片即时改写表单端口', async () => {
    stubHappy()
    const { wrapper } = await mountInKeepAlive()
    const chips = wrapper.findAll('.quick-port-chip')
    await chips[0].trigger('click') // 配置里是 8080，点快捷 80 应即时改写
    await nextTick()
    expect((wrapper.find('#share-port').element as HTMLInputElement).value).toBe('80')
    wrapper.unmount()
  })
})

describe('FileShareView 剪贴板与投递箱', () => {
  it('复制访问链接成功/失败文案逐字', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    // 机制迁移注记：复制改由 useClipboard 两级策略驱动——显式构造"安全上下文 + clipboard 可用"
    // 使其走第①路；成功/失败 toast 文案仍逐字锁定。
    const write = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: write }, configurable: true })
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    const { wrapper } = await mountInKeepAlive()
    await wrapper.find('.endpoint-card .btn-secondary').trigger('click')
    await flushMicrotasks()
    expect(write).toHaveBeenCalledWith('http://192.168.1.8:8080')
    expect(useToast().toastMsg.value).toBe('已复制访问链接')
    write.mockImplementationOnce(() => Promise.reject(new Error('denied')))
    await wrapper.find('.endpoint-card .btn-secondary').trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('复制到剪贴板失败')
    wrapper.unmount()
  })

  it('删除与清空投递：本地列表即时收敛 + toast 逐字', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    srv.GetDropInbox.mockResolvedValue([
      { id: 'a', content: '第一片', senderIp: '192.168.1.42', isUrl: false, createdAt: new Date('2026-09-05T10:00:00').toISOString() },
      { id: 'b', content: 'https://example.com/x', senderIp: '192.168.1.42', isUrl: true, createdAt: new Date('2026-09-05T10:01:00').toISOString() },
    ] as never)
    const { wrapper } = await mountInKeepAlive()
    await wrapper.findAll('.tab-item')[1].trigger('click')
    await nextTick()
    expect(wrapper.findAll('.inbox-item')).toHaveLength(2)
    expect(wrapper.text()).toContain('网址链接')
    const del = wrapper.findAll('.inbox-item')[0].findAll('button').find(b => b.text().includes('删除'))!
    await del.trigger('click')
    await flushMicrotasks()
    expect(srv.DeleteDropItem).toHaveBeenCalledWith('a')
    expect(wrapper.findAll('.inbox-item')).toHaveLength(1)
    expect(useToast().toastMsg.value).toBe('已删除投递内容')
    await wrapper.find('.card-header .btn-secondary').trigger('click')
    await flushMicrotasks()
    expect(srv.ClearDropInbox).toHaveBeenCalled()
    expect(wrapper.findAll('.inbox-item')).toHaveLength(0)
    expect(useToast().toastMsg.value).toBe('投递箱已清空')
    wrapper.unmount()
  })

  it('text-dropped 事件：入箱置顶 + toast 截断 24 字符', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['fileshare:text-dropped']({ data: { id: 'z', content: 'x'.repeat(30), senderIp: '192.168.1.9', isUrl: false, createdAt: new Date().toISOString() } })
    await nextTick()
    expect(useToast().toastMsg.value).toContain(`收到来自移动端的文本投递: ${'x'.repeat(24)}...`)
    wrapper.unmount()
  })
})

describe('FileShareView 事件、审计与轮询契约', () => {
  it('status 事件即时改写运行态与端口', async () => {
    stubHappy()
    const { wrapper } = await mountInKeepAlive()
    expect(wrapper.text()).toContain('服务已停止')
    runtime.handlers['fileshare:status']({ data: statusOf({ isRunning: true, port: 8888 }) })
    await nextTick()
    expect(wrapper.text()).toContain('服务运行中')
    expect(wrapper.text()).toContain('端口 :8888')
    wrapper.unmount()
  })

  it('transfer 事件头插进审计表（切到传输审计 tab），上限 50', async () => {
    stubHappy(statusOf({ isRunning: true, port: 8080 }))
    const { wrapper } = await mountInKeepAlive()
    runtime.handlers['fileshare:transfer']({ data: { type: 'upload', filename: 'a.bin', size: 1536, clientIp: '192.168.1.2', success: true, timestamp: new Date().toISOString() } })
    for (let i = 0; i < 55; i++) {
      runtime.handlers['fileshare:transfer']({ data: { type: 'download', filename: `f${i}`, size: 0, clientIp: 'x', success: false, timestamp: new Date().toISOString() } })
    }
    await nextTick()
    await wrapper.findAll('.tab-item')[2].trigger('click')
    await nextTick()
    const rows = wrapper.findAll('.table tbody tr')
    // 现状契约：slice(0,49)+头插＝至多 50 条，最旧被挤出（a.bin 已不可见）
    expect(rows).toHaveLength(50)
    expect(rows[0].text()).toContain('f54')
    expect(rows[0].text()).toContain('-') // size 0 显示 '-'
    expect(rows[49].text()).toContain('f5')
    expect(wrapper.text()).not.toContain('a.bin')
    // 顺带锁定该视图自有 formatBytes 口径（千进制、一位小数、非托管家族 fmtSize）
    runtime.handlers['fileshare:transfer']({ data: { type: 'upload', filename: 'big.bin', size: 1536, clientIp: '1.2.3.4', success: true, timestamp: new Date().toISOString() } })
    await nextTick()
    expect(wrapper.findAll('.table tbody tr')[0].text()).toContain('1.5 KB')
    wrapper.unmount()
  })

  it('轮询仅运行中推进；KeepAlive 停用后彻底停止（不泄漏）', async () => {
    vi.useFakeTimers()
    try {
      stubHappy(statusOf({ isRunning: true, port: 8080 }))
      const { wrapper, show } = await mountInKeepAlive()
      const after = srv.GetServerStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2000 * 3)
      expect(srv.GetServerStatus.mock.calls.length).toBeGreaterThanOrEqual(after + 3)
      show.value = false
      await nextTick()
      const stopped = srv.GetServerStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2000 * 5)
      expect(srv.GetServerStatus.mock.calls.length).toBe(stopped)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('停止态轮询空转保护：推进时钟也不拉状态', async () => {
    vi.useFakeTimers()
    try {
      stubHappy(statusOf({ isRunning: false }))
      const { wrapper } = await mountInKeepAlive()
      const after = srv.GetServerStatus.mock.calls.length
      await vi.advanceTimersByTimeAsync(2000 * 4)
      expect(srv.GetServerStatus.mock.calls.length).toBe(after)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('三事件订阅卸载全注销', async () => {
    stubHappy()
    const { wrapper } = await mountInKeepAlive()
    expect(Object.keys(runtime.handlers).sort()).toEqual(['fileshare:status', 'fileshare:text-dropped', 'fileshare:transfer'])
    wrapper.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(3)
  })
})
