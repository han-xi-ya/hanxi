// ============================================================================
// Keyviz adapter · MSI 提取族 + "无唤窗语义"方言的核验（薄测）
//
//  - 主钮动词是 StartKeyviz（非家族常见的 OpenWindow）：run 必须打到该 RPC 上，
//    且 running 态即禁用——Keyviz 驻托盘无可唤窗口，"打开窗口"家族件的
//    running 可点方言在此不成立（照抄家族形制=点击落空）；
//  - extras 无快捷方式条目：上游没有 CreateDesktopShortcut 能力，条目缺席
//    即 SharedExtrasCard 自动隐身（声明有而调不存在的 RPC 才是错）；
//  - 随关关闭档话术点名 Keyviz（家族通用词"该工具"在此被覆写为具名）。
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
  StartKeyviz: vi.fn(),
  Quit: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/keyviz/keyvizservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createKeyvizAdapter } from '../keyviz'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('keyviz · 无唤窗方言与能力缺席', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('主钮启动动词落 StartKeyviz 并透传回执；running/starting 双态禁用（无窗口可唤）', async () => {
    const adapter = createKeyvizAdapter()
    svc.StartKeyviz.mockResolvedValueOnce({ message: 'Keyviz 已启动' })
    expect(await adapter.control!.primary!.run()).toEqual({ message: 'Keyviz 已启动' })
    expect(svc.StartKeyviz).toHaveBeenCalledTimes(1)

    expect(adapter.control!.primary!.disabledFor!('running')).toBe(true)
    expect(adapter.control!.primary!.disabledFor!('starting')).toBe(true)
    expect(adapter.control!.primary!.disabledFor!('stopped')).toBe(false)
    expect(adapter.control!.primary!.titleFor!('running')).toBe('实例已在运行')
    expect(adapter.control!.primary!.cssClass).toBe('btn-primary')
    expect(adapter.control!.quit!.disabledFor!('external')).toBe(false)
  })

  it('事件名 keyviz: 前缀逐字；进度单键形投影与畸形帧丢弃', async () => {
    const adapter = createKeyvizAdapter()
    const cb = vi.fn()
    adapter.subscribeInstanceState(vi.fn())
    expect(lastHandler().name).toBe('keyviz:instance-state')
    adapter.subscribeProgress(cb)
    expect(lastHandler().name).toBe('keyviz:version-download')
    lastHandler().handler({ version: '1.1.1', stage: 'downloading', done: 1, total: 3, message: '下载中' })
    expect(cb).toHaveBeenCalledWith({ key: '1.1.1', stage: 'downloading', done: 1, total: 3, message: '下载中' })
    cb.mockClear()
    lastHandler().handler(null)
    expect(cb).not.toHaveBeenCalled()

    svc.SetActiveVersion.mockResolvedValueOnce('1.1.1')
    expect(await adapter.versions.setActive!('1.1.1')).toEqual({
      message: '已将 1.1.1 设为使用版本',
      activeVersion: '1.1.1',
    })
  })

  it('extras 能力缺席如实：无快捷方式条目；数据目录走 OpenConfigDir；随关关闭词点名 Keyviz', async () => {
    const adapter = createKeyvizAdapter()
    expect(adapter.extras?.shortcut).toBeUndefined() // 上游无 CreateDesktopShortcut，条目必须缺席而非空转
    await adapter.extras!.dataDir!.open()
    expect(svc.OpenConfigDir).toHaveBeenCalledTimes(1)

    svc.SetFollowOnExit.mockResolvedValueOnce(undefined)
    expect(await adapter.extras!.followOnExit!.set(false)).toEqual({
      message: '已关闭：Hanxi 退出不影响该工具，Keyviz 继续独立运行（下次启动生效）',
    })
    expect(await adapter.versions.download({ version: '1.1.1' } as never)).toEqual({})
  })
})
