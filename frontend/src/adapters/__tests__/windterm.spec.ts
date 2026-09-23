import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createWindTermAdapter } from '../windterm'

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
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/windterm/windtermservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

describe('createWindTermAdapter', () => {
  beforeEach(() => vi.clearAllMocks())

  it('六态词表收口：quitting/external 一级公民，未知态兜底未托管', () => {
    const adapter = createWindTermAdapter()
    expect(adapter.stateText?.({ state: 'quitting' } as never)).toBe('正在退出')
    expect(adapter.stateText?.({ state: 'external' } as never)).toBe('外部实例运行中')
    expect(adapter.stateText?.({ state: 'who-knows' } as never)).toBe('本会话未托管')
    expect(adapter.statusTone?.({ state: 'quitting' } as never)).toBe('starting')
  })

  it('confirm-force 往返：首入 confirm-required → 用户拒绝则不重入、如实回取消', async () => {
    const adapter = createWindTermAdapter()
    svc.Quit.mockResolvedValueOnce({ action: 'confirm-required', method: 'confirm-required', external: true, risk: 'R', message: 'M', stopped: false, forced: false, closeRequested: false })
    ui.confirm.mockResolvedValueOnce(false)

    const res = (await adapter.control!.quit!.run()) ?? {}
    expect(svc.Quit).toHaveBeenCalledTimes(1)
    expect(svc.Quit).toHaveBeenCalledWith(false)
    expect(res.message).toContain('已取消退出')
  })

  it('confirm-force 往返：同意后携 confirm=true 重入执行并透出终态文案', async () => {
    const adapter = createWindTermAdapter()
    svc.Quit
      .mockResolvedValueOnce({ action: 'confirm-required', method: 'confirm-required', external: true, risk: '强杀风险', message: 'M', stopped: false, forced: false, closeRequested: false })
      .mockResolvedValueOnce({ action: '', method: 'forced', external: true, message: '已按你的确认强制结束', stopped: true, forced: true, closeRequested: false })
    ui.confirm.mockResolvedValueOnce(true)

    const res = (await adapter.control!.quit!.run()) ?? {}
    expect(svc.Quit).toHaveBeenNthCalledWith(1, false)
    expect(svc.Quit).toHaveBeenNthCalledWith(2, true)
    // 确认框必须呈现后端风险原文（授权知情面）
    expect(ui.confirm.mock.calls[0][0].description).toBe('强杀风险')
    expect(res.message).toBe('已按你的确认强制结束')
  })

  it('自有实例退出直通：无 confirm-required 不弹框', async () => {
    const adapter = createWindTermAdapter()
    svc.Quit.mockResolvedValueOnce({ action: '', method: 'close-request', external: false, message: '已退出', stopped: true, forced: false, closeRequested: true })

    const res = (await adapter.control!.quit!.run()) ?? {}
    expect(svc.Quit).toHaveBeenCalledTimes(1)
    expect(ui.confirm).not.toHaveBeenCalled()
    expect(res.message).toBe('已退出')
  })

  it('版本披露诚实：无官方摘要降级三层与许可备忘进 metaHints', () => {
    const adapter = createWindTermAdapter()
    const hints = adapter.copy?.metaHints ?? []
    expect(hints.join(' ')).toContain('未发布官方哈希')
    expect(hints.join(' ')).toContain('降级三层')
    expect(hints.join(' ')).toContain('LICENSE')
  })
})
