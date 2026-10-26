// ============================================================================
// MangoDisk adapter · 动态主钮词 + 完整性压过运行态（编排面回契约单源）
//
//  - primary.label 是 state 纯函数：running/external→「打开窗口」，其余→
//    「启动 MangoDisk」——视图经 store.primaryLabel 取词不自持词表，钮面
//    翻转即此函数的外观察果；
//  - primary.run 成功回执必带 reloadVersions:true（冷启动可能激活自动版本，
//    ⑥ 双复刷承诺）；
//  - banner 优先级：当前版本的 drifted/invalid 完整性压过 running 绿条——
//    "在跑但文件不对"必须先于"在跑"被看见；当前版本判定 s.version 空则回退
//    ctx.active（两源皆空时不猜测，照常走状态横幅）；
//  - quit 对 external 禁用（本模块不强制终止外部实例，钮位即边界声明）。
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
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/mangodisk/mangodiskservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createMangoDiskAdapter } from '../mangodisk'

type LabelFn = (state: string, snap: unknown) => string

describe('mangodisk · 动态词与完整性优先级', () => {
  beforeEach(() => vi.clearAllMocks())

  it('主钮 label 纯函数翻转词面；run 带 reloadVersions 双复刷；quit 不越权管外部实例', async () => {
    const adapter = createMangoDiskAdapter()
    const label = adapter.control!.primary!.label as unknown as LabelFn
    expect(typeof adapter.control!.primary!.label).toBe('function')
    expect(label('running', null)).toBe('打开窗口')
    expect(label('external', null)).toBe('打开窗口')
    expect(label('stopped', null)).toBe('启动 MangoDisk')
    expect(label('failed', null)).toBe('启动 MangoDisk')

    svc.OpenWindow.mockResolvedValueOnce({ message: '已唤起窗口' })
    expect(await adapter.control!.primary!.run()).toEqual({ message: '已唤起窗口', reloadVersions: true })

    expect(adapter.control!.quit!.disabledFor!('external')).toBe(true)
    expect(adapter.control!.quit!.disabledFor!('running')).toBe(false)
  })

  it('drifted/invalid 压过 running 绿条：文案直取 integrityNote，tone 分琥珀/红', () => {
    const adapter = createMangoDiskAdapter()
    const ctx = (rows: Array<{ version: string; integrity: string; integrityNote: string }>, active = '') => ({
      installed: rows,
      releases: [],
      active,
    }) as never

    const drifted = adapter.banner!(
      { state: 'running', version: '2.0.0' } as never,
      ctx([{ version: '2.0.0', integrity: 'drifted', integrityNote: '本体文件与官方基线不一致' }]),
    )
    expect(drifted).toEqual({ tone: 'warn', text: '本体文件与官方基线不一致' })

    const invalid = adapter.banner!(
      { state: 'running', version: '2.0.0' } as never,
      ctx([{ version: '2.0.0', integrity: 'invalid', integrityNote: '本体文件无法核验' }]),
    )
    expect(invalid!.tone).toBe('error')
  })

  it('s.version 空回退 ctx.active 判当前版本；完整性 ok 时照常落 running 绿条（不谎报漂移）', () => {
    const adapter = createMangoDiskAdapter()
    const rows = [{ version: '1.8.0', integrity: 'drifted', integrityNote: '外部改过本体' }]

    const viaActive = adapter.banner!(
      { state: 'running', version: '' } as never,
      { installed: rows, releases: [], active: '1.8.0' } as never,
    )
    expect(viaActive).toEqual({ tone: 'warn', text: '外部改过本体' })

    const ok = adapter.banner!(
      { state: 'running', version: '2.0.0' } as never,
      { installed: [{ version: '2.0.0', integrity: 'ok', integrityNote: '' }], releases: [], active: '' } as never,
    )
    expect(ok!.tone).toBe('ok')

    // 漂移版本不是"当前"版本时不越位警示：running 绿条照常
    const other = adapter.banner!(
      { state: 'running', version: '2.0.0' } as never,
      { installed: [{ version: '1.8.0', integrity: 'drifted', integrityNote: '旧版漂移' }], releases: [], active: '' } as never,
    )
    expect(other!.tone).toBe('ok')
  })
})
