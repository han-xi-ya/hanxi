// ============================================================================
// AirSnip adapter · 内部命令通道与投影话术
//
// 本模块与其余托管件的唯一形状差异：除声明式 primary/quit 外，另有「截图」「贴图」
// 两个模块自有动作钮（走 #primary-action 槽），且"打开位置"落的是上游全局配置
// 目录 %APPDATA%\airsnip 而非版本隔离目录。用例锁的是这四件事：
//  - 动作钮各自打到正确的 RPC（Shot/Pin 打反不会报错，只会静默做错事——最坏病型）；
//  - 后端回执 message 原样透传，前端不改写、不把"投递成功"包装成"已看到窗口"；
//  - dataDir 直调 OpenConfigDir，绝不按已装/活动/快照三源去解析版本目录；
//  - 危险/输入动作在用户取消或空输入时零 RPC；failed 态 error 原文进 banner
//    （前端 ElevateRestart 靠其中的「管理员」关键词显隐，改写即断链）。
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
  OpenConfigDir: vi.fn(),
  OpenWindow: vi.fn(),
  Shot: vi.fn(),
  Pin: vi.fn(),
  Quit: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/airsnip/airsnipservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import type { ManagedActionResult } from '../../components/managed/adapter'
import { createAirsnipAdapter } from '../airsnip'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

const stoppedSnap = { state: 'stopped', version: '', pid: 0, error: '', external: false, startedAt: '', stoppedAt: '' }

beforeEach(() => {
  events.length = 0
  vi.clearAllMocks()
})

describe('airsnip · 内部命令通道', () => {
  it('两个动作钮分别打到 Shot / Pin，并按后端回执透传 message', async () => {
    const adapter = createAirsnipAdapter()
    expect(adapter.actions.map((a) => a.label)).toEqual(['✂ 截图', '📌 贴图'])

    svc.Shot.mockResolvedValue({ action: 'shot', external: false, message: '已触发截图：屏幕已冻结，框选区域即可标注' })
    svc.Pin.mockResolvedValue({ action: 'pin', external: false, message: '已请求把剪贴板内容贴到屏幕（剪贴板无位图时上游不会创建贴图）' })

    const shot = await adapter.actions[0].run()
    const pin = await adapter.actions[1].run()
    expect(svc.Shot).toHaveBeenCalledTimes(1)
    expect(svc.Pin).toHaveBeenCalledTimes(1)
    expect(shot.message).toContain('截图')
    expect(pin.message).toContain('剪贴板')
  })

  it('动作钮 starting 态只提示等待，不谎报"唤起面板/冻结屏幕"', () => {
    const adapter = createAirsnipAdapter()
    for (const action of adapter.actions) {
      expect(action.titleFor('starting')).toContain('请稍候')
    }
    // running 与 stopped 两态都给真动作语义（后端在 stopped 态会先冷启动再投递）
    expect(adapter.actions[0].titleFor('running')).toContain('F1')
    expect(adapter.actions[0].titleFor('stopped')).toContain('启动')
  })

  it('primary 走 OpenWindow、quit 走 Quit；external 态退出钮禁用并指向其托盘', async () => {
    const adapter = createAirsnipAdapter()
    svc.OpenWindow.mockResolvedValue({ action: 'opened', external: false, message: '已打开 AirSnip 设置面板' })
    svc.Quit.mockResolvedValue({ stopped: true, external: false, message: 'AirSnip 已退出' })

    expect(((await adapter.control!.primary!.run()) as ManagedActionResult).message).toBe('已打开 AirSnip 设置面板')
    expect(((await adapter.control!.quit!.run()) as ManagedActionResult).message).toBe('AirSnip 已退出')
    // external 态退出钮**可点**（点了只回指引，不越权强杀外部实例——后端 Quit 契约）
    expect(adapter.control!.quit!.disabledFor!('external')).toBe(false)
    expect(adapter.control!.quit!.titleFor!('external')).toContain('托盘')
    expect(adapter.control!.quit!.disabledFor!('stopped')).toBe(true)
    expect(adapter.control!.primary!.disabledFor!('stopped')).toBe(false)
  })

  it('状态投影：external 不越权退出、failed 原文透传（含「管理员」才有一键提权重启）、running 指回热键', () => {
    const adapter = createAirsnipAdapter()
    expect(adapter.banner!({ ...stoppedSnap, state: 'external', external: true } as never, undefined as never)).toEqual({
      tone: 'warn',
      text: '检测到外部运行的 AirSnip（非 Hanxi 托管）。可继续投去截图/贴图命令；如需退出请在其托盘菜单操作。',
    })

    const failed = { ...stoppedSnap, state: 'failed', error: 'AirSnip 异常退出（退出码 1）。请以管理员身份重启 Hanxi' }
    const banner = adapter.banner!(failed as never, undefined as never)
    expect(banner!.tone).toBe('error')
    expect(banner!.text).toBe(failed.error) // 逐字透传，改写关键词会断掉 ElevateRestart 显隐

    const running = adapter.banner!({ ...stoppedSnap, state: 'running' } as never, undefined as never)
    expect(running!.text).toContain('F1')
    expect(adapter.hint!({ ...stoppedSnap, state: 'stopped' } as never, undefined as never)).toContain('F1') // 与 Snipaste 抢键预告同句
    expect(adapter.hint!({ ...stoppedSnap, state: 'external', external: true } as never, undefined as never)).toBeNull()
  })
})

describe('airsnip · 版本区与目录直达', () => {
  it('dataDir 直调 OpenConfigDir：不读已装列表、不解析版本目录', async () => {
    const adapter = createAirsnipAdapter()
    await adapter.extras!.dataDir!.open()
    expect(svc.OpenConfigDir).toHaveBeenCalledTimes(1)
    expect(svc.OpenDir).not.toHaveBeenCalled()
    expect(svc.ListInstalledVersions).not.toHaveBeenCalled()
    expect(svc.GetActiveVersion).not.toHaveBeenCalled()
  })

  it('openDir 仍按版本记录落位目录透传（版本区每一行的「打开」钮）', async () => {
    const adapter = createAirsnipAdapter()
    await adapter.versions.openDir({ version: 'v0.7.0', dir: 'D:\\versions\\airsnip_0.7.0' } as never)
    expect(svc.OpenDir).toHaveBeenCalledWith('D:\\versions\\airsnip_0.7.0')
  })

  it('取消确认框 / 空输入路径时零 RPC', async () => {
    const adapter = createAirsnipAdapter()
    ui.confirm.mockResolvedValue(false)
    expect(await adapter.versions.remove({ version: 'v0.7.0', dir: 'D:\\x' } as never)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()

    ui.prompt.mockResolvedValue('   ')
    expect(await adapter.versions.importLocal!()).toEqual({})
    expect(svc.ImportLocal).not.toHaveBeenCalled()

    ui.confirm.mockResolvedValue(true)
    svc.RemoveVersion.mockResolvedValue(undefined)
    const out = await adapter.versions.remove({ version: 'v0.7.0', dir: 'D:\\x' } as never)
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v0.7.0')
    expect(out.reloadVersions).toBe(true)
  })

  it('导入回执带版本号并请求复刷版本区；事件名逐字锁定', async () => {
    const adapter = createAirsnipAdapter()
    ui.prompt.mockResolvedValue(' C:\\Users\\me\\Downloads\\airsnip-v0.7.0-win-x64 ')
    svc.ImportLocal.mockResolvedValue({ version: 'v0.7.0', dir: 'D:\\versions\\airsnip_0.7.0' })
    const out = await adapter.versions.importLocal!()
    expect(svc.ImportLocal).toHaveBeenCalledWith('C:\\Users\\me\\Downloads\\airsnip-v0.7.0-win-x64') // 首尾空白已剥
    expect(out.reloadVersions).toBe(true)
    expect(out.message).toContain('v0.7.0')

    const cb = vi.fn()
    adapter.subscribeInstanceState(cb)
    expect(lastHandler().name).toBe('airsnip:instance-state')
    lastHandler().handler(stoppedSnap)
    expect(cb).toHaveBeenCalledWith(stoppedSnap)
    lastHandler().handler(null) // 空载荷不回调（共享件快照不被 null 污染）
    expect(cb).toHaveBeenCalledTimes(1)
  })

  it('followOnExit / shortcut / repo 三件辅助位打到正确 RPC', async () => {
    const adapter = createAirsnipAdapter()
    svc.GetFollowOnExit.mockResolvedValue(false)
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    svc.CreateDesktopShortcut.mockResolvedValue(undefined)
    svc.RepositoryURL.mockResolvedValue('https://github.com/bd-dxg/airsnip')
    svc.OpenRepository.mockResolvedValue(undefined)

    expect(await adapter.extras!.followOnExit!.get()).toBe(false)
    expect(((await adapter.extras!.followOnExit!.set(true))!.message)).toContain('Hanxi 退出')
    expect(((await adapter.extras!.shortcut!.create())!.message)).toContain('快捷方式')
    expect(await adapter.extras!.repo!.url()).toBe('https://github.com/bd-dxg/airsnip')
    await adapter.extras!.repo!.open()
    expect(svc.OpenRepository).toHaveBeenCalledTimes(1)
  })
})
