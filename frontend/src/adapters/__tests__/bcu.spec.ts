// ============================================================================
// BCU adapter · N13 形态矩阵透传契约
//
// BCU 远程行经 variantRelease() 注入下载变体后才进 store.runDownload，
// 该行改写是唯一可能吞掉 Assets 矩阵的投影点——版本 Tab 方言表的
// 「上游发布」标注全赖此处逐字段存活（双变体行共用同一矩阵，
// Managed 高亮位跟随后端选中的便携资产，不随 variant 漂移）。
// ============================================================================

import { describe, expect, it, vi } from 'vitest'
import { createBCUAdapter, variantRelease, type BCUReleaseRow } from '../bcu'
import type { ReleaseAssetNote } from '../../components/managed/adapter'

// bindings 服务面与本契约无关，但模块顶层 import 会真实装载运行时——照例 mock 短路
vi.mock('../../../bindings/hanxi/internal/modules/bcu/bcuservice', () => ({ DownloadVersion: vi.fn() }))
// 捕获型订阅假件：票据键对齐用例要按真件语义灌事件帧
const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
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

// 铁律②复合键对齐不变式：事件回流票据键（subscribeProgress 造）与面板行反查键
// （versions.progressKey 造）必须逐字同形——一处漂移即「行不亮/票据永挂」的
// 真实故障形（同版本双变体互相覆盖史）。断言跨两条独立代码路径的一致性，
// 不是抄任一实现的字符串镜像。
describe('createBCUAdapter · 票据键与行反查键同源对齐', () => {
  const rel = {
    version: '6.3.0',
    tag: 'v6.3',
    published: '2026-09-01T00:00:00Z',
    isPre: false,
    assetName: 'BCUninstaller_6.3.0_portable.zip',
    assetUrl: 'https://github.com/BCUninstaller/Bulk-Crap-Uninstaller/releases/download/v6.3/x.zip',
    size: 12_000_000,
    sha256: 'ab12',
    fddName: 'BCUninstaller_6.3.0_net8.0-windows10.0.18362.0.zip',
  } as never

  function feedProgress(frame: Record<string, unknown>) {
    const adapter = createBCUAdapter()
    const seen: Array<{ key: string }> = []
    adapter.subscribeProgress((p) => seen.push(p))
    const entry = events[events.length - 1]
    expect(entry.name).toBe('bcu:version-download')
    entry.handler(frame)
    return { adapter, key: seen[0]?.key }
  }

  it('variant 在场：事件键与 variantRelease 行的 progressKey 逐字相等', () => {
    const { adapter, key } = feedProgress({ version: '6.3.0', variant: 'fdd', stage: 'downloading', done: 1, total: 2, message: '' })
    expect(key).toBe('6.3.0|fdd')
    expect(adapter.versions.progressKey?.(variantRelease(rel, 'fdd') as never)).toBe(key)
  })

  it('variant 缺席帧回落 portable 词，与无注入行（首用一键下载）的反查回落同形', () => {
    const { adapter, key } = feedProgress({ version: '6.3.0', stage: 'done', done: 2, total: 2, message: '' })
    expect(key).toBe('6.3.0|portable')
    // 环境未测毕 recommendedVariant=null → 反查同样回落 'portable'（两侧同缺省口径）
    expect(adapter.recommendedVariant.value).toBe(null)
    // rel 本体无 variant 注入位（首用一键下载行形）——反查回落 portable
    expect(adapter.versions.progressKey?.(rel as never)).toBe('6.3.0|portable')
  })

  it('variant 空串帧按 portable 归一（后端空字段惯例，不得生成「6.3.0|」断尾键）', () => {
    const { key } = feedProgress({ version: '6.3.0', variant: '', stage: 'downloading', done: 1, total: 2, message: '' })
    expect(key).toBe('6.3.0|portable')
  })

  it('缺 version 帧整帧丢弃：不产票据（幽灵票据=面板永挂的另一半防线）', () => {
    const adapter = createBCUAdapter()
    const seen: unknown[] = []
    adapter.subscribeProgress((p) => seen.push(p))
    events[events.length - 1].handler({ version: '', variant: 'fdd', stage: 'done', done: 1, total: 1, message: '' })
    events[events.length - 1].handler(null)
    expect(seen).toHaveLength(0)
  })
})
