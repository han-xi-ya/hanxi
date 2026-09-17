// 组件级测试：HistoryPanel 自取数面板（PLAN_HISTORY §2.3 契约）——
// 锁定 List 挂载自拉、keyword 350ms 防抖 + 过期丢弃、删除/清空走 bindings
// （清空经全局 useConfirm 单例二次确认）、apply 事件携带行内整条记录（Q6 不回查）。
import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import HistoryPanel from '../tool/HistoryPanel.vue'
import { useConfirm } from '../../composables/useConfirm'

const api = vi.hoisted(() => ({
  List: vi.fn(),
  Delete: vi.fn(),
  Clear: vi.fn(),
  GetOcrFullText: vi.fn(),
  SetOcrFullText: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => () => {} },
  Call: { ByID: () => Promise.resolve(undefined) },
}))
vi.mock('../../../bindings/hanxi/internal/history/historyservice', () => api)

const rec = (over: Record<string, unknown> = {}) => ({
  id: 1,
  funcType: 'ocr',
  summary: '识别 a.png → 12 字',
  input: 'C:\\imgs\\a.png',
  output: '你好世界',
  extra: 'ui',
  createdAt: '2026-09-17T10:00:00+08:00',
  ...over,
})

function factory(props: Record<string, unknown> = {}) {
  return mount(HistoryPanel, {
    props: { funcType: 'ocr', ...props },
  })
}

async function settleTimers() {
  await flushPromises()
  await nextTick()
}

beforeEach(() => {
  vi.useFakeTimers()
  api.List.mockResolvedValue([rec(), rec({ id: 2, summary: '识别 b.png', input: 'C:\\imgs\\b.png' })])
  api.Delete.mockResolvedValue(undefined)
  api.Clear.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('HistoryPanel', () => {
  it('挂载即按桶自取数并渲染列表', async () => {
    const w = factory()
    await settleTimers()
    expect(api.List).toHaveBeenCalledWith('ocr', '')
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.text()).toContain('识别 a.png')
    expect(w.text()).toContain('识别 b.png')
  })

  it('keyword 输入 350ms 防抖后才发请求', async () => {
    const w = factory()
    await settleTimers()
    api.List.mockClear()
    await w.find('.hp-search').setValue('8080')
    await settleTimers()
    expect(api.List).not.toHaveBeenCalled() // 未到防抖窗口
    vi.advanceTimersByTime(400)
    await settleTimers()
    expect(api.List).toHaveBeenCalledTimes(1)
    expect(api.List).toHaveBeenCalledWith('ocr', '8080')
  })

  it('快速连发只认最后一次结果（seq 过期丢弃）', async () => {
    const w = factory()
    await settleTimers()
    let resolveStale: (v: unknown[]) => void = () => {}
    api.List.mockImplementationOnce(
      () => new Promise((r) => { resolveStale = r }),
    )
    await w.find('.hp-search').setValue('k')
    vi.advanceTimersByTime(400) // 发出 stale 请求（尚未 resolve）
    await settleTimers()
    api.List.mockResolvedValueOnce([rec({ id: 9, summary: '新鲜结果' })])
    await w.find('.hp-search').setValue('k2')
    vi.advanceTimersByTime(400)
    await settleTimers()
    resolveStale([rec({ id: 1, summary: '过期结果不应出现' })]) // 旧请求迟到
    await settleTimers()
    expect(w.text()).toContain('新鲜结果')
    expect(w.text()).not.toContain('过期结果不应出现')
  })

  it('双击行上抛整条记录（Q6 行内数据直用）', async () => {
    const w = factory()
    await settleTimers()
    await w.findAll('tbody tr')[1].trigger('dblclick')
    const evt = w.emitted('apply')
    expect(evt).toBeTruthy()
    expect((evt![0][0] as { id: number }).id).toBe(2)
  })

  it('showApply=false 时不渲染应用钮也不响应双击', async () => {
    const w = factory({ showApply: false })
    await settleTimers()
    await w.findAll('tbody tr')[0].trigger('dblclick')
    expect(w.emitted('apply')).toBeFalsy()
    const detailBtns = w.findAll('.hp-actions .btn').map((b) => b.text())
    expect(detailBtns).not.toContain('应用')
  })

  it('删除本条走 Delete 后重拉列表', async () => {
    const w = factory()
    await settleTimers()
    api.List.mockResolvedValue([rec({ id: 2 })])
    await w.findAll('tbody tr')[0].trigger('click') // 选中第一行 id=1
    await w.find('.hp-detail .btn-danger-outline').trigger('click')
    await vi.runOnlyPendingTimersAsync()
    await settleTimers()
    expect(api.Delete).toHaveBeenCalledWith(1)
    expect(api.List).toHaveBeenCalledTimes(2) // 初次 + 删除后 reload
  })

  it('清空仅当前桶：二次确认后 Clear(funcType)，取消则不发', async () => {
    const { settleConfirm } = useConfirm()
    const w = factory()
    await settleTimers()
    await w.find('.hp-toolbar .btn-danger-outline').trigger('click')
    await nextTick()
    // 取消路径
    settleConfirm(false)
    await settleTimers()
    expect(api.Clear).not.toHaveBeenCalled()
    // 确认路径
    await w.find('.hp-toolbar .btn-danger-outline').trigger('click')
    await nextTick()
    settleConfirm(true)
    await vi.runOnlyPendingTimersAsync()
    await settleTimers()
    expect(api.Clear).toHaveBeenCalledWith('ocr')
  })

  it('List 报错时展示降级提示且不炸渲染', async () => {
    api.List.mockRejectedValueOnce('corrupt JSON')
    const w = factory()
    await settleTimers()
    expect(w.find('.error-box').exists()).toBe(true)
    expect(w.text()).toContain('读取历史记录失败')
  })
})
