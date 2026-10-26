// ============================================================================
// FlClash adapter · 标准家族件的接入核验（薄测）
//
// 本件声明"前端编排无模块特例"，因此断言的外部事实即"确实按标准形接入"：
//  - 事件名 flclash:instance-state / flclash:version-download 逐字、单键形投影；
//  - remove 确认闸（取消零 RPC）、卸载/已装回执词走共享单源；
//  - quit 对 external 可点且悬停指引给出托盘出口（用户能照做的下一步）；
//    primary 唤起钮 running 态不禁用（与 keyviz/quicklook 启动语义分叉）。
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

vi.mock('../../../bindings/hanxi/internal/modules/flclash/flclashservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createFlClashAdapter } from '../flclash'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('flclash · 标准接线与闸口', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('事件名逐字（家族前缀 flclash:）；进度票据单键形、空 version 丢弃', () => {
    const adapter = createFlClashAdapter()
    const snap = vi.fn()
    adapter.subscribeInstanceState!(snap)
    expect(lastHandler().name).toBe('flclash:instance-state')
    lastHandler().handler(undefined)
    expect(snap).not.toHaveBeenCalled()

    const cb = vi.fn()
    adapter.subscribeProgress(cb)
    expect(lastHandler().name).toBe('flclash:version-download')
    lastHandler().handler({ version: '0.8.80', stage: 'extract', done: 4, total: 10, message: '解压中' })
    expect(cb).toHaveBeenCalledWith({ key: '0.8.80', stage: 'extract', done: 4, total: 10, message: '解压中' })
  })

  it('remove 确认闸零 RPC、回执走共享词；external 态 quit 可点且 title 给托盘出口；primary running 不禁', async () => {
    const adapter = createFlClashAdapter()

    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove({ version: '0.8.80' } as never)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    ui.confirm.mockResolvedValueOnce(true)
    expect(await adapter.versions.remove({ version: '0.8.80' } as never)).toEqual({
      message: '已卸载 0.8.80',
      reloadVersions: true,
    })

    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    expect(await adapter.versions.download({ version: '0.8.80' } as never)).toEqual({
      message: '版本 0.8.80 已安装',
      reloadVersions: true,
    })

    expect(adapter.control!.quit!.disabledFor!('external')).toBe(false)
    expect(adapter.control!.quit!.titleFor!('external')).toBe('外部实例请在 FlClash 托盘退出')
    expect(adapter.control!.primary!.disabledFor!('running')).toBe(false)
    expect(adapter.banner!({ state: 'external' } as never, undefined as never)?.tone).toBe('warn')
  })
})
