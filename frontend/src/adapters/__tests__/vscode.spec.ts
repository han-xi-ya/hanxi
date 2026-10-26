// ============================================================================
// VS Code adapter · 双形态复合键与确认闸（custom 编排的方言面）
//
//  - 进度复合键 `form:version`（两形态同名版本不串台）：订阅票据与面板行
//    反查（versions.progressKey）必须同源构造；form 缺失/词表外、version
//    缺失的畸形帧整体丢弃（幽灵票据会挂住面板行）；
//  - 共享 store 只承接便携链：listReleases 固定查 portable 并把行归一成
//    {version, size, published:'', form:'portable'} 三字段契约形；顶层 null
//    折叠成 []；
//  - 快照投影：聚合 Status 未载时事件一律丢弃（无底本不合成状态）；
//    installer 事件并入 installer 位、交付共享件的恒为 portable 快照；
//    getStatus 聚合为 null 时回 null（视图拿不到 undefined）；
//  - runtime.download 确认闸：首轮 force=false，回 'confirm-required' 才弹
//    确认、确认后第二 RPC 必须 force=true（闸不过=零第二次 RPC）；
//    'already-installed' 翻 reloadVersions；空 message 归一 undefined（静默）；
//  - setActive 词面报后端确认的生效版本（非请求版本），activeVersion 高亮位
//    + runtime 缓存同步。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  ListRemoteVersions: vi.fn(),
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

vi.mock('../../../bindings/hanxi/internal/modules/vscode/vscodeservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))
vi.mock('../../composables/usePolling', () => ({ usePolling: vi.fn() }))

import { createVSCodeAdapter, vscodeProgressKey } from '../vscode'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

const portableSnap = { state: 'running', version: '1.95.0', pid: 11, error: '', startedAt: '', form: 'portable' }

describe('vscode · 双形态方言面', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('复合键 form:version 订阅与行反查同源；form 缺失/词表外/无 version 的畸形帧整体丢弃', () => {
    expect(vscodeProgressKey('installer', '1.95.0')).toBe('installer:1.95.0')

    const { adapter } = createVSCodeAdapter()
    const cb = vi.fn()
    adapter.subscribeProgress(cb)
    expect(lastHandler().name).toBe('vscode:version-download')

    lastHandler().handler({ version: '1.95.0', form: 'installer', stage: 'downloading', done: 1, total: 2, message: '下载中' })
    expect(cb).toHaveBeenCalledWith({ key: 'installer:1.95.0', stage: 'downloading', done: 1, total: 2, message: '下载中' })
    cb.mockClear()
    lastHandler().handler(null)
    lastHandler().handler({ version: '1.95.0', stage: 'done', done: 1, total: 1, message: '' }) // 缺 form
    lastHandler().handler({ version: '1.95.0', form: 'insider', stage: 'done', done: 1, total: 1, message: '' }) // 词表外形态
    lastHandler().handler({ form: 'portable', stage: 'downloading', done: 0, total: 0, message: '' }) // 缺 version
    expect(cb).not.toHaveBeenCalled()

    // 面板行反查与票据同源：共享表行恒为便携链，反查键即 portable: 前缀
    expect(adapter.versions.progressKey!({ version: '1.95.0' } as never)).toBe('portable:1.95.0')
  })

  it('listReleases 只承接便携链：查询词 portable、null 顶层折叠 []、行归一三字段契约形', async () => {
    const { adapter } = createVSCodeAdapter()
    svc.ListRemoteVersions.mockResolvedValueOnce(null)
    expect(await adapter.versions.listReleases()).toEqual([])
    expect(svc.ListRemoteVersions).toHaveBeenLastCalledWith('portable')

    svc.ListRemoteVersions.mockResolvedValueOnce([
      { version: '1.95.0', size: 99, form: 'installer', notes: '方言列不进通用表' },
    ])
    const rows = await adapter.versions.listReleases()
    expect(rows).toEqual([{ version: '1.95.0', size: 99, published: '', form: 'portable' }])
  })

  it('聚合快照投影：未载事件丢弃；installer 事件并入 installer 位、交付恒为 portable；聚合 null 回 null', async () => {
    const { adapter, runtime } = createVSCodeAdapter()
    const cb = vi.fn()
    adapter.subscribeInstanceState(cb)
    expect(lastHandler().name).toBe('vscode:instance-state')

    // 无聚合底本：任何事件都不投影（不拿残缺当状态）
    lastHandler().handler({ ...portableSnap, state: 'stopped' })
    expect(cb).not.toHaveBeenCalled()

    svc.GetStatus.mockResolvedValueOnce({ portable: portableSnap, installer: null, installed: null })
    expect(await adapter.getStatus()).toStrictEqual(portableSnap) // 聚合的 portable 位即契约快照

    const installerSnap = { state: 'running', version: '1.95.2', pid: 22, error: '', startedAt: '', form: 'installer' }
    lastHandler().handler(installerSnap)
    // 共享面板只跟便携引擎：installer 帧的内容（1.95.2）绝不得漏进交付快照
    expect(cb.mock.calls[0][0]).toStrictEqual(portableSnap)
    expect(runtime.installer?.state).toBe('running') // 方言双表侧 installer 位已并入

    lastHandler().handler(null)
    expect(cb).toHaveBeenCalledTimes(1)
  })

  it('download 确认闸：confirm-required 弹闸、放行后第二 RPC 带 force=true；拒绝零第二次；空 message 静默', async () => {
    const { runtime } = createVSCodeAdapter()
    const rel = { version: '1.95.0', size: 1, published: '', form: 'installer' } as never

    svc.DownloadVersion.mockResolvedValueOnce({ action: 'confirm-required', message: '需覆盖既有安装' })
    ui.confirm.mockResolvedValueOnce(true)
    svc.DownloadVersion.mockResolvedValueOnce({ action: 'started', message: '安装已开始' })
    expect(await runtime.download('installer', rel)).toEqual({ message: '安装已开始', reloadVersions: false })
    expect(svc.DownloadVersion).toHaveBeenNthCalledWith(1, '1.95.0', 'installer', false)
    expect(svc.DownloadVersion).toHaveBeenNthCalledWith(2, '1.95.0', 'installer', true)
    expect(ui.confirm).toHaveBeenCalledTimes(1)

    // 闸不过：动作未发生的硬事实——第二次 RPC 绝不发出
    svc.DownloadVersion.mockClear()
    svc.DownloadVersion.mockResolvedValueOnce({ action: 'confirm-required', message: '需覆盖既有安装' })
    ui.confirm.mockResolvedValueOnce(false)
    expect(await runtime.download('portable', rel)).toEqual({})
    expect(svc.DownloadVersion).toHaveBeenCalledTimes(1)

    // 已装词：无话术也要重拉版本区（面板"已安装"态即刻翻转）
    svc.DownloadVersion.mockResolvedValueOnce({ action: 'already-installed', message: '' })
    const res = await runtime.download('portable', rel)
    expect(res.message).toBeUndefined()
    expect(res.reloadVersions).toBe(true)

    // 契约面兜底：共享表行在便携源表查无对应资产时，不触任何 RPC
    const { adapter } = createVSCodeAdapter()
    svc.DownloadVersion.mockClear()
    expect(await adapter.versions.download({ version: '9.9.9' } as never)).toEqual({})
    expect(svc.DownloadVersion).not.toHaveBeenCalled()
  })

  it('setActive 报后端生效版本并同步高亮/缓存位；remove 确认闸走共享卸载词', async () => {
    const { adapter, runtime } = createVSCodeAdapter()
    svc.SetActiveVersion.mockResolvedValueOnce('1.95.1') // 后端确认可生效版本≠请求版本
    expect(await adapter.versions.setActive!('1.95.0')).toEqual({
      message: '已将便携版 1.95.1 设为使用版本',
      activeVersion: '1.95.1',
    })
    expect(runtime.activeVersion).toBe('1.95.1')

    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove({ version: '1.95.0' } as never)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    ui.confirm.mockResolvedValueOnce(true)
    expect(await adapter.versions.remove({ version: '1.95.0' } as never)).toEqual({
      message: '已卸载 1.95.0',
      reloadVersions: true,
    })
    expect(svc.RemoveVersion).toHaveBeenCalledWith('1.95.0')
  })
})
