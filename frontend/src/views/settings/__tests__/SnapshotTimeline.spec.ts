// 右栏文件时间线组件级测试（N33 批 B：断言自 SnapshotSection.spec 平移 + 行级 diff 新增）：
// 事件行词表/按钮位次（对比=button[0]、恢复=button[1]，父级恢复链靠此契约）、
// D 行「恢复被删内容」文案、统一式行 diff（删红加绿 token 类）、hunk 折叠可展开、
// 截断态如实标注、D/A 侧说明、config 顶层键变化标注、全等空态。
// 本件不发 RPC（数据全走 props），无需 mock bindings。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SnapshotTimeline from '../SnapshotTimeline.vue'
import type { FileDiff, FileRevision, TrackedFile } from '../../../../bindings/hanxi/internal/snapshot/models'

const file = {
  path: 'memo/memo_9.md', display: '被删的便签', group: 'memo',
  revisions: 2, lastChange: '2026-09-17T14:00:00+08:00', alive: false,
} as TrackedFile

const history = [
  { revisionId: 'bbbb222233334444', time: '2026-09-17T14:00:00+08:00', status: 'D', summary: 'memo/memo_9.md' },
  { revisionId: 'aaaa111122223333', time: '2026-09-16T09:00:00+08:00', status: 'M', summary: 'memo/memo_9.md' },
] as FileRevision[]

interface TimelineProps {
  file?: TrackedFile | null
  history?: FileRevision[]
  loading?: boolean
  diffOpen?: string
  diffData?: FileDiff | null
  diffLoading?: boolean
}

function mountTimeline(props: TimelineProps = {}) {
  return mount(SnapshotTimeline, {
    props: {
      file, history, loading: false,
      diffOpen: '', diffData: null, diffLoading: false,
      ...props,
    },
  })
}

function fd(overrides: Partial<FileDiff>): FileDiff {
  return {
    path: 'memo/memo_9.md', status: 'M', old: '', new: '',
    oldTruncated: false, newTruncated: false, summary: '', ...overrides,
  } as FileDiff
}

describe('SnapshotTimeline 事件行', () => {
  it('未选文件给引导空态', () => {
    const w = mountTimeline({ file: null, history: [] })
    expect(w.text()).toContain('左侧选择一个文件')
  })

  it('事件行渲染状态词表与时间；D 行「恢复被删内容」、M 行「恢复」，按钮位次稳定', () => {
    const w = mountTimeline()
    const rows = w.findAll('.fa-ev-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('删除')
    expect(rows[0].text()).toContain('恢复被删内容')
    expect(rows[1].text()).toContain('修改')
    const btns = rows[1].findAll('button')
    expect(btns[0].text()).toBe('对比')
    expect(btns[1].text()).toBe('恢复')
  })

  it('点击对比/恢复分别抛 toggleDiff/restore（原事件行不自己调 RPC）', async () => {
    const w = mountTimeline()
    const rows = w.findAll('.fa-ev-row')
    await rows[1].findAll('button')[0].trigger('click')
    await rows[0].findAll('button')[1].trigger('click')
    expect(w.emitted('toggleDiff')?.[0][0]).toMatchObject({ revisionId: 'aaaa111122223333' })
    expect(w.emitted('restore')?.[0][0]).toMatchObject({ revisionId: 'bbbb222233334444', status: 'D' })
  })

  it('时间线满 50 条如实挂「仅展示最近 50 版」观察窗徽标', () => {
    const long = Array.from({ length: 50 }, (_, i) => ({
      revisionId: `r${i}`, time: '2026-09-16T09:00:00+08:00', status: 'M', summary: '',
    })) as FileRevision[]
    const w = mountTimeline({ history: long })
    expect(w.text()).toContain('仅展示最近 50 版')
  })
})

describe('SnapshotTimeline 行级 diff 视图', () => {
  it('统一单栏：删行 .dl-del 在前加行 .dl-add 在后，ctx 不着色，统计如实', () => {
    const w = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ old: 'a\nX\nc', new: 'a\nY\nc' }),
    })
    const lines = w.findAll('.dl')
    expect(lines.map((l) => l.classes().join(' '))).toEqual([
      expect.stringContaining('dl-ctx'),
      expect.stringContaining('dl-del'),
      expect.stringContaining('dl-add'),
      expect.stringContaining('dl-ctx'),
    ])
    expect(lines[1].text()).toContain('X')
    expect(lines[2].text()).toContain('Y')
    expect(w.find('.diff-stat-add').text()).toBe('+1')
    expect(w.find('.diff-stat-del').text()).toBe('−1')
    expect(w.find('.fa-diff').exists()).toBe(true)
  })

  it('只渲染 diffOpen 命中的行；未展开时不出 diff 面板', () => {
    const w = mountTimeline({ diffOpen: '', diffData: fd({ old: 'a', new: 'b' }) })
    expect(w.find('.fa-diff').exists()).toBe(false)
  })

  it('hunk 折叠：连续未变段收成「… 省略 K 行 · 展开」，点击后原样回插', async () => {
    const oldLines = Array.from({ length: 15 }, (_, i) => `L${i}`)
    const newLines = oldLines.map((l, i) => (i === 7 ? 'CHANGED' : l))
    const w = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ old: oldLines.join('\n'), new: newLines.join('\n') }),
    })
    const folds = w.findAll('.dl-fold')
    expect(folds).toHaveLength(2) // 前段 7 ctx、后段 7 ctx 各藏 1 行
    expect(folds[0].text()).toContain('省略 1 行')
    await folds[0].trigger('click')
    expect(w.findAll('.dl-fold')).toHaveLength(1)
    expect(w.find('.diff-body').text()).toContain('L3') // 被藏行回插
  })

  it('折叠只藏未变行：展开前后增删行一条不少', async () => {
    const oldLines = Array.from({ length: 40 }, (_, i) => `L${i}`)
    const newLines = [...oldLines]
    newLines[39] = 'TAIL-CHANGED'
    const w = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ old: oldLines.join('\n'), new: newLines.join('\n') }),
    })
    // 中段未变 39 行折为一段；展开后 ctx 行数不变
    expect(w.findAll('.dl-fold')).toHaveLength(1)
    await w.find('.dl-fold').trigger('click')
    expect(w.findAll('.dl-del')).toHaveLength(1)
    expect(w.findAll('.dl-add')).toHaveLength(1)
    expect(w.findAll('.dl-ctx')).toHaveLength(39) // 6 直显 + 33 展开回插
    expect(w.findAll('.dl-fold')).toHaveLength(0)
  })

  it('截断态：给「已截断，仅对比现有部分」提示，不假装完整', () => {
    const w = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ old: 'a\nb-cutt', new: 'a\nb-full', oldTruncated: true }),
    })
    expect(w.text()).toContain('已截断，仅对比现有部分')
  })

  it('D 行 diff：new 空侧给「已删除此文件」说明，全文按删行呈现', () => {
    const w = mountTimeline({
      diffOpen: 'bbbb222233334444',
      diffData: fd({ status: 'D', old: 'keep\nme', new: '' }),
    })
    expect(w.text()).toContain('该版本已删除此文件')
    expect(w.findAll('.dl-del')).toHaveLength(2)
    expect(w.findAll('.dl-add')).toHaveLength(0)
  })

  it('A 行 diff：old 空侧给「新增了此文件」说明', () => {
    const w = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ status: 'A', old: '', new: 'hello' }),
    })
    expect(w.text()).toContain('该版本新增了此文件')
    expect(w.findAll('.dl-add')).toHaveLength(1)
  })

  it('零增删（内容全等）→ 不铺 ctx 行，给「完全一致」空态', () => {
    const w = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ old: 'same\nlines', new: 'same\nlines' }),
    })
    expect(w.text()).toContain('新旧内容完全一致')
    expect(w.findAll('.dl')).toHaveLength(0)
  })

  it('config.json 顶层键变化标注（中文名+嵌套计数），非 config 文件不出', () => {
    const w = mountTimeline({
      diffOpen: 'r1',
      file: { path: 'config.json', display: 'config.json', group: 'config', revisions: 1, lastChange: '', alive: true } as TrackedFile,
      history: [{ revisionId: 'r1', time: '2026-09-16T09:00:00+08:00', status: 'M', summary: '' }] as FileRevision[],
      diffData: fd({
        path: 'config.json',
        old: '{"theme":"light","trayMenu":[{"a":1},{"b":2}],"logRetainDays":7}',
        new: '{"theme":"dark","trayMenu":[{"a":1},{"b":2}],"logRetainDays":7}',
      }),
    })
    expect(w.find('.diff-keys').text()).toContain('外观主题')
    expect(w.find('.diff-keys').text()).not.toContain('日志保留天数')
    // 未动键不列；动了的嵌套键计数化（本例只动 theme）
    // 非 config 路径不出标注行
    const w2 = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ old: '{}', new: '{"a":1}' }),
    })
    expect(w2.find('.diff-keys').exists()).toBe(false)
  })

  it('超 2000 行渲染保护：只出前段并给「展开完整对比」', async () => {
    const oldLines = Array.from({ length: 2100 }, (_, i) => `L${i}`)
    const newLines = [...oldLines]
    newLines[2099] = 'TAIL'
    const w = mountTimeline({
      diffOpen: 'aaaa111122223333',
      diffData: fd({ old: oldLines.join('\n'), new: newLines.join('\n') }),
    })
    const capBtn = w.findAll('.dl-fold').find((b) => b.text().includes('展开完整对比'))
    expect(capBtn).toBeTruthy()
    expect(capBtn!.text()).toContain('101')
    await capBtn!.trigger('click')
    expect(w.findAll('.dl-fold').some((b) => b.text().includes('展开完整对比'))).toBe(false)
  })
})
