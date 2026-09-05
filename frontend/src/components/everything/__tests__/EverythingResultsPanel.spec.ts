// EverythingResultsPanel 聚焦测试：锁拆分后随 useEverythingColumns 内聚的列宽能力——
// localStorage 记忆恢复、拖拽钳位（60~900）、松手防抖 300ms 落盘、卸载中止半途拖拽不泄漏；
// 以及结果行 title 全路径拼接与打开/定位/复制事件载荷、三种空态提示。
// 本组件不直调 bindings，但模板经 useEverythingSearch 引入 resultFullPath（纯函数），
// 其模块顶层 import 会带出真实 @wailsio/runtime（Node 下无桥，导入副作用会尝试连 :3000 dev 桥）——
// 按仓库测试 seam 约定（docs/FRONTEND.md §8）对 runtime/bindings 打桩。
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import EverythingResultsPanel from '../EverythingResultsPanel.vue'

vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn(() => vi.fn()) },
}))
vi.mock('../../../../bindings/hanxi/internal/modules/everything/everythingservice', () => ({}))


const STORAGE_KEY = 'hanxi-everything-result-cols'

const rows = [
  { name: 'go.mod', path: 'D:\\proj', size: 1024, modified: '2026-09-01 10:00', isDir: false },
  { name: 'src', path: 'D:\\proj\\', size: 0, modified: '2026-09-01 09:00', isDir: true },
]

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(EverythingResultsPanel, {
    props: { results: [], searched: '', truncated: false, searching: false, ...props },
  })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('EverythingResultsPanel 列宽记忆与拖拽', () => {
  it('无记忆值：表头按默认宽渲染，表格 minWidth=各列之和（1058px）', () => {
    const w = mountPanel({ results: rows })
    const ths = w.findAll('thead th')
    expect(ths.map((t) => t.attributes('style'))).toEqual([
      'width: 320px;', 'width: 420px;', 'width: 70px;', 'width: 130px;', 'width: 118px;',
    ])
    expect(w.find('.result-tbl').attributes('style')).toContain('min-width: 1058px;')
    w.unmount()
  })

  it('localStorage 记忆恢复；首行单元格同步绑定列宽（fixed 布局对齐约定）', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ name: 500, bogus: 99 }))
    const w = mountPanel({ results: rows })
    await w.vm.$nextTick() // 装载读取发生在首帧渲染之后（与原视图 onMounted 同款时序）
    expect(w.findAll('thead th')[0].attributes('style')).toBe('width: 500px;')
    expect(w.findAll('tbody tr')[0].findAll('td')[0].attributes('style')).toBe('width: 500px;')
    // 非首行不绑定宽度
    expect(w.findAll('tbody tr')[1].findAll('td')[0].attributes('style')).toBeUndefined()
    w.unmount()
  })

  it('拖拽 name 列：move 实时更新并钳位 60~900；松手防抖 300ms 落盘', async () => {
    vi.useFakeTimers()
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ name: 500 }))
    const w = mountPanel({ results: rows })
    await w.findAll('.col-resizer')[0].trigger('mousedown', { clientX: 100 })
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 200 }))
    await w.vm.$nextTick()
    expect(w.findAll('thead th')[0].attributes('style')).toBe('width: 600px;')
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: -900 }))
    await w.vm.$nextTick()
    expect(w.findAll('thead th')[0].attributes('style')).toBe('width: 60px;')
    document.dispatchEvent(new MouseEvent('mouseup'))
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY)!).name).toBe(500) // 防抖未到期不落盘
    await vi.advanceTimersByTimeAsync(300)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY)!).name).toBe(60)
    w.unmount()
  })

  it('拖拽中途卸载：半途拖拽被收尾落盘（原 onUnmounted 即 dragCleanup 语义），且 document 监听清理不重触发', async () => {
    vi.useFakeTimers()
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ name: 500 }))
    const w = mountPanel({ results: rows })
    await w.findAll('.col-resizer')[0].trigger('mousedown', { clientX: 100 })
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 200 }))
    w.unmount() // 清理路径本身会防抖落盘一次
    await vi.advanceTimersByTimeAsync(300)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY)!).name).toBe(600)
    localStorage.clear()
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 900 }))
    document.dispatchEvent(new MouseEvent('mouseup'))
    await vi.advanceTimersByTimeAsync(600)
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull() // 监听已摘除：move/up 均无副作用
  })
})

describe('EverythingResultsPanel 结果行与空态', () => {
  it('行 title 拼完整路径（目录尾杠不重复）；打开/定位/复制上抛行数据', async () => {
    const w = mountPanel({ results: rows })
    const [tr0, tr1] = w.findAll('tbody tr')
    expect(tr0.find('.result-name').attributes('title')).toBe('点击复制：D:\\proj\\go.mod')
    expect(tr1.find('.result-name').attributes('title')).toBe('点击复制：D:\\proj\\src')
    await tr0.findAll('.row-actions .link-button')[0].trigger('click')
    await tr0.findAll('.row-actions .link-button')[1].trigger('click')
    await tr0.find('.result-path').trigger('click')
    expect(w.emitted('open')![0]).toEqual([rows[0]])
    expect(w.emitted('reveal')![0]).toEqual([rows[0]])
    expect(w.emitted('copy')![0]).toEqual([rows[0]])
    w.unmount()
  })

  it('计数行：截断警示仅在 truncated 时出现', () => {
    let w = mountPanel({ results: rows, searched: 'go', truncated: true })
    expect(w.find('.results-meta').text()).toContain('共 2 条结果')
    expect(w.find('.warn-text').text()).toContain('300 条上限')
    w.unmount()
    w = mountPanel({ results: rows, searched: 'go', truncated: false })
    expect(w.find('.warn-text').exists()).toBe(false)
    w.unmount()
  })

  it('三种空态互斥：搜索中 / 无匹配 / 空闲引导', () => {
    let w = mountPanel({ searching: true })
    expect(w.find('.empty-hint').text()).toContain('搜索中…')
    w.unmount()
    w = mountPanel({ searched: 'zzz' })
    expect(w.find('.empty-hint').text()).toBe('「zzz」无匹配结果')
    w.unmount()
    w = mountPanel()
    expect(w.find('.empty-hint').classes()).toContain('idle-hint')
    expect(w.find('.empty-hint').text()).toContain('输入即搜')
    w.unmount()
  })
})
