// TranslucentTB adapter 特征测试（崩溃处置升级 2026-09-26 + 双形态 Wave 打包线）：
//  - 纯函数判据表：isAVCrash（退出码反解码门）、detectVirtualArtifacts
//    （虚拟显卡名单/非主屏零尺寸空壳监视器）、virtualArtifactWording（点名
//    措辞纪律：陈述在场不归罪）、cmpTBVersion、pickDowngradeTarget（C-迷你候选）、
//    hasMsixBundleAsset（N13 矩阵 .msixbundle 判据）、pickAVProbe（鉴别钮双语义取位：
//    打包对照优先、便携降级回退）；
//  - B1 banner 异步点名：failed+AV 挂上后 GetReport 恰一次（同笔崩溃负结果也
//    防重刷、新崩溃换键重检）；sysinfo 停用（reject）静默回通用文案不报错；
//  - msix 契约面：GetMsixState 失败静默降级（不抛不 toast、含绑定缺席同路）、
//    动词 busy 单飞、确认框语义（远程行装走确认框、AV 对照直钮免确认）、
//    安装成败均重读、后端拦截错误原样上抛；
//  - 2026-09-27 机主实证三件套：孤儿数据档归因判据（isMsixOrphanAttribution）
//    与清理钮双闸（失败归因 × orphans 探测）、cleanOrphan 确认/回执/拒动透传、
//    msix-progress 事件入 ref 且动词落定即清、banner external 分形态
//    （打包版在册=「已由 Hanxi 唤起」ok 档；未装/预读缺席保守回原文案）。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createTBAdapter,
  hasMsixBundleAsset,
  isAVCrash,
  isMsixOrphanAttribution,
  detectVirtualArtifacts,
  pickAVProbe,
  virtualArtifactWording,
  cmpTBVersion,
  pickDowngradeTarget,
  MSIX_ORPHAN_MARKER,
} from '../translucenttb'
import type { ManagedReleaseRecord } from '../../components/managed/adapter'
import { useWailsEvent } from '../../composables/useWailsEvent'

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
  Quit: vi.fn(),
  ResetState: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
  // 打包线冻结契约五动词（bindings 待再生，spec 全 mock 顶替）+ 孤儿隔离新出口
  GetMsixState: vi.fn(),
  InstallMsix: vi.fn(),
  UninstallMsix: vi.fn(),
  RemoveMsixCache: vi.fn(),
  LaunchMsix: vi.fn(),
  CleanMsixOrphan: vi.fn(),
}))

const sysinfo = vi.hoisted(() => ({ GetReport: vi.fn() }))

const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/translucenttb/translucenttbservice', () => svc)
vi.mock('../../../bindings/hanxi/internal/modules/sysinfo/sysinfoservice', () => sysinfo)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

/** 一次 AV 崩溃的后端话术形状（逐字透传语境，仅截关键片段）。 */
const avError = 'TranslucentTB 异常退出（退出码 3221225477 / 0xC0000005），访问违例，非许可/依赖问题。排查按序——① 重启电脑后再启动一次'

function avSnap(extra: Record<string, unknown> = {}) {
  return {
    state: 'failed',
    version: '2026.2',
    pid: 0,
    exitCode: 3221225477,
    error: avError,
    external: false,
    startedAt: '2026-09-26T10:00:00Z',
    stoppedAt: '2026-09-26T10:00:03Z',
    ...extra,
  }
}

const emptyCtx = { installed: [], releases: [], active: '' } as never

async function flush(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

beforeEach(() => {
  vi.clearAllMocks()
  // sysinfo 缺省按"模块被停用（调用门拒）"处理——B1 的静默降级基线
  sysinfo.GetReport.mockRejectedValue(new Error('模块已停用'))
})

describe('isAVCrash', () => {
  it('failed+正 32 位 0xC0000005 判真；负数表征同判（解码口径同源）', () => {
    expect(isAVCrash({ state: 'failed', exitCode: 3221225477 })).toBe(true)
    expect(isAVCrash({ state: 'failed', exitCode: -1073741819 })).toBe(true)
  })
  it('非 failed 态/其他码/无码判假', () => {
    expect(isAVCrash({ state: 'running', exitCode: 3221225477 })).toBe(false)
    expect(isAVCrash({ state: 'failed', exitCode: 1 })).toBe(false)
    expect(isAVCrash({ state: 'failed' })).toBe(false)
  })
})

describe('detectVirtualArtifacts（判据表）', () => {
  it('desc 命中名单即点名（大小写不敏感）；正常显卡不误伤', () => {
    const arts = detectVirtualArtifacts({
      gpus: [
        { desc: 'Intel(R) Arc(TM) A770 Graphics' },
        { desc: 'Todesk Virtual Display Adapter' },
        { desc: 'OrayIddDriver Device' },
        { desc: 'Sunlogin...' },
        { desc: 'IddSampleDriver' },
        { desc: 'GameViewer Virtual Monitor' },
        { desc: 'tapgame capture' },
        { desc: 'NVIDIA GeForce RTX 4090' },
        null,
      ],
    })
    expect(arts.virtualGpus).toEqual([
      'Todesk Virtual Display Adapter',
      'OrayIddDriver Device',
      'Sunlogin...',
      'IddSampleDriver',
      'GameViewer Virtual Monitor',
      'tapgame capture',
    ])
    expect(arts.shellDisplays).toEqual([])
  })
  it('非主屏且宽/高为 0 → 空壳监视器；主屏零尺寸/有尺寸副屏不入列', () => {
    const arts = detectVirtualArtifacts({
      displays: [
        { name: '\\\\.\\DISPLAY1', width: 2560, height: 1440, primary: true },
        { name: '\\\\.\\DISPLAY2', width: 1920, height: 1080, primary: false },
        { name: '\\\\.\\DISPLAY3', width: 0, height: 0, primary: false },
        { name: '\\\\.\\DISPLAY4', width: 1280, height: 0, primary: false },
        { name: '\\\\.\\DISPLAY5', width: 0, height: 720, primary: true },
      ],
    })
    expect(arts.shellDisplays).toEqual(['\\\\.\\DISPLAY3', '\\\\.\\DISPLAY4'])
  })
  it('Report 段级降级（null/缺段）不炸，判空', () => {
    expect(detectVirtualArtifacts({})).toEqual({ virtualGpus: [], shellDisplays: [] })
    expect(detectVirtualArtifacts({ gpus: null, displays: null })).toEqual({ virtualGpus: [], shellDisplays: [] })
  })
})

describe('virtualArtifactWording（措辞纪律：陈述在场，不归罪）', () => {
  it('两类命中并列点名，含在场事实与验证指引', () => {
    const text = virtualArtifactWording({ virtualGpus: ['Todesk Virtual Display Adapter'], shellDisplays: ['\\\\.\\DISPLAY3'] })
    expect(text).toContain('检见虚拟显卡「Todesk Virtual Display Adapter」')
    expect(text).toContain('空壳监视器「\\\\.\\DISPLAY3」')
    expect(text).toContain('（在场事实）')
    expect(text).toContain('与本机崩溃特征高度吻合（踩坑 #85）')
    expect(text).toContain('建议设备管理器禁用该显示器验证')
    // 归罪禁词：在场≠元凶
    expect(text).not.toMatch(/导致|就是.*(原因|元凶)|罪魁祸首/)
  })
  it('无命中回空串（banner 保持通用文案）', () => {
    expect(virtualArtifactWording({ virtualGpus: [], shellDisplays: [] })).toBe('')
  })
})

describe('cmpTBVersion / pickDowngradeTarget（C-迷你候选判据）', () => {
  function rel(version: string, isPre = false): ManagedReleaseRecord {
    return { version, published: '2026-01-01T00:00:00Z', size: 1, isPre }
  }
  it('YYYY.N 数值分段：2026.10 > 2026.2；非规范号不可比', () => {
    expect(cmpTBVersion('2026.10', '2026.2')).toBe(1)
    expect(cmpTBVersion('2025.1', '2026.2')).toBe(-1)
    expect(cmpTBVersion('imported-20260906-150405', '2026.2')).toBeNull()
  })
  it('取更早稳定版中的最高者；预发布与更新版本排除', () => {
    const best = pickDowngradeTarget('2026.2', [{ version: '2026.2' }], [
      rel('2027.1'), rel('2026.2'), rel('2026.1'), rel('2025.1'), rel('2026.3-beta', true),
    ])
    expect(best?.version).toBe('2026.1')
  })
  it('本机已有旧版在场 → 不出钮（后端话术已点名直给，不重复造）', () => {
    expect(pickDowngradeTarget('2026.2', [{ version: '2026.2' }, { version: '2025.1' }], [rel('2025.1'), rel('2026.1')])).toBeNull()
  })
  it('无更早稳定候选（远程未含/imported- 崩溃版本/空列表）→ null 不硬造', () => {
    expect(pickDowngradeTarget('2025.1', [], [rel('2025.1'), rel('2026.2')])).toBeNull()
    expect(pickDowngradeTarget('imported-20260906-150405', [], [rel('2025.1')])).toBeNull()
    expect(pickDowngradeTarget('2026.2', [], [])).toBeNull()
    expect(pickDowngradeTarget('', [], [rel('2025.1')])).toBeNull()
  })
})

describe('B1 banner 异步点名', () => {
  it('failed+AV：挂上后恰一次 GetReport，命中即追加点名句且不重检', async () => {
    sysinfo.GetReport.mockResolvedValue({
      gpus: [{ desc: 'ToDesk Virtual Display Adapter' }],
      displays: [{ name: '\\\\.\\DISPLAY9', width: 0, height: 0, primary: false }],
    })
    const adapter = createTBAdapter()
    const snap = avSnap() as never

    const first = adapter.banner!(snap, emptyCtx)
    expect(first?.text).toBe(avError) // 首帧逐字后端话术，体检在途
    await flush()
    const second = adapter.banner!(snap, emptyCtx)
    expect(second?.text).toContain(avError) // 后端动态话术逐字保真
    expect(second?.text).toContain('检见虚拟显卡「ToDesk Virtual Display Adapter」')
    expect(second?.text).toContain('空壳监视器「\\\\.\\DISPLAY9」')
    expect(sysinfo.GetReport).toHaveBeenCalledTimes(1) // 同笔崩溃负/正结果都防重刷

    adapter.banner!({ ...avSnap(), state: 'running' } as never, emptyCtx)
    expect(sysinfo.GetReport).toHaveBeenCalledTimes(1) // 离开 failed 不再打扰
  })

  it('崩溃换键（新 stoppedAt）重检一次', async () => {
    sysinfo.GetReport.mockResolvedValue({ gpus: [{ desc: 'Sunlogin Virtual GPU' }], displays: [] })
    const adapter = createTBAdapter()
    adapter.banner!(avSnap() as never, emptyCtx)
    await flush()
    adapter.banner!(avSnap({ stoppedAt: '2026-09-26T12:00:00Z' }) as never, emptyCtx)
    await flush()
    expect(sysinfo.GetReport).toHaveBeenCalledTimes(2)
  })

  it('sysinfo 停用（调用门拒）→ 静默降级回通用文案，不报错', async () => {
    const adapter = createTBAdapter()
    const snap = avSnap() as never
    adapter.banner!(snap, emptyCtx)
    await flush()
    const after = adapter.banner!(snap, emptyCtx)
    expect(after?.tone).toBe('error')
    expect(after?.text).toBe(avError) // 通用话术原样，无追加无异常
    expect(sysinfo.GetReport).toHaveBeenCalledTimes(1)
  })

  it('体检无命中 → 不追加句', async () => {
    sysinfo.GetReport.mockResolvedValue({ gpus: [{ desc: 'NVIDIA GeForce RTX 4090' }], displays: [] })
    const adapter = createTBAdapter()
    const snap = avSnap() as never
    adapter.banner!(snap, emptyCtx)
    await flush()
    expect(adapter.banner!(snap, emptyCtx)?.text).toBe(avError)
  })

  it('非 AV 的 failed：不调 GetReport，逐字透传后端错误（既有契约）', async () => {
    const adapter = createTBAdapter()
    const res = adapter.banner!(
      { state: 'failed', version: '2026.2', pid: 0, exitCode: 1, error: 'TranslucentTB 异常退出（退出码 1）。若刚关闭了首次启动的欢迎授权窗口', external: false, startedAt: '', stoppedAt: '' } as never,
      emptyCtx,
    )
    expect(res?.text).toContain('退出码 1')
    await flush()
    expect(sysinfo.GetReport).not.toHaveBeenCalled()
  })
})

// ---------- 双形态 Wave：打包线契约面（判据纯函数 + adapter.msix） ----------

// 参数刻意收 unknown：null 元素是 hasMsixBundleAsset 的防御性判据面（超出 ReleaseAssetNote[] 类型契约），显式收窄回 assets 类型。
function msixRel(version: string, assets?: unknown): ManagedReleaseRecord {
  return { version, published: '2026-08-14T00:00:00Z', size: 1, assets: (assets ?? null) as ManagedReleaseRecord['assets'] }
}
const BUNDLE_ASSET = { platform: 'windows', form: 'package', label: 'TranslucentTB_2026.2.0.0_x64.msixbundle' }
const PORTABLE_ASSET = { platform: 'windows', form: 'portable', label: 'TranslucentTB-portable-x64.zip' }

describe('hasMsixBundleAsset（N13 矩阵打包资产判据）', () => {
  it('label 以 .msixbundle 收尾判真（大小写不敏感）；null 成员不炸', () => {
    expect(hasMsixBundleAsset(msixRel('2026.2', [PORTABLE_ASSET, null, BUNDLE_ASSET]))).toBe(true)
    expect(hasMsixBundleAsset(msixRel('2026.2', [{ ...BUNDLE_ASSET, label: 'TTB.MSIXBUNDLE' }]))).toBe(true)
  })
  it('无 assets（undefined/null/空数组）或仅便携/安装器资产 → false（静默缺席不装样子）', () => {
    expect(hasMsixBundleAsset(msixRel('2026.2'))).toBe(false)
    expect(hasMsixBundleAsset(msixRel('2026.2', null))).toBe(false)
    expect(hasMsixBundleAsset(msixRel('2026.2', []))).toBe(false)
    expect(hasMsixBundleAsset(msixRel('2026.2', [PORTABLE_ASSET, { form: 'installer', label: 'setup.msi' }]))).toBe(false)
    expect(hasMsixBundleAsset(null)).toBe(false)
    expect(hasMsixBundleAsset(undefined)).toBe(false)
  })
})

describe('pickAVProbe（崩溃鉴别钮：打包对照优先、便携降级回退）', () => {
  function rel(version: string, isPre = false): ManagedReleaseRecord {
    return { version, published: '2026-01-01T00:00:00Z', size: 1, isPre }
  }
  const withBundle = msixRel('2026.2', [PORTABLE_ASSET, BUNDLE_ASSET])
  const installedAt262 = [{ version: '2026.2' }]

  it('msixReady 且崩溃版本行有 msixbundle → msix 对照（即便同时存在便携降级候选也置顶）', () => {
    const p = pickAVProbe({ crashVersion: '2026.2', msixReady: true, installed: installedAt262, releases: [withBundle, rel('2026.1')] })
    expect(p).toEqual({ mode: 'msix', version: '2026.2', target: null })
  })
  it('msixReady 但崩溃版本无 msixbundle 资产/不在远程 → 逐字回退便携降级候选', () => {
    const p = pickAVProbe({ crashVersion: '2026.2', msixReady: true, installed: installedAt262, releases: [msixRel('2026.2', [PORTABLE_ASSET]), rel('2026.1')] })
    expect(p?.mode).toBe('portable')
    expect(p?.version).toBe('2026.1')
  })
  it('未 ready（预读失败或打包版已装）→ 便携降级路径行为与双形态前完全一致', () => {
    const p = pickAVProbe({ crashVersion: '2026.2', msixReady: false, installed: installedAt262, releases: [withBundle, rel('2026.1')] })
    expect(p).toEqual({ mode: 'portable', version: '2026.1', target: expect.objectContaining({ version: '2026.1' }) })
  })
  it('两路皆无候选（无旧版可降且无对照条件）→ null 不硬造', () => {
    expect(pickAVProbe({ crashVersion: '2026.2', msixReady: false, installed: installedAt262, releases: [withBundle] })).toBeNull()
    expect(pickAVProbe({ crashVersion: '', msixReady: true, installed: [], releases: [withBundle] })).toBeNull()
  })
})

describe('msix 契约面（adapter.msix：降级/单飞/确认/重读）', () => {
  const notInstalled = { installed: false, version: '', packageFamily: '', cache: [] as never[] }

  it('refresh 成功落 state、probeReady 随 installed 翻转；回 null 亦按失败降级', async () => {
    svc.GetMsixState.mockResolvedValue(notInstalled)
    const a = createTBAdapter()
    await a.msix.refresh()
    expect(a.msix.state.value).toEqual(notInstalled)
    expect(a.msix.unavailable.value).toBe(false)
    expect(a.msix.probeReady.value).toBe(true)

    svc.GetMsixState.mockResolvedValue({ installed: true, version: '2026.2', packageFamily: 'x', cache: [] })
    await a.msix.refresh()
    expect(a.msix.probeReady.value).toBe(false)

    svc.GetMsixState.mockResolvedValue(null)
    await a.msix.refresh()
    expect(a.msix.state.value).toBeNull()
    expect(a.msix.unavailable.value).toBe(true)
    expect(a.msix.probeReady.value).toBe(false)
  })

  it('refresh 失败静默降级（reject 与再生前绑定缺席的 TypeError 同路），不抛不 toast', async () => {
    svc.GetMsixState.mockRejectedValue(new Error('Appx 包状态查询失败'))
    const a = createTBAdapter()
    await expect(a.msix.refresh()).resolves.toBeUndefined()
    expect(a.msix.unavailable.value).toBe(true)

    // 再生前 bindings 命名空间缺该函数 → 调用即 TypeError，同 catch 吞掉降级
    delete (svc as Record<string, unknown>).GetMsixState
    await expect(a.msix.refresh()).resolves.toBeUndefined()
    expect(a.msix.unavailable.value).toBe(true)
    svc.GetMsixState = vi.fn() // 复位给后续用例（hoisted 对象就地回补）
  })

  it('installFromRelease：确认框先行（互不干扰话术），取消不动 RPC；确认装 → 完成重读 + 回执', async () => {
    svc.GetMsixState.mockResolvedValue(notInstalled)
    svc.InstallMsix.mockResolvedValue(undefined)
    const a = createTBAdapter()

    ui.confirm.mockResolvedValue(false)
    expect(await a.msix.installFromRelease('2026.2')).toEqual({})
    expect(svc.InstallMsix).not.toHaveBeenCalled()

    ui.confirm.mockResolvedValue(true)
    const readsBefore = svc.GetMsixState.mock.calls.length
    const res = await a.msix.installFromRelease('2026.2')
    expect(ui.confirm.mock.calls[0][0].title).toBe('安装 TranslucentTB 2026.2 打包版？')
    expect(ui.confirm.mock.calls[0][0].description).toContain('互不干扰')
    expect(svc.InstallMsix).toHaveBeenCalledWith('2026.2')
    expect(res.message).toBe('打包版 2026.2 已安装（便携版未受影响）')
    expect(svc.GetMsixState.mock.calls.length).toBe(readsBefore + 1)
  })

  it('probeInstall：对照直钮免确认，成功文案即对照步骤完成宣告', async () => {
    svc.GetMsixState.mockResolvedValue(notInstalled)
    svc.InstallMsix.mockResolvedValue(undefined)
    const a = createTBAdapter()
    const res = await a.msix.probeInstall('2026.2')
    expect(ui.confirm).not.toHaveBeenCalled()
    expect(svc.InstallMsix).toHaveBeenCalledWith('2026.2')
    expect(res.message).toContain('#85 对照步骤完成')
  })

  it('安装失败也重读区块（失败笔可能留下已校验缓存），错误原样上抛交视图裸串 toast', async () => {
    svc.GetMsixState.mockResolvedValue(notInstalled)
    svc.InstallMsix.mockRejectedValue(new Error('下载校验失败：digest 不匹配'))
    const a = createTBAdapter()
    await expect(a.msix.probeInstall('2026.2')).rejects.toThrow('下载校验失败：digest 不匹配')
    expect(svc.GetMsixState).toHaveBeenCalledTimes(1) // 仅失败路径内的 finally 重读
    expect(a.msix.busy.value).toBe(false) // 闩已释放
  })

  it('busy 单飞闩：安装进行中的二次动词回空回执（RPC 不双发），落定后释放', async () => {
    svc.GetMsixState.mockResolvedValue(notInstalled)
    let settleInstall: (v: undefined) => void = () => {}
    svc.InstallMsix.mockImplementation(() => new Promise<void>((res) => { settleInstall = res }))
    const a = createTBAdapter()
    const inflight = a.msix.probeInstall('2026.2')
    expect(a.msix.busy.value).toBe(true)
    expect(await a.msix.launch()).toEqual({}) // 复入被闩
    expect(svc.LaunchMsix).not.toHaveBeenCalled()
    settleInstall(undefined)
    await expect(inflight).resolves.toMatchObject({ message: expect.stringContaining('已安装') })
    expect(a.msix.busy.value).toBe(false)
  })

  it('uninstall：danger 确认（点明便携与缓存不碰）→ UninstallMsix → 重读；取消静默', async () => {
    svc.GetMsixState.mockResolvedValue({ installed: true, version: '2026.2', packageFamily: 'x', cache: [] })
    svc.UninstallMsix.mockResolvedValue(undefined)
    const a = createTBAdapter()
    await a.msix.refresh()

    ui.confirm.mockResolvedValue(false)
    expect(await a.msix.uninstall()).toEqual({})
    expect(svc.UninstallMsix).not.toHaveBeenCalled()

    ui.confirm.mockResolvedValue(true)
    const res = await a.msix.uninstall()
    const opts = ui.confirm.mock.calls[ui.confirm.mock.calls.length - 1][0]
    expect(opts.title).toBe('卸载打包版 TranslucentTB？')
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain('托管便携版文件与安装包缓存均不受影响')
    expect(svc.UninstallMsix).toHaveBeenCalledTimes(1)
    expect(res.message).toBe('打包版已卸载（便携版未受影响）')
  })

  it('launch/removeCache：成功回执与后端拦截错误原样上抛（文案零加工作坊）', async () => {
    svc.GetMsixState.mockResolvedValue(notInstalled)
    svc.LaunchMsix.mockResolvedValue(undefined)
    const a = createTBAdapter()
    const lr = await a.msix.launch()
    expect(svc.LaunchMsix).toHaveBeenCalledTimes(1)
    expect(lr.message).toContain('已提交打包版启动请求')

    svc.RemoveMsixCache.mockRejectedValue(new Error('打包版 2026.2 正在运行，其安装包暂不可移除'))
    await expect(a.msix.removeCache('2026.2')).rejects.toThrow('打包版 2026.2 正在运行，其安装包暂不可移除')

    svc.RemoveMsixCache.mockResolvedValue(undefined)
    const rr = await a.msix.removeCache('2026.1')
    expect(svc.RemoveMsixCache).toHaveBeenCalledWith('2026.1')
    expect(rr.message).toBe('已移除打包版安装包缓存 2026.1')
    expect(svc.GetMsixState).toHaveBeenCalledTimes(2) // launch 不重读；removeCache 成败各重读一次
  })
})

// ---------- 2026-09-27 机主实证批次：孤儿档 / 清理链 / 进度事件 / external 分形态 ----------

const ORPHAN_STATE = {
  installed: false,
  version: '',
  packageFamily: '28017CharlesMilette.TranslucentTB_v826wp6bftszj',
  cache: [],
  orphans: true,
  orphanPath: 'C:\\Users\\tester\\AppData\\Local\\Packages\\28017CharlesMilette.TranslucentTB_v826wp6bftszj',
}

/** 后端 msixOrphanGuide 孤儿档全句样本（含前端同文钉标记短语与钮指引）。 */
const ORPHAN_FAIL =
  'TranslucentTB 打包版安装失败: Windows 包操作失败: 部署失败（0x80073CF6）；' +
  '检测到上次安装的孤儿数据阻碍注册：包族已不在册，但 %LOCALAPPDATA%\\Packages 下残留的旧应用数据挡住了' +
  "部署链的'删旧数据'步骤（缺组件删不动）；点「🧹 清理后重试」把孤儿数据改名隔离（备份 .orphan-日期，不删内容、可手动删除）后重装"

/** 2026-09-27 改判后基础设施档（无冤枉路、无孤儿短语）。 */
const INFRA_FAIL =
  'TranslucentTB 打包版安装失败: Windows 包操作失败（0x80073CF6）；本系统缺少包注册基础设施（微软商店/侧载部署栈残缺），本机打包线不可用，便携版即正解'

describe('isMsixOrphanAttribution（两档归因的前端半边）', () => {
  it('孤儿档短语命中；基础设施档/空串不命中', () => {
    expect(isMsixOrphanAttribution(ORPHAN_FAIL)).toBe(true)
    expect(isMsixOrphanAttribution(INFRA_FAIL)).toBe(false)
    expect(isMsixOrphanAttribution('')).toBe(false)
  })
  it('标记短语与后端 msixOrphanGuide 同文钉（常量化，禁止两处漂移）', () => {
    expect(MSIX_ORPHAN_MARKER).toBe('检测到上次安装的孤儿数据阻碍注册')
    expect(ORPHAN_FAIL).toContain(MSIX_ORPHAN_MARKER)
  })
})

describe('msix 孤儿清理链（installFailure 记账 × orphanRetry 双闸 × cleanOrphan）', () => {
  it('安装失败按话术分档记账；双闸缺一不放行钮', async () => {
    svc.GetMsixState.mockResolvedValue({ ...ORPHAN_STATE, orphans: false, orphanPath: undefined })
    svc.InstallMsix.mockRejectedValue(new Error(ORPHAN_FAIL))
    const a = createTBAdapter()
    await a.msix.refresh()
    await expect(a.msix.probeInstall('2026.2')).rejects.toThrow()
    expect(a.msix.installFailure.value).toEqual({ message: ORPHAN_FAIL, orphan: true })
    expect(a.msix.orphanRetry.value).toBe(false) // 探测说无孤儿：话术再像也不出钮（防误导钮指空）

    svc.GetMsixState.mockResolvedValue(ORPHAN_STATE)
    await a.msix.refresh()
    expect(a.msix.orphanRetry.value).toBe(true) // 双闸齐备才放行

    svc.InstallMsix.mockRejectedValue(new Error(INFRA_FAIL))
    await expect(a.msix.probeInstall('2026.2')).rejects.toThrow()
    expect(a.msix.installFailure.value?.orphan).toBe(false)
    expect(a.msix.orphanRetry.value).toBe(false) // 基础设施档失败：即便 orphans 在场也不出钮（按档分流）
  })

  it('安装成功销失败账（旧孤儿横幅不陪跑新事实）', async () => {
    svc.GetMsixState.mockResolvedValue(ORPHAN_STATE)
    svc.InstallMsix.mockRejectedValue(new Error(ORPHAN_FAIL))
    const a = createTBAdapter()
    await a.msix.refresh()
    await expect(a.msix.probeInstall('2026.2')).rejects.toThrow()
    expect(a.msix.orphanRetry.value).toBe(true)
    svc.InstallMsix.mockResolvedValue(undefined)
    await a.msix.probeInstall('2026.2')
    expect(a.msix.installFailure.value).toBeNull()
    expect(a.msix.orphanRetry.value).toBe(false)
  })

  it('cleanOrphan：danger 确认点名服务端精确路径与"改名隔离"语义；取消不动 RPC；确认→隔离→重读→回执销账', async () => {
    svc.GetMsixState.mockResolvedValue(ORPHAN_STATE)
    const a = createTBAdapter()
    await a.msix.refresh()

    ui.confirm.mockResolvedValue(false)
    expect(await a.msix.cleanOrphan()).toEqual({})
    expect(svc.CleanMsixOrphan).not.toHaveBeenCalled()

    ui.confirm.mockResolvedValue(true)
    svc.CleanMsixOrphan.mockResolvedValue(undefined)
    svc.GetMsixState.mockResolvedValue({ ...ORPHAN_STATE, orphans: false, orphanPath: undefined })
    const res = await a.msix.cleanOrphan()
    const opts = ui.confirm.mock.calls[ui.confirm.mock.calls.length - 1][0]
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain(ORPHAN_STATE.orphanPath) // 精确路径点名（服务端实值，不前端拼）
    expect(opts.description).toContain('已核实')
    expect(opts.description).toContain('改名隔离')
    expect(svc.CleanMsixOrphan).toHaveBeenCalledTimes(1)
    expect(res.message).toContain('清理完成')
    expect(res.message).toContain('请再点安装')
    expect(a.msix.installFailure.value).toBeNull()
    expect(a.msix.orphanRetry.value).toBe(false) // 钮消失（探测翻假）
  })

  it('cleanOrphan 后端拒动（包仍在册/探测受阻）：中文实话原样上抛，仍重读区块', async () => {
    svc.GetMsixState.mockResolvedValue(ORPHAN_STATE)
    svc.CleanMsixOrphan.mockRejectedValue(new Error('TranslucentTB 打包版包仍在册，请先直接卸载（孤儿隔离不动在册包的数据）'))
    const a = createTBAdapter()
    await a.msix.refresh()
    ui.confirm.mockResolvedValue(true)
    await expect(a.msix.cleanOrphan()).rejects.toThrow('包仍在册')
    expect(svc.GetMsixState).toHaveBeenCalledTimes(2) // 拒动后事实也要重读
  })
})

describe('msix-progress 事件通道（订阅入 ref；动词落定即清）', () => {
  it('adapter 注册打包进度订阅；合法事件入 ref、坏形状丢弃；动词 finally 清空进度行', async () => {
    const a = createTBAdapter()
    const call = vi
      .mocked(useWailsEvent)
      .mock.calls
      .find((c) => c[0] === 'translucenttb:msix-progress')
    expect(call, 'adapter 应注册 translucenttb:msix-progress 订阅').toBeTruthy()
    const fire = call![1] as (p: unknown) => void

    fire({ stage: 'deploying', percent: 0, message: 'Windows 正在部署包（注册中，此段无百分比反馈）…' })
    expect(a.msix.progress.value?.stage).toBe('deploying')
    fire(null as never)
    fire({ message: '无阶段词' } as never)
    expect(a.msix.progress.value?.stage).toBe('deploying') // 坏形状静默忽略不覆写

    svc.GetMsixState.mockResolvedValue(ORPHAN_STATE)
    svc.InstallMsix.mockResolvedValue(undefined)
    await a.msix.probeInstall('2026.2')
    expect(a.msix.progress.value).toBeNull() // busy 落定即收行（成败皆然走 finally）
  })
})

describe('banner external 分形态（2026-09-27 机主实拍改判）', () => {
  const extSnap = { state: 'external', version: '', pid: 0, error: '', external: true, startedAt: '', stoppedAt: '' } as never

  it('external + 打包版在册 → ok 档「已由 Hanxi 唤起」，不再喊"非 Hanxi 托管"', async () => {
    svc.GetMsixState.mockResolvedValue({ installed: true, version: '2026.2', packageFamily: 'f', cache: [] })
    const a = createTBAdapter()
    await a.msix.refresh()
    const b = a.banner!(extSnap, emptyCtx)
    expect(b?.tone).toBe('ok')
    expect(b?.text).toContain('打包版 TranslucentTB 正在运行（已由 Hanxi 唤起）')
    expect(b?.text).toContain('生命周期归 Windows')
    expect(b?.text).not.toContain('非 Hanxi 托管')
  })

  it('external + 打包版未装 → 原外部便携实例 warn 话术逐字', async () => {
    svc.GetMsixState.mockResolvedValue({ installed: false, version: '', packageFamily: 'f', cache: [] })
    const a = createTBAdapter()
    await a.msix.refresh()
    const b = a.banner!(extSnap, emptyCtx)
    expect(b?.tone).toBe('warn')
    expect(b?.text).toBe('检测到外部 TranslucentTB 实例（非 Hanxi 托管）。可重设任务栏状态；如需彻底退出请在 TranslucentTB 托盘菜单操作。')
  })

  it('external + 预读未回/失败（state=null）→ 保守回原文案（不臆断来源）', async () => {
    svc.GetMsixState.mockRejectedValue(new Error('Appx 包状态查询失败'))
    const a = createTBAdapter()
    await a.msix.refresh()
    const b = a.banner!(extSnap, emptyCtx)
    expect(b?.tone).toBe('warn')
    expect(b?.text).toContain('非 Hanxi 托管')
  })
})
