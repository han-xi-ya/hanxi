// 左栏受保文件清单组件级测试（N33 批 B 拆分自 SnapshotSection 断言平移 + 中文名表新增；
// 批 C 补恢复生效常驻徽标、"最近 N 次变化"整句与窄屏 chip 条结构断言）：
// 分组词表、中文名映射优先级（前端表 > 后端 Display > 文件名回落）、
// 已删除徽标、选中态与 select 事件。纯呈现件，无 bindings 依赖。
// 窄屏限制说明：jsdom 不评估媒体查询，≤640px chip 条只能以 DOM 结构近似断言
// （组标签与行按钮平铺同层 = 纯 CSS 可折横排的前提），像素级溢出归批 D 真机清单。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SnapshotFileList from '../SnapshotFileList.vue'
import type { TrackedFile } from '../../../../bindings/hanxi/internal/snapshot/models'

function tf(path: string, display: string, group: string, alive = true, revisions = 1): TrackedFile {
  return { path, display, group, revisions, lastChange: '2026-09-25T10:00:00+08:00', alive } as TrackedFile
}

const files = [
  tf('memo/memo_9.md', '被删的便签', 'memo', false, 2),
  tf('config.json', 'config.json', 'config'),
  tf('state/projects.json', 'projects.json', 'state'),
  tf('state/live.json', 'live.json', 'state'),
]

describe('SnapshotFileList', () => {
  it('三组分组呈现，组序信任后端；中文名映射覆盖后端回落名', () => {
    const w = mount(SnapshotFileList, { props: { files, selectedPath: '' } })
    const groups = w.findAll('.fa-group').map((g) => g.text())
    expect(groups).toEqual(['便签', '工作台设置', '模块状态'])
    const rows = w.findAll('.fa-file')
    expect(rows).toHaveLength(4)
    // memo 走后端标题 resolver（前端表不接管）
    expect(rows[0].text()).toContain('被删的便签')
    // config.json 命中前端映射表，盖掉后端文件名回落
    expect(rows[1].text()).toContain('工作台设置')
    // state 例外表：projects.json → frpc（§2 例外，落盘名不是模块 ID）
    expect(rows[2].text()).toContain('frpc 项目配置')
    // 映射不到的 state 文件原样回落文件名（诚实边界，不瞎猜）
    expect(rows[3].text()).toContain('live.json')
    expect(rows[3].text()).not.toContain('未知模块')
  })

  it('已删除徽标只给 alive=false 行；计数给"最近 N 次变化"窗内口径整句', () => {
    const w = mount(SnapshotFileList, { props: { files, selectedPath: '' } })
    const rows = w.findAll('.fa-file')
    expect(rows[0].find('.fa-dead').exists()).toBe(true)
    expect(rows[1].find('.fa-dead').exists()).toBe(false)
    expect(rows[0].find('.fa-count').text()).toBe('最近 2 次变化')
    // 零版本文件不空着：如实给窗内无变化（不谎称"无历史"）
    expect(rows[3].find('.fa-count').text()).toBe('最近 1 次变化')
    const w0 = mount(SnapshotFileList, { props: { files: [tf('state/x.json', 'x.json', 'state', true, 0)], selectedPath: '' } })
    expect(w0.find('.fa-count').text()).toBe('窗内暂无变化')
    // 口径解释进 tooltip（改名文件与时间线沿链计数可差 1，行内只说窗内事实）
    expect(rows[0].find('.fa-count').attributes('title')).toContain('50 版观察窗')
  })

  it('恢复生效徽标常驻行上：memo 行即时生效，config/state 行重启生效', () => {
    const w = mount(SnapshotFileList, { props: { files, selectedPath: '' } })
    const rows = w.findAll('.fa-file')
    expect(rows[0].find('.fa-scope').text()).toBe('即时生效')
    expect(rows[0].find('.fa-scope').classes()).toContain('chip-positive')
    expect(rows[1].find('.fa-scope').text()).toBe('重启生效')
    expect(rows[1].find('.fa-scope').classes()).toContain('chip-warning')
    expect(rows[2].find('.fa-scope').text()).toBe('重启生效')
  })

  it('窄屏 chip 条前提的结构断言：组标签与行按钮平铺于 .fa-list 同层（无包裹盒）', () => {
    // jsdom 不跑媒体查询：≤640px 横向 chip 条是纯 CSS 重排，DOM 结构只需保证
    // 「可重排」——所有 .fa-file 直接挂 .fa-list，不困在分组列盒里。
    const w = mount(SnapshotFileList, { props: { files, selectedPath: '' } })
    const list = w.find('.fa-list')
    for (const row of w.findAll('.fa-file')) {
      expect(row.element.parentElement).toBe(list.element)
    }
    expect(list.findAll('.fa-group')).toHaveLength(3)
  })

  it('点选行抛 select(path)，selectedPath 决定 active', async () => {
    const w = mount(SnapshotFileList, { props: { files, selectedPath: 'config.json' } })
    const rows = w.findAll('.fa-file')
    expect(rows[1].classes()).toContain('active')
    expect(rows[0].classes()).not.toContain('active')
    await rows[0].trigger('click')
    expect(w.emitted('select')?.[0]).toEqual(['memo/memo_9.md'])
  })

  it('悬停行 title 保留原始路径（机器值可查）', () => {
    const w = mount(SnapshotFileList, { props: { files, selectedPath: '' } })
    expect(w.findAll('.fa-name')[1].attributes('title')).toBe('config.json')
  })
})
