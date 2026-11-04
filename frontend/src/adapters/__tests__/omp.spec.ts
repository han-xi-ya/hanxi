// ============================================================================
// omp adapter · 官方安装通道（版本树拆除后动作面锁四件事）
//
//  - 动词对号入座：install/update/uninstall 各打各 RPC——打反不会报错只会静默
//    做错事（如 uninstall 误打 InstallOfficial），用例锁调用面；
//  - 取消闸：卸载确认框否决零 RPC（删目录+改 PATH 是危险动作）；
//  - 回执词表：in-progress 如实转告不静默；started 不触发复刷（终态归事件流）、
//    同步卸载回执触发复刷——两种异步语义的分界在本件；
//  - 事件名逐字 'omp:official-op'，载荷直传不做归一化（非下载票据形制）。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  InstallOfficial: vi.fn(),
  UpdateOfficial: vi.fn(),
  UninstallOfficial: vi.fn(),
  OpenDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/omp/ompservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))

import { createOmpAdapter } from '../omp'

beforeEach(() => {
  events.length = 0
  vi.clearAllMocks()
})

describe('omp · 官方三动词调用面与回执', () => {
  it('install/update 各打各 RPC；started 回执"后台进行"且不触发复刷', async () => {
    svc.InstallOfficial.mockResolvedValue('started')
    svc.UpdateOfficial.mockResolvedValue('started')
    const adapter = createOmpAdapter()

    const a = await adapter.installOfficial()
    expect(svc.InstallOfficial).toHaveBeenCalledTimes(1)
    expect(svc.UpdateOfficial).not.toHaveBeenCalled()
    expect(a.reloadVersions).toBeFalsy()
    expect(a.message).toContain('后台')

    const b = await adapter.updateOfficial()
    expect(svc.UpdateOfficial).toHaveBeenCalledTimes(1)
    expect(b.message).toContain('后台')
  })

  it('撞车语义：后端报 in-progress 时如实转告"已有一趟在跑"，不静默', async () => {
    svc.UpdateOfficial.mockResolvedValue('in-progress')
    const adapter = createOmpAdapter()
    const out = await adapter.updateOfficial()
    expect(out.message).toContain('已有一趟')
    expect(out.reloadVersions).toBeFalsy()
  })

  it('uninstall：确认框否决时零 RPC（危险动作取消不得落地）', async () => {
    ui.confirm.mockResolvedValue(false)
    const adapter = createOmpAdapter()
    const out = await adapter.uninstallOfficial()
    expect(ui.confirm).toHaveBeenCalled()
    expect(svc.UninstallOfficial).not.toHaveBeenCalled()
    expect(out).toEqual({})
  })

  it('uninstall：确认后透传后端中文回执并触发复刷（同步语义）', async () => {
    ui.confirm.mockResolvedValue(true)
    svc.UninstallOfficial.mockResolvedValue('已删除 C:/Users/x/AppData/Local/omp；已从用户 PATH 剔除该目录并广播')
    const adapter = createOmpAdapter()
    const out = await adapter.uninstallOfficial()
    expect(out.message).toContain('PATH')
    expect(out.reloadVersions).toBe(true)
  })

  it('事件名逐字锁定 omp:official-op，订阅即透传载荷', () => {
    const adapter = createOmpAdapter()
    const cb = vi.fn()
    adapter.subscribeOfficialOp(cb)
    expect(events[events.length - 1]!.name).toBe('omp:official-op')
    const p = { op: 'install', stage: 'done', message: 'ok' }
    events[events.length - 1]!.handler(p)
    expect(cb).toHaveBeenCalledWith(p)
  })
})
