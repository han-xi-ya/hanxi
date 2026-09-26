// ============================================================================
// BCU adapter · N13 形态矩阵透传契约
//
// BCU 远程行经 variantRelease() 注入下载变体后才进 store.runDownload，
// 该行改写是唯一可能吞掉 Assets 矩阵的投影点——版本 Tab 方言表的
// 「上游发布」标注全赖此处逐字段存活（双变体行共用同一矩阵，
// Managed 高亮位跟随后端选中的便携资产，不随 variant 漂移）。
// ============================================================================

import { describe, expect, it, vi } from 'vitest'
import { variantRelease, type BCUReleaseRow } from '../bcu'
import type { ReleaseAssetNote } from '../../components/managed/adapter'

// bindings 服务面与本契约无关，但模块顶层 import 会真实装载运行时——照例 mock 短路
vi.mock('../../../bindings/hanxi/internal/modules/bcu/bcuservice', () => ({ DownloadVersion: vi.fn() }))
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ({ confirm: vi.fn() }) }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ({ prompt: vi.fn() }) }))

describe('variantRelease · N13 assets 透传', () => {
  it('注入 variant 的同时保留形态矩阵原样（Managed 高亮位不漂移）', () => {
    const assets: ReleaseAssetNote[] = [
      { platform: 'windows', form: 'portable', label: 'BCUninstaller_6.3.0_portable.7z' },
      { platform: 'windows', form: 'installer', label: 'BCUninstaller_6.3.0_setup.exe' },
      { platform: 'windows', form: 'archive', label: 'Localisation_Pack_6.3.0.zip' },
      { platform: 'windows', form: 'portable', label: 'BCUninstaller_6.3.0_net8.0-windows10.0.18362.0.zip', managed: true },
    ]
    const rel = {
      version: '6.3.0',
      tag: 'v6.3',
      published: '2026-09-01T00:00:00Z',
      isPre: false,
      assetName: 'BCUninstaller_6.3.0_net8.0-windows10.0.18362.0.zip',
      assetUrl: 'https://github.com/BCUninstaller/Bulk-Crap-Uninstaller/releases/download/v6.3/x.zip',
      size: 12_000_000,
      sha256: 'ab12',
      assets,
      fddName: 'BCUninstaller_6.3.0_net8.0-windows10.0.18362.0.zip',
    } as never

    for (const variant of ['portable', 'fdd'] as const) {
      const row = variantRelease(rel, variant) as BCUReleaseRow & { assets?: ReleaseAssetNote[] }
      expect(row.variant).toBe(variant)
      expect(row.assets).toEqual(assets)
      expect(row.assets?.filter((a) => a.managed)).toHaveLength(1)
    }
  })
})
