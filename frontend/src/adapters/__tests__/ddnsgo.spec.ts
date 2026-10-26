// ============================================================================
// ddns-go adapter · log/port 两个可选扩展槽 + 第三钮的方言事实
//
//  - 日志流：`ddnsgo:instance-log` 事件名逐字；只有带非空 line 的帧才进面板
//    （空帧放行=日志区幽灵空行）；pull 首屏回补固定取最近 200 行；
//  - 端口回执两分支词面语义分化：'pending' 承诺"下次启动生效"（运行实例未变，
//    谎报"已设为"会误导用户以为即时生效），其余词回"监听端口已设为 X"；
//  - running 横幅的 http:// 地址仅在 listenAddr 在册时拼出——缺字段绝不编造 URL；
//  - 第三钮「打开控制台」五态皆可点（未运行点击=先自动启动是本模块语义），
//    仅快照未载的空态禁用。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  Start: vi.fn(),
  Quit: vi.fn(),
  OpenConsole: vi.fn(),
  Logs: vi.fn(),
  GetListenPort: vi.fn(),
  SetListenPort: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/ddnsgo/ddnsgoservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createDdnsGoAdapter } from '../ddnsgo'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('ddnsgo · 扩展槽与投影', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('log 槽：事件名逐字、空帧/缺 line 丢弃、行内容原样透传；pull 固定回补最近 200 行', async () => {
    const adapter = createDdnsGoAdapter()
    const cb = vi.fn()
    adapter.log.subscribe(cb)
    expect(lastHandler().name).toBe('ddnsgo:instance-log')

    lastHandler().handler(null)
    lastHandler().handler({})
    lastHandler().handler({ line: '' })
    expect(cb).not.toHaveBeenCalled()
    lastHandler().handler({ line: '2026/10/26 更新成功 dns.example.com' })
    expect(cb).toHaveBeenCalledWith('2026/10/26 更新成功 dns.example.com')

    svc.Logs.mockResolvedValueOnce(['a', 'b'])
    expect(await adapter.log.pull()).toEqual(['a', 'b'])
    expect(svc.Logs).toHaveBeenCalledWith(200)
  })

  it('port.set 回执两分支：pending 明示"下次启动生效"不误报即时；其余词报即时生效', async () => {
    const adapter = createDdnsGoAdapter()
    svc.SetListenPort.mockResolvedValueOnce('pending')
    expect(await adapter.port.set(9876)).toEqual({
      message: '端口已设为 9876（当前运行实例不变，下次启动生效）',
    })
    expect(svc.SetListenPort).toHaveBeenCalledWith(9876)

    svc.SetListenPort.mockResolvedValueOnce('ok')
    expect(await adapter.port.set(8080)).toEqual({ message: '监听端口已设为 8080' })

    svc.GetListenPort.mockResolvedValueOnce(9876)
    expect(await adapter.port.get()).toBe(9876)
  })

  it('running 横幅只在 listenAddr 在册时拼 http:// 地址；缺字段不编 URL', () => {
    const adapter = createDdnsGoAdapter()
    const withAddr = adapter.banner!({ state: 'running', listenAddr: '127.0.0.1:9876' } as never, undefined as never)
    expect(withAddr?.tone).toBe('ok')
    expect(withAddr?.text).toContain('http://127.0.0.1:9876')
    const bare = adapter.banner!({ state: 'running', listenAddr: null } as never, undefined as never)
    expect(bare?.text).not.toContain('http://')
  })

  it('三钮语义：Start/OpenConsole 回执透传；「打开控制台」未运行也可点（点击自动拉起），空态才禁用', async () => {
    const adapter = createDdnsGoAdapter()
    expect(adapter.openConsole.disabledFor!('stopped')).toBe(false)
    expect(adapter.openConsole.disabledFor!('failed')).toBe(false)
    expect(adapter.openConsole.disabledFor!('external')).toBe(false)
    expect(adapter.openConsole.disabledFor!('')).toBe(true)

    svc.Start.mockResolvedValueOnce({ message: 'ddns-go 已启动' })
    expect(await adapter.control!.primary!.run()).toEqual({ message: 'ddns-go 已启动' })
    svc.OpenConsole.mockResolvedValueOnce({ message: '控制台已打开' })
    expect(await adapter.openConsole.run()).toEqual({ message: '控制台已打开' })
  })
})
