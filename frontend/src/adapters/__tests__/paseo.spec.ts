// ============================================================================
// Paseo adapter · 「active 空=自动最新已装」方言的投影对账
//
//  - implicitActive：canonical 前缀判据（^\d+\.\d+\.\d+）容纳 0.8.0-beta.N
//    目录名、排除 imported- 等非常规目录——隐式使用版本只能落在规范版本目录；
//  - hint/metaLead 的三态口径同源：active 在册用 active，空则回退隐式最新，
//    再无则如实报「尚未安装」——引导行绝不引用一个不存在的版本；
//  - copy 词面族是面板可见事实：动词词「安装」（非"下载"）、verify/extract
//    合并词「校验并解压…」、失败前缀「安装失败: 」（⑥ 词源覆写回表）；
//  - paseoUpgradeAvailable 走 utils/version 单源比较：beta 抹平互认相等不
//    提示、imported-* 不可比不提示（旧字典序"永新"伪序的回归闸）。
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
  GetReleaseChannel: vi.fn(),
  SetReleaseChannel: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  OpenElectronDataDir: vi.fn(),
  OpenDaemonHome: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/paseo/paseoservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createPaseoAdapter, openPaseoDaemonHome, paseoUpgradeAvailable } from '../paseo'

describe('paseo · 隐式使用版本方言', () => {
  beforeEach(() => vi.clearAllMocks())

  it('implicitActive：首个规范目录即隐式使用版本；beta 目录可为候选、imported- 永不入选，无候选回空串', () => {
    const adapter = createPaseoAdapter()
    const impl = adapter.versions.implicitActive!
    expect(impl([{ version: '0.9.0' }, { version: '0.8.0-beta.2' }] as never)).toBe('0.9.0')
    expect(impl([{ version: 'imported-1.0' }, { version: '0.8.0-beta.2' }] as never)).toBe('0.8.0-beta.2')
    expect(impl([{ version: 'imported-1.0' }] as never)).toBe('')
    expect(impl([] as never)).toBe('')
  })

  it('hint 与 metaLead 同源三态：active 优先、空回退隐式、无版本不引用；安装词面诚实', () => {
    const adapter = createPaseoAdapter()
    const stopped = { state: 'stopped', version: '', pid: 0, error: '', startedAt: '' } as never

    const byActive = adapter.hint!(stopped, { installed: [], releases: [], active: '0.7.0' } as never)
    expect(byActive).toContain('Paseo 0.7.0')
    const byImplicit = adapter.hint!(stopped, { installed: [{ version: '0.9.0' }], releases: [], active: '' } as never)
    expect(byImplicit).toContain('Paseo 0.9.0')
    const none = adapter.hint!(stopped, { installed: [{ version: 'imported-x' }], releases: [], active: '' } as never)
    expect(none).toContain('尚未安装') // 没有可信版本号时宁可不引用

    expect(adapter.copy!.metaLead!({ installed: [{ version: '0.9.0' }], releases: [], active: '0.7.0' } as never)).toBe(
      '使用版本 0.7.0',
    )
    expect(adapter.copy!.metaLead!({ installed: [{ version: '0.9.0' }], releases: [], active: '' } as never)).toBe(
      '使用版本 自动最新（0.9.0）',
    )
    expect(adapter.copy!.metaLead!({ installed: [], releases: [], active: '' } as never)).toBe('使用版本 未安装')
  })

  it('面板词面族覆写回表：动词「安装」、verify/extract 合并词、下载失败前缀；daemon 数据钮打 OpenDaemonHome', () => {
    const adapter = createPaseoAdapter()
    expect(adapter.copy!.downloadLabel!({ installedCount: 1 } as never)).toBe('安装')
    expect(adapter.copy!.stageWord!('verify', undefined as never)).toBe('校验并解压…')
    expect(adapter.copy!.stageWord!('extract', undefined as never)).toBe('校验并解压…')
    expect(adapter.copy!.stageWord!('downloading', undefined as never)).toBe('') // 其余进行态回空走面板 message 透出
    expect(adapter.copy!.errorPrefix!.download).toBe('安装失败: ')
    expect(adapter.copy!.activeBadge).toBe('使用版本')

    void openPaseoDaemonHome()
    expect(svc.OpenDaemonHome).toHaveBeenCalledTimes(1)

    // 升级谓词走 utils/version 单源：数值核心比较，beta 互认相等与不可比一律不提示
    expect(paseoUpgradeAvailable('0.8.0', '0.9.0')).toBe(true)
    expect(paseoUpgradeAvailable('0.9.0', '0.9.0')).toBe(false)
    expect(paseoUpgradeAvailable('0.9.0-beta.1', '0.9.0')).toBe(false) // 核心抹平互认
    expect(paseoUpgradeAvailable('imported-abc', '0.9.0')).toBe(false) // 不可比≠落后（旧字典序"永新"病灶）
  })

  it('setActive 回执「下次启动将使用」+ activeVersion 高亮位；remove 确认闸零 RPC', async () => {
    const adapter = createPaseoAdapter()
    svc.SetActiveVersion.mockResolvedValueOnce('0.8.0-beta.2')
    expect(await adapter.versions.setActive!('0.8.0-beta.2')).toEqual({
      message: '下次启动将使用 Paseo 0.8.0-beta.2',
      activeVersion: '0.8.0-beta.2',
    })

    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove({ version: '0.8.0' } as never)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    ui.confirm.mockResolvedValueOnce(true)
    expect(await adapter.versions.remove({ version: '0.8.0' } as never)).toEqual({
      message: '已卸载 0.8.0',
      reloadVersions: true,
    })
  })
})
