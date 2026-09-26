// EverythingReleaseTable 聚焦测试：远程槽位行的通道徽标/快照徽标与 N13 形态 chip
// （后端 ListRemote 回填 form，画法与 SnipasteReleaseTable 逐字对齐、取词走
// releaseFormWord 单源）；下载编排与事件回抛在视图层，不在本组件。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import EverythingReleaseTable from '../EverythingReleaseTable.vue'

const stableRow = {
  version: '1.4.1.1032',
  channel: 'stable',
  published: '2026-01-23',
  assetUrl: 'https://www.voidtools.com/Everything-1.4.1.1032.x64.zip',
  sha256: '698df475ec44e638f66f1b6a32d28fea613cec78d3b6310e6abe53431eeb940c',
  size: 1906504,
  stale: false,
}

const betaRow = {
  ...stableRow,
  version: '1.5.0.1422b',
  channel: 'beta',
  published: '2026-08-13',
}

function mountTable(props: Record<string, unknown> = {}) {
  return mount(EverythingReleaseTable, {
    props: {
      releases: [stableRow, betaRow],
      installed: [],
      downloading: {},
      loading: false,
      ...props,
    },
  })
}

describe('EverythingReleaseTable', () => {
  it('N13 形态 chip：form=portable 出「便携」词值（取词与共享面板同源），未回填行 chip 缺席', () => {
    const w = mountTable({
      releases: [{ ...stableRow, form: 'portable' }, stableRow],
    })
    const rows = w.findAll('.tbl tbody tr')
    expect(rows[0].find('.form-chip').text()).toBe('便携')
    expect(rows[0].find('.form-chip').attributes('title')).toContain('本托管形态（安装链事实）')
    expect(rows[1].find('.form-chip').exists()).toBe(false)
  })

  it('词表外形态值如实透传（后端扩词不静默吞标）', () => {
    const w = mountTable({ releases: [{ ...stableRow, form: 'appimage' }] })
    expect(w.find('.form-chip').text()).toBe('appimage')
  })

  it('通道徽标 stable/beta 分档 + 快照徽标与形态 chip 可共存', () => {
    const w = mountTable({
      releases: [{ ...betaRow, stale: true, form: 'portable' }],
    })
    const row = w.find('.tbl tbody tr')
    expect(row.find('.channel-badge').classes()).toContain('ch-beta')
    expect(row.find('.badge-pre').text()).toBe('快照')
    expect(row.find('.form-chip').text()).toBe('便携')
  })

  it('已安装行不出下载钮；idle 行点「下载安装」上抛整行载荷', async () => {
    const w = mountTable({ installed: [{ version: '1.4.1.1032' }] })
    const rows = w.findAll('.tbl tbody tr')
    expect(rows[0].find('.ver-status').text()).toBe('已安装')
    expect(rows[0].find('button.btn-primary').exists()).toBe(false)
    await rows[1].find('button.btn-primary').trigger('click')
    expect(w.emitted('download')).toHaveLength(1)
    expect(w.emitted('download')![0]![0]).toMatchObject({ version: '1.5.0.1422b' })
  })
})
