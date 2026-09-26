// ============================================================================
// 果核看图 adapter · N13 形态矩阵透传契约
//
// listReleases 是本轮唯一改写行对象形态的投影（published 空串占位注入），
// 必须验证后端 ViewRelease.Assets（hostfeed 矩阵）在该改写中逐条存活——
// 丢一条，面板「上游发布」列就静默降级为空白。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createGuoheViewAdapter } from '../guoheview'
import type { ReleaseAssetNote } from '../../components/managed/adapter'

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
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/guoheview/guoheviewservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ({ confirm: vi.fn() }) }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ({ prompt: vi.fn() }) }))

/** 官方接口三发布物形态（与真实 files 数组同构，bindings 再生前以方言注记型携带 assets）。 */
function fakeRow(version: string, assets: ReleaseAssetNote[]) {
  return {
    version,
    channel: 'stable',
    isPre: false,
    assetName: `GuoheView_${version}-便携版.zip`,
    assetUrl: 'https://rj.lovestu.com/api/v1/release-files/152/download',
    size: 7211590,
    md5: '9675b658217ae2bb25a2258f8663aee3',
    assets,
  } as never
}

describe('createGuoheViewAdapter · N13 assets 透传', () => {
  beforeEach(() => vi.clearAllMocks())

  it('listReleases 注入 published 占位的同时逐条保留形态矩阵（含 Managed 高亮位）', async () => {
    const assets: ReleaseAssetNote[] = [
      { platform: 'windows', form: 'installer', label: 'GuoheView_v3.3.3.105-安装包.exe' },
      { platform: 'windows', form: 'portable', label: 'GuoheView_v3.3.3.105-便携版.7z' },
      { platform: 'windows', form: 'portable', label: 'GuoheView_v3.3.3.105-便携版.zip', managed: true },
    ]
    svc.ListReleases.mockResolvedValueOnce([fakeRow('v3.3.3.105', assets)])
    const adapter = createGuoheViewAdapter()
    const rows = (await adapter.versions.listReleases()) ?? []
    expect(rows).toHaveLength(1)
    expect(rows[0].published).toBe('')
    expect(rows[0].assets).toEqual(assets)
  })

  it('后端无数据回空表，不造幻影行', async () => {
    svc.ListReleases.mockResolvedValueOnce(null)
    const adapter = createGuoheViewAdapter()
    expect((await adapter.versions.listReleases()) ?? []).toEqual([])
  })
})
