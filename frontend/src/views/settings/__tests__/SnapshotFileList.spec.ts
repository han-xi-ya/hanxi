// 左栏受保文件清单组件级测试（N33 批 B 拆分自 SnapshotSection 断言平移 + 中文名表新增）：
// 分组词表、中文名映射优先级（前端表 > 后端 Display > 文件名回落）、
// 已删除徽标、选中态与 select 事件。纯呈现件，无 bindings 依赖。
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

  it('已删除徽标只给 alive=false 行；版本号原样带出', () => {
    const w = mount(SnapshotFileList, { props: { files, selectedPath: '' } })
    const rows = w.findAll('.fa-file')
    expect(rows[0].find('.fa-dead').exists()).toBe(true)
    expect(rows[1].find('.fa-dead').exists()).toBe(false)
    expect(rows[0].find('.fa-count').text()).toBe('2')
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
