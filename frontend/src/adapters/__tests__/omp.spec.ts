// ============================================================================
// omp adapter · 版本管理动作面（仅版本管理：无控制台，形状锁四件事）
//
//  - 动词逐一对号入座：setActive→SetActiveVersion / remove→DeleteVersion /
//    importLocal→ImportLocalVersion / openDir→OpenDir——打反不会报错只会静默
//    做错事（如误调 SetActiveVersion 代替 DeleteVersion），用例锁调用面；
//  - 危险/输入动作的取消闸：确认框否决、prompt 取消、空白串一律零 RPC；
//    路径先剥首尾空白再进后端（原样透传会换来看不懂的路径错误）；
//  - 回执词面与 activeVersion 走 managedCommon 单源（setActive RPC 返回
//    void，回执必须由入参版本构造——锁这条形状差异）；
//  - 事件名逐字 'omp:version-download'、单键形归一（key=version，铁律②），
//    空载荷/缺 version 票据丢弃。
//
// 注：RPC 面以 bindings/hanxi/internal/modules/omp 生成物为准；本 spec 经
// vi.mock 桩接 bindings 模块（无桥环境 RPC 不可达，mock 恒优先于真实件）。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  ListReleases: vi.fn(),
  ListInstalledVersions: vi.fn(),
  DownloadVersion: vi.fn(),
  DeleteVersion: vi.fn(),
  ImportLocalVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  GetStatus: vi.fn(),
  OpenDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
  InstallOfficial: vi.fn(),
  UpdateOfficial: vi.fn(),
  UninstallOfficial: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/omp/ompservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createOmpAdapter } from '../omp'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

beforeEach(() => {
  events.length = 0
  vi.clearAllMocks()
})

describe('omp · 版本动作逐一对号', () => {
  it('setActive 打 SetActiveVersion；RPC 返回 void，回执仍带 activeVersion 与标准词', async () => {
    const adapter = createOmpAdapter()
    svc.SetActiveVersion.mockResolvedValue(undefined) // 契约：void
    const out = await adapter.setActive('v1.2.3')
    expect(svc.SetActiveVersion).toHaveBeenCalledTimes(1)
    expect(svc.SetActiveVersion).toHaveBeenCalledWith('v1.2.3')
    expect(out.activeVersion).toBe('v1.2.3') // 由**入参**构造（回读通道缺席，锁这条形状）
    expect(out.message).toContain('v1.2.3')
  })

  it('download 词面回执：started 静默、already-installed 提示并要求重拉版本区', async () => {
    const adapter = createOmpAdapter()
    svc.DownloadVersion.mockResolvedValue('started')
    expect(await adapter.download('v1.2.3')).toEqual({})
    svc.DownloadVersion.mockResolvedValue('in-progress')
    expect(await adapter.download('v1.2.3')).toEqual({})
    svc.DownloadVersion.mockResolvedValue('already-installed')
    const out = await adapter.download('v1.2.3')
    expect(out.reloadVersions).toBe(true)
    expect(out.message).toContain('v1.2.3')
  })

  it('remove 确认后打 DeleteVersion（非 SetActive/Set 类动词），回执请求复刷', async () => {
    const adapter = createOmpAdapter()
    ui.confirm.mockResolvedValue(true)
    svc.DeleteVersion.mockResolvedValue(undefined)
    const out = await adapter.remove('v1.2.3')
    expect(svc.DeleteVersion).toHaveBeenCalledWith('v1.2.3')
    expect(svc.SetActiveVersion).not.toHaveBeenCalled()
    expect(out.message).toContain('v1.2.3')
    expect(out.reloadVersions).toBe(true)
  })

  it('openDir 路径逐字透传（仅目录语义由后端把关，前端不改写）', async () => {
    const adapter = createOmpAdapter()
    svc.OpenDir.mockResolvedValue(undefined)
    await adapter.openDir('D:\\hanxi-data\\versions\\omp_1.2.3')
    expect(svc.OpenDir).toHaveBeenCalledWith('D:\\hanxi-data\\versions\\omp_1.2.3')
  })

  it('读取面直传：listReleases/listInstalled/getStatus 各打各 RPC 并原样转发（视图侧单一状态源）', async () => {
    const adapter = createOmpAdapter()
    const releases = [{ version: 'v1.3.0' }]
    const installed = [{ version: 'v1.2.3', active: true }]
    svc.ListReleases.mockResolvedValue(releases)
    svc.ListInstalledVersions.mockResolvedValue(installed)
    await expect(adapter.listReleases()).resolves.toBe(releases)
    await expect(adapter.listInstalled()).resolves.toBe(installed)
    const status = { activeVersion: 'v1.2.3', exePath: 'D:\\omp.exe', dataDir: 'C:\\Users\\me\\.omp' }
    svc.GetStatus.mockResolvedValue(status)
    await expect(adapter.getStatus()).resolves.toBe(status)
  })

  it('仓库行两件：repositoryURL 直回串、openRepository 打对 RPC', async () => {
    const adapter = createOmpAdapter()
    svc.RepositoryURL.mockResolvedValue('https://github.com/can1357/oh-my-pi')
    svc.OpenRepository.mockResolvedValue(undefined)
    expect(await adapter.repositoryURL()).toBe('https://github.com/can1357/oh-my-pi')
    await adapter.openRepository()
    expect(svc.OpenRepository).toHaveBeenCalledTimes(1)
    expect(svc.OpenDir).not.toHaveBeenCalled()
  })
})

describe('omp · 取消闸与输入归一', () => {
  it('删除确认框否决时零 RPC（取消回静默回执，无 message 无 reload）', async () => {
    const adapter = createOmpAdapter()
    ui.confirm.mockResolvedValue(false)
    expect(await adapter.remove('v1.2.3')).toEqual({})
    expect(svc.DeleteVersion).not.toHaveBeenCalled()
  })

  it('importLocal：取消(null)/纯空白串都拦在 RPC 之前；有效路径剥首尾空白后原样进后端', async () => {
    const adapter = createOmpAdapter()

    ui.prompt.mockResolvedValue(null) // 取消/Esc
    expect(await adapter.importLocal()).toEqual({})
    expect(svc.ImportLocalVersion).not.toHaveBeenCalled()

    ui.prompt.mockResolvedValue('   ') // 只敲空格
    expect(await adapter.importLocal()).toEqual({})
    expect(svc.ImportLocalVersion).not.toHaveBeenCalled()

    ui.prompt.mockResolvedValue(' C:\\Users\\me\\Downloads\\omp-v1.2.3-x64\\omp.exe ')
    svc.ImportLocalVersion.mockResolvedValue({ version: 'v1.2.3', source: 'local' })
    const out = await adapter.importLocal()
    expect(svc.ImportLocalVersion).toHaveBeenCalledWith('C:\\Users\\me\\Downloads\\omp-v1.2.3-x64\\omp.exe')
    expect(out.message).toContain('v1.2.3')
    expect(out.reloadVersions).toBe(true)
  })
})

describe('omp · 下载进度事件通道', () => {
  it('事件名逐字锁定 omp:version-download；单键形归一 key=version', () => {
    const adapter = createOmpAdapter()
    const cb = vi.fn()
    adapter.subscribeProgress(cb)
    expect(lastHandler().name).toBe('omp:version-download')

    const t = { version: 'v1.2.3', stage: 'downloading', done: 512, total: 1024, message: '' }
    lastHandler().handler(t)
    expect(cb).toHaveBeenCalledWith({ key: 'v1.2.3', stage: 'downloading', done: 512, total: 1024, message: '' })
  })

  it('空载荷与缺 version 票据一律丢弃（共享 map 不被 undefined 键污染）', () => {
    const adapter = createOmpAdapter()
    const cb = vi.fn()
    adapter.subscribeProgress(cb)
    lastHandler().handler(null)
    lastHandler().handler({ stage: 'error', done: 0, total: 0, message: '炸了' } as never) // 缺 version
    expect(cb).not.toHaveBeenCalled()
  })
})

describe('omp · 官方安装方式（装/更/卸）', () => {
  it('installOfficial：started 回执带"后台进行"词面且不触发复刷（终态归事件流）', async () => {
    svc.InstallOfficial.mockResolvedValue('started')
    const adapter = createOmpAdapter()
    const out = await adapter.installOfficial()
    expect(svc.InstallOfficial).toHaveBeenCalledTimes(1)
    expect(out.reloadVersions).toBeFalsy()
    expect(out.message).toContain('后台')
  })

  it('install/update 撞车：后端报 in-progress 时如实转告"已有一趟在跑"，不是静默', async () => {
    svc.UpdateOfficial.mockResolvedValue('in-progress')
    const adapter = createOmpAdapter()
    const out = await adapter.updateOfficial()
    expect(out.message).toContain('已有一趟')
  })

  it('uninstallOfficial：确认框否决时零 RPC（删目录+改 PATH 是危险动作，取消不得落地）', async () => {
    ui.confirm.mockResolvedValue(false)
    const adapter = createOmpAdapter()
    const out = await adapter.uninstallOfficial()
    expect(ui.confirm).toHaveBeenCalled()
    expect(svc.UninstallOfficial).not.toHaveBeenCalled()
    expect(out).toEqual({})
  })

  it('uninstallOfficial：确认后透传后端中文回执并触发复刷（同步卸载跑完才返回）', async () => {
    ui.confirm.mockResolvedValue(true)
    svc.UninstallOfficial.mockResolvedValue('已删除 C:/Users/x/AppData/Local/omp；已从用户 PATH 剔除该目录并广播')
    const adapter = createOmpAdapter()
    const out = await adapter.uninstallOfficial()
    expect(out.message).toContain('PATH')
    expect(out.reloadVersions).toBe(true)
  })

  it('官方进度事件名逐字锁定 omp:official-op，订阅即透传载荷', () => {
    const adapter = createOmpAdapter()
    const cb = vi.fn()
    adapter.subscribeOfficialOp(cb)
    expect(events[events.length - 1]!.name).toBe('omp:official-op')
    events[events.length - 1]!.handler({ op: 'install', stage: 'running', message: 'x' })
    expect(cb).toHaveBeenCalled()
  })
})
