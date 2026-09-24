import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRAMMapAdapter } from '../rammap'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  OpenDir: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  ImportLocal: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  OfficialSiteURL: vi.fn(),
  OpenOfficialSite: vi.fn(),
  ElevationStatus: vi.fn().mockResolvedValue({ requiresElevation: true, hostElevated: false, executionLevel: 'requireAdministrator' }),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/rammap/rammapservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

describe('createRAMMapAdapter', () => {
  beforeEach(() => vi.clearAllMocks())

  it('六态词表收口：quitting/external 一级公民，未知态兜底未托管', () => {
    const adapter = createRAMMapAdapter()
    expect(adapter.stateText?.({ state: 'quitting' } as never)).toBe('正在退出')
    expect(adapter.stateText?.({ state: 'external' } as never)).toBe('外部实例运行中')
    expect(adapter.stateText?.({ state: 'who-knows' } as never)).toBe('本会话未托管')
    expect(adapter.statusTone?.({ state: 'quitting' } as never)).toBe('starting')
  })

  it('外部退出 force-free 直通：不弹确认框、单次 RPC、透终态文案', async () => {
    const adapter = createRAMMapAdapter()
    svc.Quit.mockResolvedValueOnce({
      action: '', method: 'forced', external: true, message: '已强制结束',
      stopped: true, forced: true, closeRequested: false,
    })
    const res = (await adapter.control!.quit!.run()) ?? {}
    expect(svc.Quit).toHaveBeenCalledTimes(1)
    // force-free 档核心事实：绝不像会话类（WindTerm/Termora）弹确认
    expect(ui.confirm).not.toHaveBeenCalled()
    expect(res.message).toBe('已强制结束')
  })

  it('单独提权启动的外部实例：提示不属于 Hanxi 管控，退出不再调用 Quit RPC', async () => {
    const adapter = createRAMMapAdapter()
    svc.OpenWindow.mockResolvedValueOnce({
      action: 'started-external-elevated', external: true, elevated: true, managed: false,
      canQuit: false, launchMode: 'external-elevated', message: '已单独提权启动 RAMMap',
    })
    const primary = (await adapter.control!.primary!.run()) ?? {}
    expect(primary.message).toContain('已单独提权')
    const banner = adapter.banner?.({ state: 'running' } as never, undefined as never)
    expect(banner?.text).toContain('外部实例')
    const quit = (await adapter.control!.quit!.run()) ?? {}
    expect(quit.message).toContain('窗口内关闭')
    expect(svc.Quit).not.toHaveBeenCalled()
  })
  it('提权预告：装载即查 ElevationStatus，未提权引导行含"管理员"指引', async () => {
    const adapter = createRAMMapAdapter()
    expect(svc.ElevationStatus).toHaveBeenCalledTimes(1)
    await new Promise((r) => setTimeout(r, 0)) // 等 mock promise 落地 hostElevated=false
    const hint = adapter.hint?.({ state: 'stopped' } as never, undefined as never) ?? ''
    expect(hint).toContain('管理员')
  })

  it('已提权宿主引导行不再显提权预告（仅未提权态预告）', async () => {
    svc.ElevationStatus.mockResolvedValueOnce({ requiresElevation: true, hostElevated: true })
    const adapter = createRAMMapAdapter()
    await new Promise((r) => setTimeout(r, 0))
    const hint = adapter.hint?.({ state: 'stopped' } as never, undefined as never) ?? ''
    expect(hint).not.toContain('管理员')
  })

  it('版本披露诚实：无官方摘要降级三层、日期版模型、仅转链进 metaHints', () => {
    const adapter = createRAMMapAdapter()
    const joined = (adapter.copy?.metaHints ?? []).join(' ')
    expect(joined).toContain('降级三层')
    expect(joined).toContain('日期')
    expect(joined).toContain('代下载')
  })
})
