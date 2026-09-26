// dbx adapter 特征测试（三线并行 · 冻结契约口径）：
// 绑定面经 vi.mock 打桩（真实生成物落盘前由 vitest.config 解析缝兜底）；
// 覆盖账本漂移警示投影（banner 压过运行态 + 状态灯色档 + 事件轮次账目回填）、
// metaHints 五条如实披露（日更节奏/漂移语义/数据留存/托盘退出/minisign 不验）、
// 下载无 confirm 闸新口径、末版卸载预告复用（soleVersionUninstallNote）、
// followOnExit 回执词与数据目录钮取径。
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
  OfficialSiteURL: vi.fn(),
  OpenRepo: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))
const wailsEvent = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/hanxi/internal/modules/dbx/dbxservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: wailsEvent }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createDbxAdapter } from '../dbx'
import type { DbxStatus } from '../dbx'
import type { ManagedActionResult } from '../../components/managed/adapter'

function statusOf(partial: Partial<DbxStatus>): DbxStatus {
  return { state: 'stopped', version: '', pid: 0, error: '', startedAt: '', drifted: false, driftNote: '', dataDir: 'D:\\hanxi-data\\dbx', ...partial }
}

const emptyCtx = { installed: [], releases: [], active: '' } as never

const installedSole = {
  version: '0.9.30',
  exePath: 'D:\\dbx\\0.9.30\\dbx.exe',
  dir: 'D:\\dbx\\0.9.30',
  size: 1024,
  installedAt: '2026-09-10',
}

const releaseLatest = { version: '0.9.32', published: '2026-09-25T00:00:00Z', size: 1024, form: 'portable' }

describe('createDbxAdapter', () => {
  beforeEach(() => vi.clearAllMocks())

  it('六态词表收口：quitting/external 一级公民，未知态兜底未托管', () => {
    const adapter = createDbxAdapter()
    expect(adapter.stateText?.(statusOf({ state: 'quitting' }))).toBe('正在退出')
    expect(adapter.stateText?.(statusOf({ state: 'external' }))).toBe('外部实例运行中')
    expect(adapter.stateText?.(statusOf({ state: 'who-knows' }))).toBe('本会话未托管')
    expect(adapter.statusTone?.(statusOf({ state: 'quitting' }))).toBe('starting')
  })

  it('账本漂移：banner 压过运行态 ok 横幅，文案含逐字口径与后端附言；状态灯降 warn 档', async () => {
    const adapter = createDbxAdapter()
    const s = statusOf({ state: 'running', version: '0.9.30', drifted: true, driftNote: 'SHA256 与下载账目不符' })
    svc.GetStatus.mockResolvedValue(s)
    await adapter.getStatus()

    const banner = adapter.banner!(s, emptyCtx)
    expect(banner!.tone).toBe('warn')
    expect(banner!.text).toContain('二进制已被应用自更新替换，与下载账目不一致')
    expect(banner!.text).toContain('只警示、不自动处置')
    expect(banner!.text).toContain('SHA256 与下载账目不符')
    expect(banner!.text).not.toContain('DBX 正在运行')
    expect(adapter.statusTone!(s)).toBe('warn')
  })

  it('failed 恒先于漂移（当下故障更要紧，保红不降琥珀）', () => {
    const adapter = createDbxAdapter()
    const s = statusOf({ state: 'failed', error: '启动超时', drifted: true })
    expect(adapter.banner!(s, emptyCtx)!.tone).toBe('error')
    expect(adapter.banner!(s, emptyCtx)!.text).toBe('启动超时')
    expect(adapter.statusTone!(s)).toBe('failed')
  })

  it('metaHints 五条如实披露：校验口径（minisign 不验）、日更长列表、漂移徽章语义、数据留存、托盘退出与托管备份', () => {
    const adapter = createDbxAdapter()
    const hints = adapter.copy!.metaHints!
    expect(hints).toHaveLength(5)
    const joined = hints.join('\n')
    expect(joined).toContain('官方 digest 四层完整校验')
    expect(joined).toContain('.sig 为 minisign 签名，本托管不验证')
    expect(joined).toContain('月均 20+')
    expect(joined).toContain('更新频繁，按需停留稳定版即可')
    expect(joined).toContain('二进制已被应用自更新替换，与下载账目不一致')
    expect(joined).toContain('卸载版本不删数据')
    expect(joined).toContain('portable 标记文件随包保留')
    expect(joined).toContain('宽限后强杀兜底')
    expect(joined).toContain('托管备份')
    expect(joined).toContain('不追杀外部同名进程')
  })

  it('下载无 confirm 闸（新口径）：运行态直调 DownloadVersion，确认框不弹；already-installed 回执弹 toast 并复刷', async () => {
    const adapter = createDbxAdapter()
    svc.GetStatus.mockResolvedValue(statusOf({ state: 'running', version: '0.9.30', pid: 7 }))
    await adapter.getStatus()

    svc.DownloadVersion.mockResolvedValueOnce('')
    await adapter.versions.download(releaseLatest)
    expect(ui.confirm).not.toHaveBeenCalled()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('0.9.32')

    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    const res = await adapter.versions.download(releaseLatest)
    expect(res).toEqual({ message: '版本 0.9.32 已安装', reloadVersions: true })
  })

  it('末版卸载放行清账口径：soleVersionUninstallNote 复用进确认文案，数据不随删如实预告；多版本不预告', async () => {
    const adapter = createDbxAdapter()

    svc.ListInstalledVersions.mockResolvedValue([installedSole])
    ui.confirm.mockResolvedValueOnce(true)
    const res = await adapter.versions.remove(installedSole)
    const opts = ui.confirm.mock.calls[0][0]
    expect(opts.title).toBe('确定卸载 DBX 0.9.30？')
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain('这是最后一个版本，卸载后将回到未安装状态')
    expect(opts.description).toContain('DBX_DATA_DIR')
    expect(svc.RemoveVersion).toHaveBeenCalledWith('0.9.30')
    expect(res.message).toBe('已卸载 0.9.30')

    vi.clearAllMocks()
    svc.ListInstalledVersions.mockResolvedValue([installedSole, { ...installedSole, version: '0.9.29' }])
    ui.confirm.mockResolvedValueOnce(true)
    await adapter.versions.remove(installedSole)
    expect(ui.confirm.mock.calls[0][0].description).not.toContain('最后一个版本')

    vi.clearAllMocks()
    svc.ListInstalledVersions.mockResolvedValue([installedSole])
    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove(installedSole)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
  })

  it('事件面订阅名与进度归一；instance-state 裸快照回填最近账目事实，漂移警示不闪失', async () => {
    const adapter = createDbxAdapter()
    const stateCb = vi.fn()
    const progressCb = vi.fn()
    adapter.subscribeInstanceState(stateCb)
    adapter.subscribeProgress(progressCb)
    expect(wailsEvent.mock.calls[0][0]).toBe('dbx:instance-state')
    expect(wailsEvent.mock.calls[1][0]).toBe('dbx:version-download')

    const stateHandler = wailsEvent.mock.calls[0][1] as (s: DbxStatus) => void
    const progressHandler = wailsEvent.mock.calls[1][1] as (t: unknown) => void

    // 先经轮询喂入账目事实，再收引擎裸 Snapshot 事件（无 drifted/driftNote/dataDir）：
    // 账目沿用最近事实，运行现值（pid 等）以事件为准
    svc.GetStatus.mockResolvedValue(statusOf({ state: 'running', drifted: true, driftNote: '原地换 exe', dataDir: 'D:\\hx\\dbx' }))
    await adapter.getStatus()
    stateHandler({ state: 'running', version: '0.9.30', pid: 1, error: '', startedAt: '' } as unknown as DbxStatus)
    const merged = stateCb.mock.calls[stateCb.mock.calls.length - 1][0] as DbxStatus
    expect(merged.drifted).toBe(true)
    expect(merged.driftNote).toBe('原地换 exe')
    expect(merged.dataDir).toBe('D:\\hx\\dbx')
    expect(merged.pid).toBe(1)
    // 事件携全量字段时以现值为准（后端若升级为组合快照亦自洽）
    stateHandler(statusOf({ state: 'stopped', drifted: false, driftNote: '', dataDir: 'D:\\other' }))
    const fresh = stateCb.mock.calls[stateCb.mock.calls.length - 1][0] as DbxStatus
    expect(fresh.drifted).toBe(false)
    expect(fresh.dataDir).toBe('D:\\other')

    progressHandler({ version: '0.9.32', stage: 'downloading', done: 30, total: 100, message: '' })
    expect(progressCb).toHaveBeenCalledWith({ key: '0.9.32', stage: 'downloading', done: 30, total: 100, message: '' })
    progressHandler({ version: '', stage: 'resolve', done: 0, total: 0, message: '' })
    expect(progressCb).toHaveBeenCalledTimes(1)
  })

  it('followOnExit 开关回执如实：开启讲清托盘挂住与宽限强杀，关闭讲清驻托盘继续运行', async () => {
    const adapter = createDbxAdapter()
    svc.GetFollowOnExit.mockResolvedValue(false)
    expect(await adapter.extras!.followOnExit!.get()).toBe(false)

    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const on = (await adapter.extras!.followOnExit!.set(true)) as ManagedActionResult
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(true)
    expect(on.message).toContain('Hanxi 退出时一并关闭')
    expect(on.message).toContain('强杀兜底')
    const off = (await adapter.extras!.followOnExit!.set(false)) as ManagedActionResult
    expect(off.message).toContain('继续驻托盘独立运行')
    expect(adapter.extras!.followOnExit!.note).toContain('托管备份')
  })

  it('数据目录钮：优先账目现值，现值缺席现拉 GetStatus 兜底，仍无则如实抛错', async () => {
    const adapter = createDbxAdapter()

    svc.GetStatus.mockResolvedValue(statusOf({ dataDir: 'D:\\hanxi-data\\dbx' }))
    await adapter.getStatus()
    svc.OpenDir.mockResolvedValue(undefined)
    await expect(adapter.extras!.dataDir!.open()).resolves.toEqual({})
    expect(svc.OpenDir).toHaveBeenCalledWith('D:\\hanxi-data\\dbx')
    expect(svc.GetStatus).toHaveBeenCalledTimes(1) // 走现值，不再多拉

    vi.clearAllMocks()
    // 新 adapter（账目现值为空）且现拉也拿不到目录：如实抛错交 extras 卡 toast
    const starving = createDbxAdapter()
    svc.GetStatus.mockResolvedValue(statusOf({ dataDir: '' }))
    await expect(starving.extras!.dataDir!.open()).rejects.toThrow('尚未取得数据目录路径')

    vi.clearAllMocks()
    // 兜底路径：新 adapter（无现值）→ open 内部现拉一次 GetStatus 取到 dataDir
    const fresh = createDbxAdapter()
    svc.GetStatus.mockResolvedValue(statusOf({ dataDir: 'D:\\late\\dbx' }))
    svc.OpenDir.mockResolvedValue(undefined)
    await fresh.extras!.dataDir!.open()
    expect(svc.GetStatus).toHaveBeenCalledTimes(1)
    expect(svc.OpenDir).toHaveBeenCalledWith('D:\\late\\dbx')
  })

  it('控制动词投影：主钮 starting/quitting 禁用、退出钮 external 可点且 title 讲明不代关', () => {
    const adapter = createDbxAdapter()
    expect(adapter.control!.primary!.disabledFor!('starting')).toBe(true)
    expect(adapter.control!.primary!.disabledFor!('quitting')).toBe(true)
    expect(adapter.control!.primary!.disabledFor!('stopped')).toBe(false)
    expect(adapter.control!.quit!.disabledFor!('external')).toBe(false)
    expect(adapter.control!.quit!.disabledFor!('stopped')).toBe(true)
    expect(adapter.control!.quit!.titleFor!('external')).toContain('不代关')
    expect(adapter.control!.quit!.titleFor!('running')).toContain('宽限后强杀兜底')
    expect(adapter.control!.quit!.titleFor!('running')).toContain('托管备份')
  })
})
