// ============================================================================
// LiteMonitor adapter · 第三路运行时探测与「打开位置」三源优先级
//
//  - GetRuntimeStatus 视图生命周期内单发：两次 getStatus 只触一次探测 RPC；
//    探测结果以快照扩展字段 hasDesktop8 合流（getStatus 与 instance-state
//    事件两路都并入——事件路漏合流=运行中警示闪断）；
//  - 探测失败静默：hasDesktop8 缺席（undefined）而非谎报 false——"探测不到"
//    与"探测到缺失"是两个事实，后者会向用户亮误警；
//  - listInstalled/getActive 顶层 null 线形折叠成 []/''（视图永不见 null）；
//  - 「打开位置」按 运行版本 > 活动版本 > 任一已装 的优先级解析目录；
//    无任何已装时 open 以失败回执抛出（共享 extras 卡无禁用位的兜底话术）。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  GetRuntimeStatus: vi.fn(),
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
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/litemonitor/litemonitorservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createLiteMonitorAdapter } from '../litemonitor'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

const stoppedSnap = { state: 'stopped', version: '', pid: 0, error: '', startedAt: '' }

describe('litemonitor · 运行时探测合流', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('探测单发：两次 getStatus 只触一次 GetRuntimeStatus；结果并入快照并随事件回流', async () => {
    const adapter = createLiteMonitorAdapter()
    svc.GetRuntimeStatus.mockResolvedValue({ hasDesktop8: true })
    svc.GetStatus.mockResolvedValue(stoppedSnap)

    const first = await adapter.getStatus()
    const second = await adapter.getStatus()
    expect(svc.GetRuntimeStatus).toHaveBeenCalledTimes(1)
    expect(first!.hasDesktop8).toBe(true)
    expect(second!.hasDesktop8).toBe(true)

    // 事件路同样合流：引擎快照本体不带 hasDesktop8，投影后必须带出缓存事实
    const cb = vi.fn()
    adapter.subscribeInstanceState!(cb)
    expect(lastHandler().name).toBe('litemonitor:instance-state')
    lastHandler().handler({ state: 'running', version: '1.3.0', pid: 9, error: '', startedAt: '' })
    expect(cb.mock.calls[0][0].hasDesktop8).toBe(true)
  })

  it('探测失败不谎报：hasDesktop8 缺席（undefined）而非 false；快照照常交付；null 线形折叠 []/\'\'', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const adapter = createLiteMonitorAdapter()
    svc.GetRuntimeStatus.mockRejectedValue(new Error('探测通道不可用'))
    svc.GetStatus.mockResolvedValueOnce(stoppedSnap)
    const snap = await adapter.getStatus()
    expect(snap).not.toBeNull()
    expect(snap!.hasDesktop8).toBeUndefined() // 「探测不到」≠「探测到缺失」，警示宁缺席不误警
    warnSpy.mockRestore()

    svc.ListInstalledVersions.mockResolvedValueOnce(null)
    expect(await adapter.versions.listInstalled()).toEqual([])
    svc.GetActiveVersion.mockResolvedValueOnce(null)
    expect(await adapter.versions.getActive!()).toBe('')
  })
})

describe('litemonitor · 「打开位置」三源优先级', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('活动版本命中优先于任一已装；运行快照再压过活动版本；无已装以失败回执兜底', async () => {
    const adapter = createLiteMonitorAdapter()
    svc.GetRuntimeStatus.mockResolvedValue({ hasDesktop8: true })
    svc.ListInstalledVersions.mockResolvedValue([
      { version: '1.0.0', dir: 'D:\\lm\\1.0.0' },
      { version: '2.0.0', dir: 'D:\\lm\\2.0.0' },
    ])
    svc.GetActiveVersion.mockResolvedValue('2.0.0')
    await adapter.versions.listInstalled()
    await adapter.versions.getActive!()

    await adapter.extras!.dataDir!.open()
    expect(svc.OpenDir).toHaveBeenLastCalledWith('D:\\lm\\2.0.0') // 活动版本源

    svc.GetStatus.mockResolvedValueOnce({ state: 'running', version: '1.0.0', pid: 5, error: '', startedAt: '' })
    await adapter.getStatus()
    await adapter.extras!.dataDir!.open()
    expect(svc.OpenDir).toHaveBeenLastCalledWith('D:\\lm\\1.0.0') // 运行版本压过活动版本

    const fresh = createLiteMonitorAdapter()
    svc.GetRuntimeStatus.mockResolvedValue(null)
    svc.OpenDir.mockClear()
    await expect(fresh.extras!.dataDir!.open()).rejects.toThrow('尚未安装任何版本')
    expect(svc.OpenDir).not.toHaveBeenCalled()
  })
})
