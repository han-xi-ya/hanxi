// Piik adapter 特征测试（五路并行 · piik 前端线 C，对位 internal/modules/piik
// service.go 冻结面）：绑定面经 vi.mock 打桩（真实生成物落盘前由 vitest.config
// 解析缝兜底）；覆盖 headless 话术单源（running stateText/banner 恒含「界面在
// 浏览器」）、GATE_NO_BROWSER 兜底横幅、stopped hint「机读不自动弹→打开
// 界面进控制台」双引导、**metaHints 透传后端六条**（MetaHints() RPC，前端零自造，
// 端口/使用版本变动动词后重取）、Start/OpenWindow/Quit 三面（退出确认必须
// 消费后端 QuitAdvisory 预告；OpenWindow 负裁决停用禁用）、账外漂移警示压过
// 运行态横幅（DBX 同款克制）、裸事件 lastLedger 回填、下载无 confirm 闸与
// in-progress/already-installed 回执、末版卸载预告复用与"不删数据"明示、
// followOnExit 回执词与子进程树话术。
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
  Start: vi.fn(),
  OpenWindow: vi.fn(),
  QuitAdvisory: vi.fn(),
  Quit: vi.fn(),
  MetaHints: vi.fn(),
  OpenDataDir: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))
const wailsEvent = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/hanxi/internal/modules/piik/piikservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: wailsEvent }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createPiikAdapter } from '../piik'
import type { PiikStatus } from '../piik'
import type { ManagedActionResult } from '../../components/managed/adapter'

function statusOf(partial: Partial<PiikStatus>): PiikStatus {
  return {
    state: 'stopped',
    version: '',
    pid: 0,
    error: '',
    startedAt: '',
    listenPort: 8787,
    consoleUrl: '',
    localAccessOpen: true,
    passwordSet: false,
    lanInvitation: '',
    publicInvitation: '',
    noBrowser: false,
    dataDir: 'D:\\hanxi-data\\piik',
    configPath: 'D:\\hanxi-data\\piik\\client.json',
    logDir: 'D:\\hanxi-data\\piik\\logs',
    drifted: false,
    driftNote: '',
    ...partial,
  }
}

const emptyCtx = { installed: [], releases: [], active: '' } as never

const installedSole = {
  version: 'v1.6.5',
  exePath: 'D:\\piik\\v1.6.5\\piik-app.exe',
  dir: 'D:\\piik\\v1.6.5',
  size: 46 * 1024 * 1024,
  installedAt: '2026-09-20',
}

const releaseNext = { version: 'v1.6.6', published: '2026-09-24T00:00:00Z', size: 46 * 1024 * 1024 }

// 后端六条的替身账（断言"透传"只锁逐字回带与重取时机，前端零自造）
const BACKEND_HINTS = ['第1条钉版账', '第2条0.0.0.0账', '第3条隧道账', '第4条不代管账', '第5条子进程账', '第6条数据留存账']

describe('createPiikAdapter', () => {
  beforeEach(() => vi.clearAllMocks())

  it('六态词表收口：running 恒带「界面在浏览器」headless 话术，未知态兜底未托管；quitting 走 starting 脉冲档', () => {
    const adapter = createPiikAdapter()
    expect(adapter.stateText?.(statusOf({ state: 'running' }))).toContain('界面在浏览器')
    expect(adapter.stateText?.(statusOf({ state: 'stopped' }))).toBe('本会话未托管')
    expect(adapter.stateText?.(statusOf({ state: 'external' }))).toBe('外部实例运行中')
    expect(adapter.stateText?.(statusOf({ state: 'who-knows' }))).toBe('本会话未托管')
    expect(adapter.statusTone?.(statusOf({ state: 'quitting' }))).toBe('starting')
    expect(adapter.statusTone?.(statusOf({ state: 'running' }))).toBe('running')
  })

  it('running banner：ok 档讲明无自有窗口 + 界面在系统浏览器 + consoleUrl + 页面内开播指引', () => {
    const adapter = createPiikAdapter()
    const s = statusOf({ state: 'running', version: 'v1.6.5', consoleUrl: 'http://127.0.0.1:8787/' })
    const banner = adapter.banner!(s, emptyCtx)
    expect(banner!.tone).toBe('ok')
    expect(banner!.text).toContain('无自有窗口')
    expect(banner!.text).toContain('界面在系统浏览器')
    expect(banner!.text).toContain('http://127.0.0.1:8787/')
    expect(banner!.text).toContain('页面内进行')
    expect(banner!.text).not.toContain('WebView2') // 档案①#11：窗口模板措辞全页禁绝
  })

  it('GATE_NO_BROWSER：running+noBrowser 降 warn 兜底档，点名「打开界面」手动通道', () => {
    const adapter = createPiikAdapter()
    const banner = adapter.banner!(statusOf({ state: 'running', consoleUrl: 'http://127.0.0.1:8788/', noBrowser: true }), emptyCtx)
    expect(banner!.tone).toBe('warn')
    expect(banner!.text).toContain('自动拉起浏览器未成功')
    expect(banner!.text).toContain('打开界面')
    expect(banner!.text).toContain('http://127.0.0.1:8788/')
  })

  it('failed 直取后端原话（恒含端口/端口查杀指引），漂移警示压过运行态 ok 横幅但让位 failed', () => {
    const adapter = createPiikAdapter()
    const failed = adapter.banner!(statusOf({ state: 'failed', error: '等待 piik 就绪超时（8 秒）：端口 8787 上的服务没能起来…可用「端口查杀」页复核' }), emptyCtx)
    expect(failed!.tone).toBe('error')
    expect(failed!.text).toContain('端口查杀')

    const drifted = adapter.banner!(statusOf({ state: 'running', version: 'v1.6.5', drifted: true, driftNote: '落位后 exe mtime 变新且摘要不符', consoleUrl: 'http://127.0.0.1:8787/' }), emptyCtx)
    expect(drifted!.tone).toBe('warn')
    expect(drifted!.text).toContain('账外漂移')
    expect(drifted!.text).toContain('只警示、不自动处置')
    expect(drifted!.text).toContain('落位后 exe mtime 变新且摘要不符')
    expect(drifted!.text).not.toContain('正在运行') // 漂移压过运行态横幅
    expect(adapter.statusTone!(statusOf({ state: 'running', drifted: true }))).toBe('warn')
    expect(adapter.statusTone!(statusOf({ state: 'failed', drifted: true }))).toBe('failed') // failed 恒先
  })

  it('external banner 点名端口与不越权纪律；stopped hint 双引导（机读不自动弹→点打开界面进控制台）', () => {
    const adapter = createPiikAdapter()
    const ext = adapter.banner!(statusOf({ state: 'external', listenPort: 8787 }), emptyCtx)
    expect(ext!.tone).toBe('warn')
    expect(ext!.text).toContain('8787')
    expect(ext!.text).toContain('不越权')

    const hint = adapter.hint!(statusOf({ state: 'stopped' }), emptyCtx)!
    expect(hint).toContain('机读模式不自动弹浏览器')
    expect(hint).toContain('打开界面')
    expect(hint).toContain('打开界面')
    expect(hint).toContain('局域网敞开分享端口')
    expect(adapter.hint!(statusOf({ state: 'starting' }), emptyCtx)).toContain('端口就绪')
    expect(adapter.hint!(statusOf({ state: 'quitting' }), emptyCtx)).toContain('优雅')
    expect(adapter.hint!(statusOf({ state: 'running', consoleUrl: 'http://127.0.0.1:8787/' }), emptyCtx)).toBeNull()
  })

  it('metaHints 透传后端六条：首读懒拉 MetaHints()，逐字回带零自造；setActive/start 成功后重取一次', async () => {
    const adapter = createPiikAdapter()
    svc.MetaHints.mockResolvedValue(BACKEND_HINTS)

    // 首读：账还没回来先呈空账（不装样子），并触发唯一一次拉取
    expect(adapter.copy!.metaHints).toEqual([])
    await vi.waitFor(() => expect(adapter.copy!.metaHints).toHaveLength(6))
    expect(adapter.copy!.metaHints).toEqual(BACKEND_HINTS) // 逐字透传，前端不改一词
    expect(svc.MetaHints).toHaveBeenCalledTimes(1)

    // 端口/使用版本变动的动词改口后端账：setActive 与 primary(Start) 后各重取
    // 一次（在途单飞合并在途请求：连续动词不会叠加并发拉取）
    svc.SetActiveVersion.mockResolvedValue('v1.6.6')
    await adapter.versions.setActive!('v1.6.6')
    await vi.waitFor(() => expect(svc.MetaHints).toHaveBeenCalledTimes(2))
    svc.Start.mockResolvedValue({ action: 'started', external: false, message: 'piik 已启动', port: 8787, url: 'http://127.0.0.1:8787/' })
    await adapter.control!.primary!.run()
    await vi.waitFor(() => expect(svc.MetaHints).toHaveBeenCalledTimes(3))
    await vi.waitFor(() => expect(adapter.copy!.metaHints).toEqual(BACKEND_HINTS))
  })

  it('MetaHints 拉取失败静默保旧账：不弹错不刷屏，披露位宁缺不造', async () => {
    svc.MetaHints.mockRejectedValue(new Error('租约拒绝'))
    const adapter = createPiikAdapter()
    expect(adapter.copy!.metaHints).toEqual([])
    await vi.waitFor(() => expect(svc.MetaHints).toHaveBeenCalledTimes(1))
    expect(adapter.copy!.metaHints).toEqual([])
  })

  it('下载无 confirm 闸（服务型骨架无安装器通道）：already-installed 复刷、in-progress 只回执', async () => {
    const adapter = createPiikAdapter()
    svc.DownloadVersion.mockResolvedValueOnce('')
    await adapter.versions.download(releaseNext)
    expect(ui.confirm).not.toHaveBeenCalled()
    expect(svc.DownloadVersion).toHaveBeenCalledWith('v1.6.6')

    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    expect(await adapter.versions.download(releaseNext)).toEqual({ message: '版本 v1.6.6 已安装', reloadVersions: true })

    svc.DownloadVersion.mockResolvedValueOnce('in-progress')
    expect(await adapter.versions.download(releaseNext)).toEqual({ message: '版本 v1.6.6 已在下载中' })
  })

  it('末版卸载如实预告 + "不删数据"明示：确认文案含数据留存账目，取消不下刀', async () => {
    const adapter = createPiikAdapter()
    svc.ListInstalledVersions.mockResolvedValue([installedSole])
    ui.confirm.mockResolvedValueOnce(true)
    const res = await adapter.versions.remove(installedSole)
    const opts = ui.confirm.mock.calls[0][0]
    expect(opts.title).toBe('确定卸载 Piik v1.6.5？')
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain('这是最后一个版本，卸载后将回到未安装状态')
    expect(opts.description).toContain('卸载任何托管版本都不删数据')
    expect(opts.description).toContain('不提供删数据通道')
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v1.6.5')
    expect(res!.message).toBe('已卸载 v1.6.5')

    vi.clearAllMocks()
    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove(installedSole)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
  })

  it('退出确认必须消费后端 QuitAdvisory 预告（断播账说在点确认之前）；取消不发 Quit', async () => {
    const adapter = createPiikAdapter()
    svc.QuitAdvisory.mockResolvedValue('退出托管会关闭 piik 分享服务：正在观看的观众当场断播，公网邀请链接（Cloudflare 隧道）随之失效。')
    ui.confirm.mockResolvedValueOnce(true)
    svc.Quit.mockResolvedValue({ stopped: true, external: false, message: 'piik 已退出（优雅停通道收口，界面端口 8787 已释放）', port: 8787 })
    const res = (await adapter.control!.quit!.run()) as ManagedActionResult
    const opts = ui.confirm.mock.calls[0][0]
    expect(opts.description).toContain('当场断播')
    expect(opts.tone).toBe('warning')
    expect(svc.Quit).toHaveBeenCalledTimes(1)
    expect(res!.message).toContain('优雅停通道收口')

    vi.clearAllMocks()
    svc.QuitAdvisory.mockResolvedValue('预告')
    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.control!.quit!.run()).toEqual({})
    expect(svc.Quit).not.toHaveBeenCalled()
  })

  it('事件面订阅名与进度归一；裸快照 lastLedger 回填账目位、机读字段以现值为准', async () => {
    const adapter = createPiikAdapter()
    const stateCb = vi.fn()
    const progressCb = vi.fn()
    adapter.subscribeInstanceState(stateCb)
    adapter.subscribeProgress(progressCb)
    expect(wailsEvent.mock.calls[0][0]).toBe('piik:instance-state')
    expect(wailsEvent.mock.calls[1][0]).toBe('piik:version-download')

    // 先经轮询喂入账目事实（consoleUrl/漂移/三账目录）
    svc.GetStatus.mockResolvedValue(statusOf({ state: 'running', consoleUrl: 'http://127.0.0.1:8788/', listenPort: 8788, drifted: true, driftNote: '手工换件' }))
    await adapter.getStatus()

    const stateHandler = wailsEvent.mock.calls[0][1] as (s: unknown) => void
    // A 线裸快照：无 consoleUrl 键（JSON 不含 B 线账目位），lanInvitation 为事件现值
    stateHandler({ state: 'running', version: 'v1.6.5', pid: 9, error: '', startedAt: '', lanInvitation: 'http://192.168.1.5:8788/join/x' })
    const merged = stateCb.mock.calls[0][0] as PiikStatus
    expect(merged.consoleUrl).toBe('http://127.0.0.1:8788/')
    expect(merged.listenPort).toBe(8788)
    expect(merged.drifted).toBe(true)
    expect(merged.driftNote).toBe('手工换件')
    expect(merged.lanInvitation).toBe('http://192.168.1.5:8788/join/x')
    expect(merged.pid).toBe(9)
    // 事件携全量账目位时以现值为准（后端若升级组合快照亦自洽）
    stateHandler(statusOf({ state: 'stopped', consoleUrl: 'http://127.0.0.1:8790/', drifted: false, driftNote: '' }))
    const fresh = stateCb.mock.calls[1][0] as PiikStatus
    expect(fresh.consoleUrl).toBe('http://127.0.0.1:8790/')
    expect(fresh.drifted).toBe(false)

    const progressHandler = wailsEvent.mock.calls[1][1] as (t: unknown) => void
    progressHandler({ version: 'v1.6.6', stage: 'downloading', done: 30, total: 100, message: '' })
    expect(progressCb).toHaveBeenCalledWith({ key: 'v1.6.6', stage: 'downloading', done: 30, total: 100, message: '' })
    progressHandler({ version: '', stage: 'resolve', done: 0, total: 0, message: '' })
    expect(progressCb).toHaveBeenCalledTimes(1)
  })

  it('控制动词三面矩阵：启动幂等可点、退出四态可点、打开界面负裁决停用如实禁用', () => {
    const adapter = createPiikAdapter()
    expect(adapter.control!.primary!.label).toBe('▶ 启动')
    expect(adapter.control!.primary!.disabledFor!('starting')).toBe(true)
    expect(adapter.control!.primary!.disabledFor!('running')).toBe(false) // 幂等直返界面地址
    expect(adapter.control!.primary!.titleFor!('stopped')).toContain('局域网敞开分享端口')
    expect(adapter.control!.primary!.titleFor!('external')).toContain('不越权接管')

    expect(adapter.control!.quit!.disabledFor!('external')).toBe(false)
    expect(adapter.control!.quit!.disabledFor!('stopped')).toBe(true)
    expect(adapter.control!.quit!.titleFor!('external')).toContain('不越权终止')
    expect(adapter.control!.quit!.titleFor!('running')).toContain('断播')

    expect(adapter.openUI.label).toBe('🌐 打开界面')
    expect(adapter.openUI.disabledFor!('stopped')).toBe(true) // 负裁决：绝不冷启动
    expect(adapter.openUI.disabledFor!('failed')).toBe(true)
    expect(adapter.openUI.disabledFor!('running')).toBe(false)
    expect(adapter.openUI.disabledFor!('external')).toBe(false)
    expect(adapter.openUI.titleFor!('stopped')).toContain('须点「启动」明示')
    expect(adapter.openUI.titleFor!('running')).toContain('界面恒在浏览器')
  })

  it('openUI 动词回带后端 message；数据目录钮走 OpenDataDir 专口（未创建如实抛错交 extras 卡 toast）', async () => {
    const adapter = createPiikAdapter()
    svc.OpenWindow.mockResolvedValue({ action: 'opened', external: false, message: '已在浏览器打开 piik 界面 http://127.0.0.1:8787/', port: 8787, url: 'http://127.0.0.1:8787/' })
    expect(await adapter.openUI.run()).toEqual({ message: '已在浏览器打开 piik 界面 http://127.0.0.1:8787/' })

    svc.OpenDataDir.mockResolvedValue(undefined)
    await expect(adapter.extras!.dataDir!.open()).resolves.toEqual({})
    expect(svc.OpenDataDir).toHaveBeenCalledTimes(1)

    svc.OpenDataDir.mockRejectedValueOnce(new Error('piik 托管数据目录尚未创建（还未托管启动过）'))
    await expect(adapter.extras!.dataDir!.open()).rejects.toThrow('尚未创建')
  })

  it('followOnExit 回执词点名子进程树与驻留语义；注记限定托管实例', async () => {
    const adapter = createPiikAdapter()
    svc.GetFollowOnExit.mockResolvedValue(false)
    expect(await adapter.extras!.followOnExit!.get()).toBe(false)
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    const on = (await adapter.extras!.followOnExit!.set(true)) as ManagedActionResult
    expect(on!.message).toContain('Hanxi 退出时连带终止')
    expect(on!.message).toContain('cloudflared/piik-capture')
    const off = (await adapter.extras!.followOnExit!.set(false)) as ManagedActionResult
    expect(off!.message).toContain('原地驻留继续开播')
    expect(adapter.extras!.followOnExit!.note).toContain('外部自行启动')
  })
})
