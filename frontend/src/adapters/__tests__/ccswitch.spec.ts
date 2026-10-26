// ============================================================================
// CC Switch adapter · 托管家族黄金样本的接线核验
//
// 本件无模块方言（managedCommon.spec 已锚定共享回执词面），因此这里断的是
// "ccswitch 确实以标准形接入了共享骨架"这一外部事实：
//  - 事件名 ccswitch:instance-state / ccswitch:version-download 逐字（错名=收不到推送）；
//  - 快照按引用交付、畸形帧丢弃；进度键 = version 单键最简形；
//  - remove 确认闸：取消必须零 RPC 零回执（toast 静默）；确认后 RemoveVersion
//    收到版本号本体（非整个记录对象）；
//  - importLocal 取消零 RPC；路径去空白后才交后端（用户粘贴带尾空格是常态）；
//  - 钮禁用矩阵与 external 态悬停指引（quit 对 external 可点=代发消息语义）。
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
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/ccswitch/ccswitchservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createCCSwitchAdapter } from '../ccswitch'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('ccswitch · 黄金样本接线', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('两路事件名逐字透传；快照按引用交付、空载荷丢弃；进度单键形 key=version', () => {
    const adapter = createCCSwitchAdapter()
    const snap = vi.fn()
    const cb = vi.fn()
    adapter.subscribeInstanceState!(snap)
    expect(lastHandler().name).toBe('ccswitch:instance-state')
    lastHandler().handler(null)
    expect(snap).not.toHaveBeenCalled()
    lastHandler().handler({ state: 'running' })
    expect(snap).toHaveBeenCalledWith({ state: 'running' })

    adapter.subscribeProgress(cb)
    expect(lastHandler().name).toBe('ccswitch:version-download')
    lastHandler().handler({ version: '2.1.0', stage: 'verify', done: 3, total: 9, message: '校验中' })
    expect(cb).toHaveBeenCalledWith({ key: '2.1.0', stage: 'verify', done: 3, total: 9, message: '校验中' })
    cb.mockClear()
    lastHandler().handler({ stage: 'error', done: 0, total: 0, message: '畸形帧' })
    expect(cb).not.toHaveBeenCalled()
  })

  it('版本动词回执分叉：setActive 高亮位、download 已装词、remove 确认闸零 RPC、import 去空白', async () => {
    const adapter = createCCSwitchAdapter()

    svc.SetActiveVersion.mockResolvedValueOnce('2.1.0')
    expect(await adapter.versions.setActive!('2.1.0')).toEqual({
      message: '已将 2.1.0 设为使用版本',
      activeVersion: '2.1.0',
    })

    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    expect(await adapter.versions.download({ version: '2.0.0' } as never)).toEqual({
      message: '版本 2.0.0 已安装',
      reloadVersions: true,
    })
    svc.DownloadVersion.mockResolvedValueOnce('started')
    expect(await adapter.versions.download({ version: '2.1.0' } as never)).toEqual({})

    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove({ version: '2.0.0' } as never)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled() // 取消=动作未发生的硬事实
    ui.confirm.mockResolvedValueOnce(true)
    expect(await adapter.versions.remove({ version: '2.0.0' } as never)).toEqual({
      message: '已卸载 2.0.0',
      reloadVersions: true,
    })
    expect(svc.RemoveVersion).toHaveBeenCalledWith('2.0.0')

    ui.prompt.mockResolvedValueOnce(null)
    expect(await adapter.versions.importLocal!()).toEqual({})
    expect(svc.ImportLocal).not.toHaveBeenCalled()
    ui.prompt.mockResolvedValueOnce('  C:\\Tools\\cc-switch  ')
    svc.ImportLocal.mockResolvedValueOnce({ version: '1.9.0' })
    expect(await adapter.versions.importLocal!()).toEqual({
      message: '已导入 CC Switch 1.9.0',
      reloadVersions: true,
    })
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\Tools\\cc-switch')
  })

  it('钮禁用矩阵与投影：primary 仅 starting 禁用；quit 对 external 可点且指引在位；banner/hint 三态互斥', async () => {
    const adapter = createCCSwitchAdapter()
    const primary = adapter.control!.primary!
    const quit = adapter.control!.quit!
    expect(primary.disabledFor!('starting')).toBe(true)
    expect(primary.disabledFor!('running')).toBe(false)
    expect(primary.titleFor!('running')).toBe('唤起窗口')
    expect(quit.disabledFor!('stopped')).toBe(true)
    expect(quit.disabledFor!('failed')).toBe(true)
    expect(quit.disabledFor!('external')).toBe(false)
    expect(quit.titleFor!('external')).toBe('外部实例请在 CC Switch 托盘退出')

    expect(adapter.banner!({ state: 'failed', error: '' } as never, undefined as never)).toEqual({
      tone: 'error',
      text: 'CC Switch 异常退出',
    })
    expect(adapter.banner!({ state: 'failed', error: '退出码 0xC000013A' } as never, undefined as never)?.text).toBe(
      '退出码 0xC000013A',
    )
    expect(adapter.banner!({ state: 'stopped' } as never, undefined as never)).toBeNull()
    expect(adapter.hint!({ state: 'running' } as never, undefined as never)).toBeNull()
    expect(adapter.hint!({ state: 'stopped' } as never, undefined as never)).toContain('~/.cc-switch')

    svc.OpenWindow.mockResolvedValueOnce({ message: '已启动 CC Switch' })
    expect(await primary.run()).toEqual({ message: '已启动 CC Switch' })
  })
})
