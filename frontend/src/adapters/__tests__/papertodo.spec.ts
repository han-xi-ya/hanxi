// ============================================================================
// PaperTodo adapter · 变体单源与 0/1 版本区投影（variant 槽首个接线者）
//
//  - variantName 词表：仅 'no-runtime' 判「精简版」，null/未知一律「完整版」
//    ——视图变体卡与回执话术共用此单源；
//  - listReleases 的 size 按当前变体偏好投影：该变体资产缺席回 0（面板永
//    不见 undefined）；偏好读取单飞——连刷远程表只触一次 GetVariant；
//  - download 已装词携带变体中文名（`版本 X（精简版）已安装`——用户要知道
//    的是"哪个变体已装"）；RPC 实参带上归一后的变体；
//  - variant.set 词表外值归一为 self-contained（脏值不下后端），且 set 成功
//    即事实：缓存跟进，后续 get 不再重探；
//  - 快照合流 honesty：GetRuntimeStatus 探空时 runtime 落 null 而非 undefined
//    （契约 PaperSnapshot.runtime 非可选，事件路同样带出）；listInstalled
//    退化为 0/1——版本为空的"在册记录"投影为未安装，方言字段（variant/
//    hasData/assetSha256）不泄进共享契约记录，经 installedInfo 钩子另供。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  GetInstalledVersion: vi.fn(),
  ListReleases: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveInstalled: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  HidePapers: vi.fn(),
  GetVariant: vi.fn(),
  SetVariant: vi.fn(),
  GetRuntimeStatus: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/papertodo/papertodoservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createPaperTodoAdapter, variantName } from '../papertodo'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('papertodo · 变体单源与投影', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('listReleases 按当前变体投影 size：资产缺席回 0；顶层 null 回 []；偏好读取单飞', async () => {
    svc.GetVariant.mockResolvedValue('no-runtime')
    svc.ListReleases.mockResolvedValue([
      { version: '1.1.0', published: '2026-10-01T00:00:00Z', selfContained: { size: 71000000 }, noRuntime: { size: 2400000 } },
      { version: '1.0.0', published: '2026-09-01T00:00:00Z', selfContained: { size: 70000000 } },
    ])
    const adapter = createPaperTodoAdapter()
    const rows = (await adapter.versions.listReleases()) ?? []
    expect(rows.map((r) => r.size)).toEqual([2400000, 0]) // 无 no-runtime 资产的行不谎报体积
    await adapter.versions.listReleases()
    expect(svc.GetVariant).toHaveBeenCalledTimes(1) // 连刷远程表不重复探偏好

    svc.GetVariant.mockResolvedValue('self-contained')
    const full = createPaperTodoAdapter()
    svc.ListReleases.mockResolvedValueOnce(null)
    expect(await full.versions.listReleases()).toEqual([])
  })

  it('download 已装词携带变体中文名且 RPC 实参带偏好；remove 回执承诺与确认框一致', async () => {
    // 变体词表：仅 'no-runtime' 判精简版，null/未知一律完整版（回执话术共用此单源）
    expect(variantName('no-runtime')).toBe('精简版')
    expect(variantName('self-contained')).toBe('完整版')
    expect(variantName(null)).toBe('完整版')
    expect(variantName('乱七八糟')).toBe('完整版')

    svc.GetVariant.mockResolvedValue('no-runtime')
    const adapter = createPaperTodoAdapter()

    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    expect(await adapter.versions.download({ version: '1.1.0' } as never)).toEqual({
      message: '版本 1.1.0（精简版）已安装',
      reloadVersions: true,
    })
    expect(svc.DownloadVersion).toHaveBeenCalledWith('1.1.0', 'no-runtime')
    svc.DownloadVersion.mockResolvedValueOnce('started')
    expect(await adapter.versions.download({ version: '1.1.0' } as never)).toEqual({})

    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove({ version: '1.1.0' } as never)).toEqual({})
    expect(svc.RemoveInstalled).not.toHaveBeenCalled()
    ui.confirm.mockResolvedValueOnce(true)
    expect(await adapter.versions.remove({ version: '1.1.0' } as never)).toEqual({
      message: '已卸载 PaperTodo（便签数据已保留）',
      reloadVersions: true,
    })
  })

  it('variant.set 词表外值归一 self-contained 再下后端；set 成功缓存跟进，get 不重探', async () => {
    svc.GetVariant.mockResolvedValue('no-runtime')
    const adapter = createPaperTodoAdapter()
    expect(await adapter.variant.get()).toBe('no-runtime')
    expect(svc.GetVariant).toHaveBeenCalledTimes(1)

    svc.SetVariant.mockResolvedValue(undefined)
    const res = await adapter.variant.set('beta-channel') // 脏值：非变体词表取值
    expect(svc.SetVariant).toHaveBeenCalledWith('self-contained')
    expect(res).toEqual({
      message: '下载变体已切换为完整版（下次下载生效）',
      reloadVersions: true,
    })
    expect(await adapter.variant.get()).toBe('self-contained')
    expect(svc.GetVariant).toHaveBeenCalledTimes(1) // 缓存即事实，不再回读
  })

  it('runtime 合流永不为 undefined（探空落 null，事件路同带）；listInstalled 0/1 投影不泄方言', async () => {
    svc.GetRuntimeStatus.mockResolvedValue(null)
    svc.GetStatus.mockResolvedValueOnce({ state: 'running', version: '1.1.0', pid: 3, error: '', startedAt: '' })
    const adapter = createPaperTodoAdapter()
    const snap = await adapter.getStatus()
    expect(snap).not.toBeNull()
    expect(snap!.runtime).toBeNull() // 契约字段必有值：视图拿不到 undefined

    const cb = vi.fn()
    adapter.subscribeInstanceState!(cb)
    lastHandler().handler({ state: 'stopped', version: '', pid: 0, error: '', startedAt: '' })
    expect(cb.mock.calls[0][0].runtime).toBeNull()

    svc.GetInstalledVersion.mockResolvedValueOnce({
      version: '1.1.0', exePath: 'D:\\p\\PaperTodo.exe', dir: 'D:\\p', size: 71000000,
      installedAt: '2026-10-01', isImport: false, source: '',
      variant: 'self-contained', hasData: true, assetSha256: 'ab12',
    })
    const rows = (await adapter.versions.listInstalled()) ?? []
    expect(rows).toHaveLength(1)
    expect(Object.keys(rows[0]).sort()).toEqual(
      ['dir', 'exePath', 'installedAt', 'isImport', 'size', 'source', 'version'].sort(),
    ) // 方言字段经 installedInfo 另供，不混入共享契约记录
    expect(adapter.installedInfo()!.hasData).toBe(true)

    svc.GetInstalledVersion.mockResolvedValueOnce({ version: '', exePath: '', dir: '', size: 0, installedAt: '', variant: null, hasData: false })
    expect(await adapter.versions.listInstalled()).toEqual([]) // 版本为空的在册记录=未安装
    expect(adapter.installedInfo()).toBeNull()
  })
})
