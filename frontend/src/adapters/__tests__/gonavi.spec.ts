// gonavi adapter 特征测试（三线并行 · 对齐骨架线实码契约）：
// 绑定面经 vi.mock 打桩（真实生成物落盘前由 vitest.config 解析缝兜底）；
// 覆盖漂移警示投影（banner 压过运行态 + 状态灯色档 + driftNote 明细）、
// metaHints 四条如实披露（含漂移徽章语义）、下载 confirm 闸（若运行实例在）、
// 退出前 QuitAdvisory 预告确认流、末版卸载预告复用、事件订阅名与进度归一。
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
  QuitAdvisory: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))
const wailsEvent = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/hanxi/internal/modules/gonavi/gonaviservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: wailsEvent }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createGoNaviAdapter } from '../gonavi'
import type { GoNaviStatus } from '../gonavi'

function statusOf(partial: Partial<GoNaviStatus>): GoNaviStatus {
  return { state: 'stopped', version: '', pid: 0, error: '', startedAt: '', drifted: false, driftNote: '', ...partial }
}

const release220 = { version: 'v2.2.0', published: '2026-09-01T00:00:00Z', size: 1024 }

describe('createGoNaviAdapter', () => {
  beforeEach(() => vi.clearAllMocks())

  it('五态词表收口（实面无 quitting 档）：external 一级公民，未知态兜底未托管', () => {
    const adapter = createGoNaviAdapter()
    expect(adapter.stateText?.(statusOf({ state: 'external' }))).toBe('外部实例运行中')
    expect(adapter.stateText?.(statusOf({ state: 'running' }))).toBe('托管实例运行中')
    // 引擎无 quitting 态；旧词/未知词一律落兜底，不冒充退出中
    expect(adapter.stateText?.(statusOf({ state: 'quitting' }))).toBe('本会话未托管')
    expect(adapter.stateText?.(statusOf({ state: 'who-knows' }))).toBe('本会话未托管')
  })

  it('账外漂移：banner 压过运行态 ok 横幅，文案含"二进制已被应用自更新替换，与下载账目不一致"与后端明细', async () => {
    const adapter = createGoNaviAdapter()
    const driftedSnap = statusOf({ state: 'running', version: 'v2.1.0', drifted: true, driftNote: 'SHA256 与账本落位摘要不符' })
    svc.GetStatus.mockResolvedValue(driftedSnap)
    await adapter.getStatus()

    const banner = adapter.banner!(driftedSnap, { installed: [], releases: [], active: 'v2.1.0' })
    expect(banner!.tone).toBe('warn')
    expect(banner!.text).toContain('二进制已被应用自更新替换，与下载账目不一致')
    expect(banner!.text).toContain('只警示、不自动处置')
    expect(banner!.text).toContain('SHA256 与账本落位摘要不符')
    expect(banner!.text).not.toContain('正在运行：')
  })

  it('failed 恒先于漂移（当下故障更要紧）；漂移时状态灯降 warn 档、failed 保红', () => {
    const adapter = createGoNaviAdapter()
    const s = statusOf({ state: 'failed', error: '启动超时', drifted: true })
    expect(adapter.banner!(s, { installed: [], releases: [], active: '' })!.tone).toBe('error')
    expect(adapter.banner!(s, { installed: [], releases: [], active: '' })!.text).toBe('启动超时')
    expect(adapter.statusTone!(s)).toBe('failed')
    expect(adapter.statusTone!(statusOf({ state: 'running', drifted: true }))).toBe('warn')
  })

  it('metaHints 四条如实披露（骨架口径）：摘要宁拒不装、漂移徽章语义、数据不随卸载、WebView2+多开只甄别', () => {
    const adapter = createGoNaviAdapter()
    const hints = adapter.copy!.metaHints!
    expect(hints).toHaveLength(4)
    const joined = hints.join('\n')
    expect(joined).toContain('二进制已被应用自更新替换，与下载账目不一致')
    expect(joined).toContain('账外漂移')
    expect(joined).toContain('%USERPROFILE%\\.gonavi')
    expect(joined).toContain('卸载任何版本（含末版）都不触碰')
    expect(joined).toContain('宁拒不装')
    expect(joined).toContain('WebView2 Runtime')
    expect(joined).toContain('多开属用户自由')
    expect(joined).toContain('宽限后强杀兜底')
  })

  it('下载 confirm 闸：运行实例在才弹——拒绝静默不调后端，同意才 DownloadVersion；停动态直通', async () => {
    const adapter = createGoNaviAdapter()

    // 停止态：闸不发
    svc.GetStatus.mockResolvedValue(statusOf({ state: 'stopped' }))
    svc.DownloadVersion.mockResolvedValue('')
    await adapter.getStatus()
    await adapter.versions.download(release220)
    expect(ui.confirm).not.toHaveBeenCalled()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('v2.2.0')

    // 运行态：拒绝 → 取消静默（无 message 无 reload、不动后端）
    vi.clearAllMocks()
    svc.GetStatus.mockResolvedValue(statusOf({ state: 'running', version: 'v2.1.0' }))
    await adapter.getStatus()
    ui.confirm.mockResolvedValueOnce(false)
    const cancelled = await adapter.versions.download(release220)
    expect(ui.confirm).toHaveBeenCalledTimes(1)
    expect(ui.confirm.mock.calls[0][0].title).toContain('实例正在运行')
    expect(cancelled).toEqual({})
    expect(svc.DownloadVersion).not.toHaveBeenCalled()

    // 运行态：同意 → 放行下载；后端回执如实转词
    ui.confirm.mockResolvedValueOnce(true)
    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    const res = await adapter.versions.download(release220)
    expect(svc.DownloadVersion).toHaveBeenCalledWith('v2.2.0')
    expect(res).toEqual({ message: '版本 v2.2.0 已安装', reloadVersions: true })
    ui.confirm.mockResolvedValueOnce(true)
    svc.DownloadVersion.mockResolvedValueOnce('in-progress')
    const busy = await adapter.versions.download(release220)
    expect(busy).toEqual({ message: '版本 v2.2.0 正在下载中，请留意进度' })
  })

  it('退出前预告确认流：QuitAdvisory 后端话术原文进确认框，拒绝不下投 Quit，同意后回执转述', async () => {
    const adapter = createGoNaviAdapter()
    svc.QuitAdvisory.mockResolvedValue('GoNavi 若有未保存 SQL 草稿会弹确认框；托管退出在宽限后将强制终止进程。')
    svc.Quit.mockResolvedValue({ stopped: true, external: false, message: 'GoNavi 已退出' })

    ui.confirm.mockResolvedValueOnce(false)
    await expect(adapter.control!.quit!.run()).resolves.toEqual({})
    expect(svc.Quit).not.toHaveBeenCalled()

    ui.confirm.mockResolvedValueOnce(true)
    const res = await adapter.control!.quit!.run()
    expect(ui.confirm.mock.calls[0][0].description).toBe('GoNavi 若有未保存 SQL 草稿会弹确认框；托管退出在宽限后将强制终止进程。')
    expect(svc.Quit).toHaveBeenCalledTimes(1)
    expect(res!.message).toBe('GoNavi 已退出')
  })

  it('末版卸载放行清账口径：soleVersionUninstallNote 复用进确认文案；多版本不预告', async () => {
    const adapter = createGoNaviAdapter()
    const v = { version: 'v2.1.0', exePath: 'D:\\gonavi\\v2.1.0\\GoNavi.exe', dir: 'D:\\gonavi\\v2.1.0', size: 1, installedAt: '2026-09-01' }

    svc.ListInstalledVersions.mockResolvedValue([v])
    ui.confirm.mockResolvedValueOnce(true)
    const res = await adapter.versions.remove(v)
    expect(ui.confirm.mock.calls[0][0].description).toContain('这是最后一个版本，卸载后将回到未安装状态')
    expect(ui.confirm.mock.calls[0][0].description).toContain('%USERPROFILE%\\.gonavi')
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v2.1.0')
    expect(res.message).toBe('已卸载 v2.1.0')

    vi.clearAllMocks()
    svc.ListInstalledVersions.mockResolvedValue([v, { ...v, version: 'v2.0.0' }])
    ui.confirm.mockResolvedValueOnce(true)
    await adapter.versions.remove(v)
    expect(ui.confirm.mock.calls[0][0].description).not.toContain('最后一个版本')
  })

  it('事件面订阅名与进度归一；instance-state 裸快照回填最近账目事实，漂移警示不闪失', () => {
    const adapter = createGoNaviAdapter()
    const stateCb = vi.fn()
    const progressCb = vi.fn()
    adapter.subscribeInstanceState(stateCb)
    adapter.subscribeProgress(progressCb)
    expect(wailsEvent.mock.calls[0][0]).toBe('gonavi:instance-state')
    expect(wailsEvent.mock.calls[1][0]).toBe('gonavi:version-download')

    // 事件 handler 捕获（useWailsEvent(name, handler) 第二参）
    const stateHandler = wailsEvent.mock.calls[0][1] as (s: GoNaviStatus) => void
    const progressHandler = wailsEvent.mock.calls[1][1] as (t: unknown) => void

    // 引擎事件只发基础 Snapshot（无 drifted/driftNote）：沿用最近 GetStatus 的账目事实
    stateHandler(statusOf({ state: 'running', drifted: true, driftNote: '原地换 exe' }))
    stateHandler({ state: 'running', version: 'v2.1.0', pid: 1, error: '', startedAt: '' } as unknown as GoNaviStatus)
    const last = stateCb.mock.calls[stateCb.mock.calls.length - 1][0] as GoNaviStatus
    expect(last.drifted).toBe(true)
    expect(last.driftNote).toBe('原地换 exe')
    expect(last.pid).toBe(1)
    // 事件携全量字段时以现值为准（后端若升级为组合快照亦自洽）
    stateHandler(statusOf({ state: 'stopped' }))
    expect(stateCb.mock.calls[stateCb.mock.calls.length - 1][0].drifted).toBe(false)

    progressHandler({ version: 'v2.2.0', stage: 'downloading', done: 30, total: 100, message: '' })
    expect(progressCb).toHaveBeenCalledWith({ key: 'v2.2.0', stage: 'downloading', done: 30, total: 100, message: '' })
    progressHandler({ version: '', stage: 'resolve', done: 0, total: 0, message: '' })
    expect(progressCb).toHaveBeenCalledTimes(1)
  })

  it('引导与提示：stopped 引导行预告无单实例唤回；starting hint 讲 WebView2；external 横幅讲只甄别不拦阻', () => {
    const adapter = createGoNaviAdapter()
    expect(adapter.hint!(statusOf({ state: 'stopped' }), { installed: [], releases: [], active: '' })).toContain('唤回既有窗口')
    expect(adapter.hint!(statusOf({ state: 'starting' }), { installed: [], releases: [], active: '' })).toContain('WebView2')
    const externalBanner = adapter.banner!(statusOf({ state: 'external' }), { installed: [], releases: [], active: '' })!
    expect(externalBanner.tone).toBe('warn')
    expect(externalBanner.text).toContain('多开属用户自由')
    expect(externalBanner.text).toContain('只甄别')
  })
})
