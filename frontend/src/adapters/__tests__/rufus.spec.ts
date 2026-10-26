// ============================================================================
// Rufus adapter · 单文件落位方言与一次性工具的能力缺席（薄测）
//
//  - stageWord 词族：resolve/verify/install 三档合词「校验落位…」，extract
//    刻意回空——rufus-X.Yp.exe 是单文件落位而非解压，若照抄 zip 族给 extract
//    配词即谎报工艺（面板对空词回落到 message 透出）；
//  - extras 缺席位：无常驻工具故无快捷方式/数据目录条目（ManagedExtrasCard
//    缺项自动隐身——声明了就会调不存在的 RPC）；
//  - quit 的 external 指引给"直接在其窗口关闭"（Rufus 无托盘），running 态
//    横幅必须携带写盘中断警示（半成品启动盘是不可逆代价，警示前置）。
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
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/rufus/rufusservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createRufusAdapter } from '../rufus'

describe('rufus · 单文件方言与缺席声明', () => {
  beforeEach(() => vi.clearAllMocks())

  it('stageWord 三档合词「校验落位…」且 extract 无词（落位≠解压）；extras 无快捷方式/数据目录条目', () => {
    const adapter = createRufusAdapter()
    expect(adapter.copy!.stageWord!('resolve', undefined as never)).toBe('校验落位…')
    expect(adapter.copy!.stageWord!('verify', undefined as never)).toBe('校验落位…')
    expect(adapter.copy!.stageWord!('install', undefined as never)).toBe('校验落位…')
    expect(adapter.copy!.stageWord!('extract', undefined as never)).toBe('')
    expect(adapter.copy!.importLabel).toBe('⇥ 导入本地 exe')

    expect(adapter.extras?.shortcut).toBeUndefined()
    expect(adapter.extras?.dataDir).toBeUndefined()
    expect(adapter.extras?.repo).toBeDefined()
  })

  it('remove 确认闸零 RPC、确认走共享卸载词；quit 外部态指引"直接关窗"；running 横幅带写盘中断警示', async () => {
    const adapter = createRufusAdapter()

    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove({ version: '4.15' } as never)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    ui.confirm.mockResolvedValueOnce(true)
    expect(await adapter.versions.remove({ version: '4.15' } as never)).toEqual({
      message: '已卸载 4.15',
      reloadVersions: true,
    })

    expect(adapter.control!.quit!.titleFor!('external')).toBe('外部实例请直接在其窗口关闭')
    const running = adapter.banner!({ state: 'running' } as never, undefined as never)
    expect(running?.tone).toBe('ok')
    expect(running?.text).toContain('切勿') // 写盘中途终止的不可逆代价必须前置告知
    expect(adapter.hint!({ state: 'stopped' } as never, undefined as never)).toContain('管理员')
  })
})
