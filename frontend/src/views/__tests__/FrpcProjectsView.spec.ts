// 特征测试（组 G4）：FrpcProjectsView——frpc 多实例工作台（项目 CRUD/启停/日志抽屉/分享）。
// 基线锁定迁移前行为；迁移（useWailsEvent/useConfirm/useClipboard/useToast/format + MainTabNav）
// 后除机制面（confirm 载体、toast 位置）外逐字保持。
import { KeepAlive, defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FrpcProjectsView from '../FrpcProjectsView.vue'
import { useConfirm } from '../../composables/useConfirm'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  ListProjects: vi.fn(),
  ListInstanceStates: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  StartProject: vi.fn(),
  StopProject: vi.fn(),
  DeleteProject: vi.fn(),
  GetProjectLogs: vi.fn(),
  ParseToml: vi.fn(),
  GenerateToml: vi.fn(),
  DownloadVersion: vi.fn(),
  ImportLocalFrpc: vi.fn(),
  RemoveVersion: vi.fn(),
  OpenDir: vi.fn(),
  SaveProject: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/frpc/frpcservice', () => svc)

const writeText = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

const project = {
  id: 'p1',
  name: 'demo',
  version: 'v0.61.1',
  createdAt: '2026-07-01 10:00:00',
  updatedAt: '2026-07-01 10:00:00',
  server: {
    serverAddr: 'frp.example.com', serverPort: 7000, token: 'tk', protocol: 'tcp',
    proxyUrl: '', tlsEnable: true, useEncryption: true, useCompression: false, logLevel: 'info',
  },
  proxies: [
    {
      name: 'ssh', type: 'tcp', role: 'server', localIp: '127.0.0.1', localPort: 22, remotePort: 6000,
      customDomains: [], subdomain: '', secretKey: '', serverName: '', bindAddr: '', bindPort: 0,
      hostHeaderRewrite: '', proxyProtocolVersion: '', bandwidthLimit: '', encryptTransport: false,
    },
  ],
}

function snapOf(over: Record<string, unknown> = {}) {
  return {
    projectId: 'p1', projectName: 'demo', version: 'v0.61.1',
    state: 'running', connState: 'connected', pid: 4321, exitCode: 0, error: '',
    startedAt: new Date(Date.now() - 65_000).toISOString(), ...over,
  }
}

function stubBase(snaps: unknown[] = [snapOf()]) {
  svc.ListProjects.mockResolvedValue([project])
  svc.ListInstanceStates.mockResolvedValue(snaps)
  svc.ListInstalledVersions.mockResolvedValue([{ version: 'v0.61.1' }])
  svc.ListReleases.mockResolvedValue([])
}

function mountInKeepAlive() {
  const show = ref(true)
  const Host = defineComponent({
    render: () => (show.value ? h(KeepAlive, null, h(FrpcProjectsView)) : h('div')),
  })
  return { wrapper: mount(Host, { attachTo: document.body }), show }
}

beforeEach(() => {
  Object.defineProperty(globalThis.navigator, 'clipboard', { value: { writeText }, configurable: true })
  vi.stubGlobal('isSecureContext', true) // useClipboard 两级策略走安全上下文主路
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  writeText.mockClear()
})

describe('FrpcProjectsView 列表与状态徽章', () => {
  it('装载：卡片渲染 + 汇总行 + 子组件版本页并发拉取', async () => {
    stubBase()
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    expect(wrapper.find('.proj-name').text()).toBe('demo')
    expect(wrapper.text()).toContain('共 1 个项目 · 1 个运行中')
    expect(wrapper.find('.server-addr').text()).toBe('frp.example.com:7000')
    expect(svc.ListReleases).toHaveBeenCalled() // 版本管理 Tab（v-show 常驻）自加载
    wrapper.unmount()
  })

  it('状态矩阵：running+connected 嗅探文案 / auth_failed / 无快照未启动', async () => {
    stubBase([snapOf({ connState: 'connected' })])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    expect(wrapper.find('.proj-status').text()).toContain('已连接服务端')
    wrapper.unmount()

    stubBase([snapOf({ connState: 'auth_failed' })])
    const { wrapper: w2 } = mountInKeepAlive()
    await flushMicrotasks()
    expect(w2.find('.proj-status').text()).toContain('鉴权失败 (Token错误)')
    expect(w2.find('.proj-status').classes()).toContain('failed')
    w2.unmount()

    stubBase([])
    const { wrapper: w3 } = mountInKeepAlive()
    await flushMicrotasks()
    expect(w3.find('.proj-status').text()).toContain('未启动')
    w3.unmount()
  })

  it('运行中项目：停止按钮在、编辑/删除禁用；instance-state 事件即时改写徽章', async () => {
    stubBase([snapOf()])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    const actions = wrapper.findAll('.proj-actions button')
    expect(actions.map((b) => b.text()).join('|')).toContain('停止')
    expect(actions.find((b) => b.text() === '编辑')!.attributes('disabled')).toBeDefined()
    expect(actions.find((b) => b.text() === '删除')!.attributes('disabled')).toBeDefined()
    runtime.handlers['frpc:instance-state']({ data: snapOf({ state: 'running', connState: 'reconnecting' }) })
    await nextTick()
    expect(wrapper.find('.proj-status').text()).toContain('重连服务端中…')
    wrapper.unmount()
  })

  it('空态：无项目时出现引导空态与创建按钮', async () => {
    stubBase([])
    svc.ListProjects.mockResolvedValue([])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    expect(wrapper.find('.empty-state').text()).toContain('还没有 frp 项目')
    wrapper.unmount()
  })
})

describe('FrpcProjectsView 启停与删除', () => {
  it('停止运行中实例：StopProject + 回执 toast', async () => {
    stubBase([snapOf()])
    svc.StopProject.mockResolvedValue(undefined)
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    await wrapper.findAll('.proj-actions button').find((b) => b.text().includes('停止'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.StopProject).toHaveBeenCalledWith('p1')
    wrapper.unmount()
  })

  it('启动停止态实例：StartProject；在途/starting 态按钮禁用', async () => {
    stubBase([])
    let resolveStart: () => void = () => {}
    svc.StartProject.mockReturnValue(new Promise<void>((r) => (resolveStart = r)))
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    const startBtn = wrapper.findAll('.proj-actions button').find((b) => b.text().includes('启动'))!
    await startBtn.trigger('click')
    expect(svc.StartProject).toHaveBeenCalledWith('p1')
    expect(wrapper.findAll('.proj-actions button').find((b) => b.text().includes('启动'))!.attributes('disabled')).toBeDefined()
    resolveStart()
    await flushMicrotasks()
    wrapper.unmount()
  })

  // 迁移注记：删除确认由 window.confirm 收编至全局 useConfirm 单例（文案逐字拆 title/description）
  it('删除：运行中守卫；停止后经确认对话框删除并重载', async () => {
    const { confirmState, settleConfirm } = useConfirm()
    stubBase([snapOf()])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    const delBtn = wrapper.findAll('.proj-actions button').find((b) => b.text() === '删除')!
    await delBtn.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(false) // 运行中守卫直接拦截，不弹确认
    expect(svc.DeleteProject).not.toHaveBeenCalled()
    wrapper.unmount()

    stubBase([])
    svc.DeleteProject.mockResolvedValue(undefined)
    const { wrapper: w2 } = mountInKeepAlive()
    await flushMicrotasks()
    const del2 = w2.findAll('.proj-actions button').find((b) => b.text() === '删除')!
    await del2.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定删除项目「demo」？')
    expect(confirmState.options.tone).toBe('danger')
    settleConfirm(true)
    await flushMicrotasks(100)
    expect(svc.DeleteProject).toHaveBeenCalledWith('p1')
    expect(useToast().toastMsg.value).toBe('已删除「demo」')
    w2.unmount()
  })
})

describe('FrpcProjectsView 日志抽屉', () => {
  it('打开抽屉拉取 500 行并剥离 ANSI；warn 行高亮', async () => {
    stubBase([snapOf()])
    svc.GetProjectLogs.mockResolvedValue(['\x1b[32m[I] frps connected\x1b[0m', '[E] login fail: error'])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    await wrapper.findAll('.proj-actions button').find((b) => b.text() === '日志')!.trigger('click')
    await flushMicrotasks()
    expect(svc.GetProjectLogs).toHaveBeenCalledWith('p1', 500)
    const lines = wrapper.findAll('.log-line')
    expect(lines[0].text()).toContain('[I] frps connected')
    expect(lines[0].text()).not.toContain('\x1b')
    expect(lines[1].classes()).toContain('log-warn')
    wrapper.unmount()
  })

  it('instance-log 事件：仅当前项目行入抽屉；清屏清空', async () => {
    stubBase([snapOf()])
    svc.GetProjectLogs.mockResolvedValue([])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    await wrapper.findAll('.proj-actions button').find((b) => b.text() === '日志')!.trigger('click')
    await flushMicrotasks()
    runtime.handlers['frpc:instance-log']({ data: { projectId: 'other', line: '别家日志' } })
    runtime.handlers['frpc:instance-log']({ data: { projectId: 'p1', line: '本家日志' } })
    await nextTick()
    expect(wrapper.text()).not.toContain('别家日志')
    expect(wrapper.find('.log-line').text()).toBe('本家日志')
    await wrapper.findAll('.log-drawer-tools button').find((b) => b.text() === '清屏')!.trigger('click')
    expect(wrapper.find('.log-empty').text()).toContain('暂无日志输出')
    wrapper.unmount()
  })
})

describe('FrpcProjectsView 分享与端点复制', () => {
  it('分享链接：frp:// + base64 写入剪贴板（现状 navigator 直用）', async () => {
    stubBase([])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    await wrapper.findAll('.proj-actions button').find((b) => b.text().includes('分享'))!.trigger('click')
    await flushMicrotasks()
    const link = writeText.mock.calls[0][0] as string
    expect(link.startsWith('frp://')).toBe(true)
    expect(JSON.parse(decodeURIComponent(atob(link.slice(6)))).server.serverAddr).toBe('frp.example.com')
    wrapper.unmount()
  })

  it('tcp 端点行展示 穿透目标 且迷你复制可用', async () => {
    stubBase([snapOf()])
    const { wrapper } = mountInKeepAlive()
    await flushMicrotasks()
    const row = wrapper.find('.endpoint-row')
    expect(row.text()).toContain('frp.example.com:6000')
    await row.find('.btn-copy-mini').trigger('click')
    expect(writeText).toHaveBeenCalledWith('frp.example.com:6000')
    wrapper.unmount()
  })
})

it('卸载注销全部事件订阅（视图 2 + 版本子页 1）', async () => {
  stubBase()
  const { wrapper } = mountInKeepAlive()
  await flushMicrotasks()
  wrapper.unmount()
  await nextTick()
  expect(runtime.unlisten).toHaveBeenCalledTimes(3)
})
