// 组 G1 特征测试：MemoView——锁迁移前基线（第三方言视图，交互结果导向断言）。
// 现状钉死：删除无二次确认（直接 Delete）、搜索 @input 即时查询、标签自动加 # 前缀。
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MemoView from '../MemoView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  List: vi.fn(),
  GetStats: vi.fn(),
  Create: vi.fn(),
  Update: vi.fn(),
  Delete: vi.fn(),
  TogglePin: vi.fn(),
  ToggleMask: vi.fn(),
}))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data?: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data?: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

vi.mock('../../../bindings/hanxi/internal/modules/memo', () => ({
  MemoService: svc,
}))

const memo = (over: Record<string, unknown> = {}) => ({
  id: 'm1',
  title: '生产库连接串',
  content: 'host=10.0.0.1;pw=topsecret',
  tags: ['#SQL'],
  colorTag: 'blue',
  isPinned: false,
  isMasked: false,
  updatedAt: '2026-09-01T10:00:00Z',
  ...over,
})

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function defaults(items: unknown[] = [memo()], tagCloud: Record<string, number> = { '#SQL': 1 }) {
  svc.List.mockResolvedValue(items)
  svc.GetStats.mockResolvedValue({ totalCount: items.length, pinnedCount: 0, tagCloud })
}

async function mountView(items?: unknown[], tagCloud?: Record<string, number>) {
  defaults(items, tagCloud)
  const w = mount(defineComponent({ render: () => h(MemoView) }), { attachTo: document.body })
  await flushMicrotasks()
  return w
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  useToast().clearToast()
})

describe('MemoView 列表与过滤', () => {
  it('挂载拉取列表与统计，渲染便签卡与标签云计数', async () => {
    const w = await mountView()
    expect(svc.List).toHaveBeenCalledTimes(1)
    expect(svc.GetStats).toHaveBeenCalled()
    expect(svc.List.mock.calls[0][0]).toMatchObject({ keyword: '', tag: '', pinned: null, sortBy: 'updated', sortDesc: true })
    expect(w.text()).toContain('生产库连接串')
    expect(w.find('.tag-pill').text()).toBe('#SQL')
    const chips = w.findAll('.tag-chip').map((c) => c.text())
    expect(chips).toContain('全部 (1)')
    expect(chips.find((t) => t.includes('#SQL'))).toContain('(1)')
    w.unmount()
  })

  it('空状态与加载中文案（现状类名语义断言，不依赖具体 class）', async () => {
    const w = await mountView([], {})
    expect(w.text()).toContain('暂无符合条件的备忘录便签')
    w.unmount()
  })

  it('搜索框输入即时重查（keyword 入参）', async () => {
    const w = await mountView()
    const before = svc.List.mock.calls.length
    await w.find('.search-input').setValue('redis')
    await flushMicrotasks()
    expect(svc.List.mock.calls.length).toBeGreaterThan(before)
    expect(svc.List.mock.calls[svc.List.mock.calls.length - 1][0]).toMatchObject({ keyword: 'redis' })
    w.unmount()
  })

  it('点标签过滤，再点同标签取消回全部', async () => {
    const w = await mountView([memo()], { '#SQL': 3 })
    const sqlChip = w.findAll('.tag-chip').find((c) => c.text().includes('#SQL'))!
    await sqlChip.trigger('click')
    await flushMicrotasks()
    expect(svc.List.mock.calls.at(-1)![0]).toMatchObject({ tag: '#SQL' })
    await w.findAll('.tag-chip').find((c) => c.text().includes('#SQL'))!.trigger('click')
    await flushMicrotasks()
    expect(svc.List.mock.calls.at(-1)![0]).toMatchObject({ tag: '' })
    w.unmount()
  })

  it('置顶切换按后端回执播报并刷新', async () => {
    const w = await mountView([memo({ isPinned: false })])
    svc.TogglePin.mockResolvedValue(true)
    const before = svc.List.mock.calls.length
    await w.find('.memo-card .memo-actions button:nth-child(2)').trigger('click')
    await flushMicrotasks()
    expect(svc.TogglePin).toHaveBeenCalledWith('m1')
    expect(useToast().toastMsg.value).toBe('已置顶便签')
    expect(svc.List.mock.calls.length).toBe(before + 1)
    w.unmount()
  })

  it('脱敏遮罩：masked 卡片渲染圆点且可揭示', async () => {
    const w = await mountView([memo({ isMasked: true })])
    expect(w.find('.memo-content-box').classes()).toContain('masked')
    expect(w.find('.memo-content-box').text()).toContain('•••')
    svc.ToggleMask.mockResolvedValue(false)
    await w.find('.memo-card .memo-actions button').trigger('click') // 第一枚即遮罩钮
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已揭示明文')
    w.unmount()
  })

  it('删除无二次确认直发后端（现状钉死，是否补确认属产品决策）', async () => {
    const w = await mountView()
    const spy = vi.fn(() => true)
    vi.stubGlobal('confirm', spy) // happy-dom 下 window.confirm 不存在，用桩捕捉任何潜在调用
    svc.Delete.mockResolvedValue(undefined)
    await w.find('.memo-actions .text-danger').trigger('click')
    await flushMicrotasks()
    expect(spy).not.toHaveBeenCalled() // 原生确认都没有——直接删
    expect(svc.Delete).toHaveBeenCalledWith('m1')
    expect(useToast().toastMsg.value).toBe('已删除便签')
    w.unmount()
  })

  it('memo:changed 事件触发重查；卸载注销订阅', async () => {
    const w = await mountView()
    expect(runtime.handlers['memo:changed']).toBeDefined()
    const before = svc.List.mock.calls.length
    runtime.handlers['memo:changed']({})
    await flushMicrotasks()
    expect(svc.List.mock.calls.length).toBeGreaterThan(before)
    w.unmount()
    await nextTick()
    expect(runtime.unlisten).toHaveBeenCalledTimes(1)
  })
})

describe('MemoView 编辑器', () => {
  it('新建流：校验空表单→填题→标签回车规范化→创建成功', async () => {
    const w = await mountView([])
    await w.findAll('button').find((b) => b.text().includes('新建备忘便签'))!.trigger('click')
    expect(w.find('.modal-dialog').exists()).toBe(true)
    // 空表单保存被拦
    await w.find('.modal-footer .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('标题或内容至少填写一项')
    // 填标题
    await w.find('.modal-body .input-control').setValue('JWT 样本')
    // 标签输入回车：自动补 #，重复不叠加
    const tagInput = w.findAll('.tags-input-box .input-control')[0]
    await tagInput.setValue('SQL')
    await tagInput.trigger('keydown.enter')
    await tagInput.setValue('#SQL')
    await tagInput.trigger('keydown.enter')
    expect(w.findAll('.tag-removable')).toHaveLength(1)
    expect(w.find('.tag-removable').text()).toContain('#SQL')
    svc.Create.mockResolvedValue(undefined)
    await w.find('.modal-footer .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenCalledWith('JWT 样本', '', ['#SQL'], 'blue')
    expect(useToast().toastMsg.value).toBe('已新建备忘录')
    expect(w.find('.modal-dialog').exists()).toBe(false)
    w.unmount()
  })

  it('编辑流：回填既有数据并以 Update 保存', async () => {
    const w = await mountView([memo({ tags: ['#SQL', '#prod'], colorTag: 'amber' })])
    await w.find('.memo-actions button:nth-child(3)').trigger('click') // ✏️ 编辑
    const inputs = w.findAll('.modal-body .input-control')
    expect((inputs[0].element as HTMLInputElement).value).toBe('生产库连接串')
    expect((inputs[1].element as HTMLTextAreaElement).value).toContain('topsecret')
    // 选中态色彩圈
    expect(w.findAll('.color-circle')[2].classes()).toContain('selected')
    svc.Update.mockResolvedValue(undefined)
    await w.find('.modal-footer .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Update).toHaveBeenCalledWith('m1', '生产库连接串', 'host=10.0.0.1;pw=topsecret', ['#SQL', '#prod'], 'amber')
    expect(useToast().toastMsg.value).toBe('备忘录已更新')
    w.unmount()
  })

  it('新建时预选标签带入当前过滤标签', async () => {
    const w = await mountView([memo()], { '#SQL': 1 })
    await w.findAll('.tag-chip').find((c) => c.text().includes('#SQL'))!.trigger('click')
    await flushMicrotasks()
    await w.findAll('button').find((b) => b.text().includes('新建备忘便签'))!.trigger('click')
    expect(w.find('.tag-removable').text()).toContain('#SQL')
    w.unmount()
  })

  it('复制便签内容 toast 契约', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = await mountView()
    await w.find('.btn-copy-chip').trigger('click')
    await flushMicrotasks()
    expect(writeText).toHaveBeenCalledWith('host=10.0.0.1;pw=topsecret')
    expect(useToast().toastMsg.value).toBe('已复制便签内容到剪贴板')
    w.unmount()
  })
})
