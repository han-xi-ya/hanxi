// ============================================================================
// Recordly adapter · NSIS 单目录覆盖式方言（版本互认/封锁/无设定版本位）
//
//  - versions.sameVersion 进共享面板做互认单源：剥 v 前缀、抹 -beta 后缀的
//    数值核心相等即互认（tag 'v1.3.5' 与目录 '1.3.5'、PE 号 '1.2.0-beta.1'
//    与 '1.2.0'）；≥4 段不再被旧 3 段截断抹平（1.3.5.1 ≠ 1.3.5，漏报修正）；
//    imported-* 判不可比——旧实现的字典序"永新"伪序是横幅常亮病灶；
//  - recordlyUpgradeAvailable：相等/不可比一律 false，只有核心落后才提示；
//  - downloadBlock：running/starting 封锁覆盖安装并给指引，其余态放行；
//  - 契约缺席位：单目录覆盖式无 getActive/setActive（共享件据此不渲染
//    「使用中」位与「设为使用」钮）；hint/metaLead 引用已装版本或如实
//    报未安装；downloadLabel 随已装数翻转「安装/覆盖安装」。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
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
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/recordly/recordlyservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createRecordlyAdapter, recordlyUpgradeAvailable } from '../recordly'

describe('recordly · NSIS 覆盖式方言', () => {
  beforeEach(() => vi.clearAllMocks())

  it('sameVersion 互认账目：剥 v、抹 beta 互认；≥4 段不再抹平；imported-* 不可比', () => {
    const adapter = createRecordlyAdapter()
    const same = adapter.versions.sameVersion!
    expect(same('v1.3.5', '1.3.5')).toBe(true) // tag 与目录名剥前缀互认
    expect(same('1.2.0-beta.1', '1.2.0')).toBe(true) // beta 后缀抹平互认（刻意语义）
    expect(same('1.3.5.1', '1.3.5')).toBe(false) // PE 四段不再被 3 段截断抹平（漏报修正）
    expect(same('imported-legacy', '1.3.5')).toBe(false) // 不再产"imported 永新"字典序伪判
    expect(same('imported-legacy', 'imported-legacy')).toBe(true) // 精确串仍等值命中
    expect(same('1.2.0', '1.3.0')).toBe(false)
  })

  it('升级谓词：核心落后才真；相等、反超、不可比皆不亮警告条', () => {
    expect(recordlyUpgradeAvailable('1.2.0', '1.3.0')).toBe(true)
    expect(recordlyUpgradeAvailable('1.3.0', '1.3.0-beta.2')).toBe(false) // 核心抹平互认相等
    expect(recordlyUpgradeAvailable('1.4.0', '1.3.0')).toBe(false)
    expect(recordlyUpgradeAvailable('imported-x', '1.3.0')).toBe(false)
  })

  it('downloadBlock 运行态封锁覆盖安装；契约缺席位（无 getActive/setActive）如实不声明', () => {
    const adapter = createRecordlyAdapter()
    const block = adapter.versions.downloadBlock!
    expect(block('running')).toBe('请先退出运行中的 Recordly')
    expect(block('starting')).toBe('请先退出运行中的 Recordly')
    expect(block('stopped')).toBeNull()
    expect(block('external')).toBeNull()
    expect(adapter.versions.getActive).toBeUndefined()
    expect(adapter.versions.setActive).toBeUndefined()
  })

  it('词面与投影：metaLead/downloadLabel 随已装数分叉；hint 引用在册版本或如实报未装；已装回执走共享词', async () => {
    const adapter = createRecordlyAdapter()
    const withVer = { installed: [{ version: '1.3.5' }], releases: [], active: '' } as never
    const empty = { installed: [], releases: [], active: '' } as never

    expect(adapter.copy!.metaLead!(withVer)).toBe('当前托管 1.3.5')
    expect(adapter.copy!.metaLead!(empty)).toBe('当前托管 未安装')
    expect(adapter.copy!.downloadLabel!({ installedCount: 1 } as never)).toBe('覆盖安装')
    expect(adapter.copy!.downloadLabel!({ installedCount: 0 } as never)).toBe('安装')
    expect(adapter.copy!.stageWord!('install', undefined as never)).toBe('校验并静默安装…')
    expect(adapter.copy!.stageWord!('extract', undefined as never)).toBe('') // recordly 无 extract 档

    const stopped = { state: 'stopped', version: '', pid: 0, error: '', startedAt: '' } as never
    expect(adapter.hint!(stopped, withVer)).toContain('Recordly 1.3.5')
    expect(adapter.hint!(stopped, empty)).toContain('尚未安装')

    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    expect(await adapter.versions.download({ version: '1.3.5' } as never)).toEqual({
      message: '版本 1.3.5 已安装',
      reloadVersions: true,
    })
  })
})
