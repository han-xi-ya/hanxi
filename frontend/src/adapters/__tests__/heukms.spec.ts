// ============================================================================
// HEU 托管 adapter · 下载链与事件面契约锁
//
// 本模块的形态在集成中途被实测纠正过一次（初判"上游 rar 解不了、只能导入"，
// 实测 nwaples/rardecode 可解 RAR5 且产物哈希与上游公布值一致，于是恢复标准
// 下载链，取证与反思见 docs/TROUBLESHOOTING.md #108）。因此这里要锁的是
// **"下载是真的"**这一面，防止退化回临时禁用态：
//   ① download 存在且真调 DownloadVersion RPC（不是抛错的占位）；
//   ② 不声明 downloadBlock——远程表每一行都是可安装版本（后端已按"有 rar + 有
//      官方摘要"筛过），不存在"看得见点不动"的行；
//   ③ 不覆写 downloadLabel/firstUseDownloadLabel 词族（那是禁用态时代的补丁话术）；
//   ④ 两个事件各订一次：heukms:instance-state 与 heukms:version-download，
//      进度键 = version（单键最简形态）；
//   ⑤ 'already-installed' 同步终态回执要转成"提示 + 重拉"，不能让票据空挂；
//   ⑥ 文档话术不得残留"不代下载"式过时声明。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createHeuKmsAdapter } from '../heukms'

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
  Shutdown: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
  OpenReleasePage: vi.fn(),
}))
const useWailsEvent = vi.hoisted(() => vi.fn())
const prompt = vi.hoisted(() => vi.fn())

vi.mock('../../../bindings/hanxi/internal/modules/heukms/heukmsservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ({ confirm: vi.fn() }) }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ({ prompt }) }))

describe('createHeuKmsAdapter · 下载链契约', () => {
  let adapter: ReturnType<typeof createHeuKmsAdapter>

  beforeEach(() => {
    vi.clearAllMocks()
    svc.ListInstalledVersions.mockResolvedValue([])
    svc.ListReleases.mockResolvedValue([])
    svc.GetActiveVersion.mockResolvedValue('')
    adapter = createHeuKmsAdapter()
  })

  it('① download 真调 RPC 并回静默回执（进度由事件收口）', async () => {
    svc.DownloadVersion.mockResolvedValue('started')
    const res = await adapter.versions.download({ version: '64.0.0' } as never)
    expect(svc.DownloadVersion).toHaveBeenCalledWith('64.0.0')
    expect(res).toEqual({})
  })

  it('⑤ already-installed 同步终态转提示 + 重拉版本区', async () => {
    svc.DownloadVersion.mockResolvedValue('already-installed')
    const res = await adapter.versions.download({ version: '63.1.0' } as never)
    expect(res.message).toContain('已安装')
    expect(res.reloadVersions).toBe(true)
  })

  it('② 不声明 downloadBlock（远程表全是可安装版本）', () => {
    expect(adapter.versions.downloadBlock).toBeUndefined()
  })

  it('③ 词族不覆写，沿用共享件默认「下载安装 / 下载最新版」', () => {
    expect(adapter.copy?.downloadLabel).toBeUndefined()
    expect(adapter.copy?.firstUseDownloadLabel).toBeUndefined()
  })

  it('④ 事件面各订一次，进度键为 version', () => {
    const progress = vi.fn()
    adapter.subscribeProgress(progress)
    adapter.subscribeInstanceState(vi.fn())
    expect(useWailsEvent).toHaveBeenCalledTimes(2)
    const names = useWailsEvent.mock.calls.map((c) => c[0] as string).sort()
    expect(names).toEqual(['heukms:instance-state', 'heukms:version-download'])

    // 回放一条进度事件 → 归一为 NormalizedProgress（key=version，阶段词逐字透传）
    const cb = useWailsEvent.mock.calls.find((c) => c[0] === 'heukms:version-download')![1] as (p: unknown) => void
    cb({ version: '64.0.0', stage: 'downloading', done: 1024, total: 5112665, message: '' })
    expect(progress).toHaveBeenCalledWith({ key: '64.0.0', stage: 'downloading', done: 1024, total: 5112665, message: '' })
    // 空票据（缺 version）不得投给面板
    progress.mockClear()
    cb({ version: '', stage: 'done' })
    expect(progress).not.toHaveBeenCalled()
  })

  it('⑥ 提示话术不再残留「不代下载」，且如实交代 rar 通道与兜底路径', () => {
    const all = (adapter.copy?.metaHints ?? []).join(' ') + ' ' + (adapter.copy?.firstUseEmpty ?? '')
    expect(all).not.toContain('不代下载')
    expect(all).toContain('rar')
    expect(all).toContain('导入本地')
    // 62.0.0 及更早无摘要版本不进可安装列表——这条事实必须在话术里可见
    expect(all).toContain('摘要')
  })

  it('导入仍是兜底通道，提示保留上游原始文件名约束', async () => {
    prompt.mockResolvedValue('F:\\')
    svc.ImportLocal.mockResolvedValue({
      version: '64.0.0', dir: 'x', exePath: 'x/a.exe', exeName: 'HEU_KMS_Activator_v64.0.0.exe',
      size: 1, installedAt: '', isImport: true, source: 'imported', sha256: '',
    })
    const res = await adapter.versions.importLocal!()
    const arg = prompt.mock.calls[0][0] as { title: string; description: string }
    expect(arg.description).toContain('HEU_KMS_Activator')
    expect(arg.description).toContain('保留上游原始文件名')
    expect(res.reloadVersions).toBe(true)
    expect(svc.ImportLocal).toHaveBeenCalledWith('F:\\')
  })

  it('用户取消路径输入时静默返回，不发起任何 RPC', async () => {
    prompt.mockResolvedValue(null)
    const res = await adapter.versions.importLocal!()
    expect(res).toEqual({})
    expect(svc.ImportLocal).not.toHaveBeenCalled()
  })
})
