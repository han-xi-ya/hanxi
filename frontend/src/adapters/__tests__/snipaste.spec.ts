import { describe, expect, it, vi } from 'vitest'
import { createSnipasteAdapter } from '../snipaste'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  OpenDir: vi.fn(),
  Launch: vi.fn(),
  Quit: vi.fn(),
  ImportLocal: vi.fn(),
  OfficialSiteURL: vi.fn(),
  OpenOfficialSite: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/snipaste/snipasteservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))

describe('createSnipasteAdapter', () => {
  it('声明 custom 版本编排，避免共享 store 接管 Snipaste 事实核验票据', () => {
    const adapter = createSnipasteAdapter(vi.fn())
    expect(adapter.versions.orchestration).toBe('custom')
  })

  it('保留本会话所有权、quitting 与 external 状态文案', () => {
    const adapter = createSnipasteAdapter(vi.fn())
    expect(adapter.stateText?.({ state: 'stopped' } as never)).toBe('本会话未托管')
    expect(adapter.stateText?.({ state: 'running' } as never)).toBe('本会话实例运行中')
    expect(adapter.stateText?.({ state: 'quitting' } as never)).toBe('正在退出')
    // W2/N1：external 升为一级状态（N3 分档接管），不再走"未托管"兜底
    expect(adapter.stateText?.({ state: 'external' } as never)).toBe('外部实例运行中')
    expect(adapter.stateText?.({ state: 'unknown' } as never)).toBe('本会话未托管')
  })
})
