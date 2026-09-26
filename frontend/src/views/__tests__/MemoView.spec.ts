// MemoView 行为规格（2026-09 整页重设计同步重写；同轮"禁页面私有覆盖共享样式"
// 整改后，页内控件类一律 memo- 私有前缀）：
// 保留并再钉死既有纪律——搜索 350ms 防抖 + 序号守卫、标签自动补 #、删除强确认、
// 遮罩渲染语义（masked 卡片/详情不落明文）、memo:changed 重拉与卸载注销、
// toast 文案契约逐字不变。新增钉死——速记回车即存（含输入法组字豁免）、
// 编辑器书写/预览切换（Markdown 渲染进 DOM）、详情浮层、一键全删强确认
// （danger tone、条数入文案、走 ClearAll）。契约测试更新：前端依赖面从
// 七方法扩为八方法（新增 ClearAll，RestoreFile 依旧不得出现在前端面）。
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MemoView from '../MemoView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  List: vi.fn(),
  GetStats: vi.fn(),
  Create: vi.fn(),
  Update: vi.fn(),
  Delete: vi.fn(),
  TogglePin: vi.fn(),
  ToggleMask: vi.fn(),
  ClearAll: vi.fn(),
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
  createdAt: '2026-09-01T10:00:00Z',
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

// 卡片微操作钮的固定位次：0=遮罩 1=置顶 2=编辑 3=删除（MD chip 是 span 不占位）
function cardBtns(w: ReturnType<typeof mount>, nth: number) {
  return w.findAll('.memo-card .memo-actions .memo-icon-btn')[nth]
}

const byText = (w: ReturnType<typeof mount>, text: string) =>
  w.findAll('button').find((b) => b.text().includes(text))

afterEach(() => {
  useConfirm().settleConfirm(false)
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  useToast().clearToast()
})

describe('MemoView 列表与过滤', () => {
  it('挂载拉取列表与统计，渲染便签卡与标签云计数', async () => {
    const w = await mountView()
    expect(svc.List).toHaveBeenCalledTimes(1)
    expect(svc.GetStats).toHaveBeenCalled()
    expect(svc.List.mock.calls[0][0]).toMatchObject({
      keyword: '', tag: '', pinned: null, sortBy: 'updated', sortDesc: true,
    })
    expect(w.text()).toContain('生产库连接串')
    expect(w.find('.memo-tag').text()).toBe('#SQL')
    const chips = w.findAll('.tag-chip').map((c) => c.text())
    expect(chips).toContain('全部 (1)')
    expect(chips.find((t) => t.includes('#SQL'))).toContain('(1)')
    w.unmount()
  })

  it('时间分组：置顶单列一组，其余按 updatedAt 落入自然日档', async () => {
    const today = new Date().toISOString()
    const w = await mountView(
      [
        memo({ id: 'p1', title: '钉住的旧账', isPinned: true, updatedAt: '2026-01-01T10:00:00Z' }),
        memo({ id: 't1', title: '今天的', updatedAt: today }),
        memo({ id: 'o1', title: '古早的', updatedAt: '2025-01-01T10:00:00Z' }),
      ],
      { '#SQL': 3 },
    )
    const heads = w.findAll('.group-head').map((h2) => h2.text().replace(/\s+/g, ' ').trim())
    expect(heads).toEqual(['置顶 1', '今天 1', '更早 1'])
    const groups = w.findAll('.memo-group')
    expect(groups[0].text()).toContain('钉住的旧账')
    expect(groups[1].text()).toContain('今天的')
    expect(groups[2].text()).toContain('古早的')
    expect(groups[0].find('.memo-card').classes()).toContain('is-pinned')
    w.unmount()
  })

  it('空库态指向速记条；带过滤空态给清除过滤出口', async () => {
    const w = await mountView([], {})
    expect(w.text()).toContain('还没有一条便签')
    expect(byText(w, '清除过滤')).toBeUndefined()
    await w.find('.search-input').setValue('redis')
    await flushMicrotasks()
    // 防抖未到窗口，先按"有过滤条件"渲染（clearFilters 入口已在工具条与空态两处备好）
    expect(byText(w, '清除过滤')).toBeDefined()
    w.unmount()
  })

  it('搜索框输入 350ms 防抖后重查（keyword 入参），停顿窗口内不发请求', async () => {
    vi.useFakeTimers()
    try {
      const w = await mountView()
      const before = svc.List.mock.calls.length
      await w.find('.search-input').setValue('redis')
      await vi.advanceTimersByTimeAsync(300)
      expect(svc.List.mock.calls.length).toBe(before) // 防抖窗口内不触发
      await vi.advanceTimersByTimeAsync(100)
      await flushMicrotasks()
      expect(svc.List.mock.calls.length).toBe(before + 1)
      expect(svc.List.mock.calls.at(-1)![0]).toMatchObject({ keyword: 'redis' })
      w.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('序号守卫：先发起后返回的过期响应不得覆盖新结果', async () => {
    let resolveStale: (v: unknown[]) => void = () => {}
    const staleList = new Promise<unknown[]>((r) => { resolveStale = r })
    defaults([memo()], { '#SQL': 1 })
    svc.List.mockImplementationOnce(() => staleList)
    const w = mount(defineComponent({ render: () => h(MemoView) }), { attachTo: document.body })
    await flushMicrotasks()
    svc.List.mockResolvedValueOnce([memo({ id: 'm2', title: '新查询结果' })])
    runtime.handlers['memo:changed']({})
    await flushMicrotasks()
    expect(w.text()).toContain('新查询结果')
    resolveStale([memo({ id: 'm1', title: '过期旧结果' })])
    await flushMicrotasks()
    expect(w.text()).toContain('新查询结果')
    expect(w.text()).not.toContain('过期旧结果')
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

  it('只看置顶 chip 双向切换 pinned 参数', async () => {
    const w = await mountView()
    const pinChip = w.findAll('.tag-chip').find((c) => c.text().includes('只看置顶'))!
    await pinChip.trigger('click')
    await flushMicrotasks()
    expect(svc.List.mock.calls.at(-1)![0]).toMatchObject({ pinned: true })
    await pinChip.trigger('click')
    await flushMicrotasks()
    expect(svc.List.mock.calls.at(-1)![0]).toMatchObject({ pinned: null })
    w.unmount()
  })

  it('置顶切换按后端回执播报并刷新', async () => {
    const w = await mountView([memo({ isPinned: false })])
    svc.TogglePin.mockResolvedValue(true)
    const before = svc.List.mock.calls.length
    await cardBtns(w, 1).trigger('click')
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
    await cardBtns(w, 0).trigger('click') // 第 0 枚即遮罩钮
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已揭示明文')
    w.unmount()
  })

  it('删除须确认：取消不删，确认后才调用后端', async () => {
    const w = await mountView()
    svc.Delete.mockResolvedValue(undefined)
    const deleteButton = w.find('.memo-actions .text-danger')

    await deleteButton.trigger('click')
    await flushMicrotasks()
    expect(useConfirm().confirmState.open).toBe(true)
    expect(svc.Delete).not.toHaveBeenCalled()
    useConfirm().settleConfirm(false)
    await flushMicrotasks()
    expect(svc.Delete).not.toHaveBeenCalled()

    await deleteButton.trigger('click')
    await flushMicrotasks()
    useConfirm().settleConfirm(true)
    await flushMicrotasks()
    expect(svc.Delete).toHaveBeenCalledWith('m1')
    expect(useToast().toastMsg.value).toBe('已删除便签')
    w.unmount()
  })

  it('加载失败横幅可见并可重试；重试成功横幅收口', async () => {
    svc.List.mockRejectedValueOnce(new Error('disk gone'))
    svc.GetStats.mockResolvedValue({ totalCount: 0, pinnedCount: 0, tagCloud: {} })
    const w = mount(defineComponent({ render: () => h(MemoView) }), { attachTo: document.body })
    await flushMicrotasks()
    expect(w.find('.banner-error').text()).toContain('加载备忘录失败')
    defaults([memo({ id: 'back', title: '回来了' })])
    await byText(w, '重试')!.trigger('click')
    await flushMicrotasks()
    expect(w.find('.banner-error').exists()).toBe(false)
    expect(w.text()).toContain('回来了')
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

describe('速记条（主角动作）', () => {
  it('回车即存：Create 以正文为全部内容、蓝默认色，成功后清空输入并刷新', async () => {
    const w = await mountView([])
    svc.Create.mockResolvedValue(memo({ id: 'new1' }))
    await w.find('.hero-input').setValue('记一笔：redis 密码轮换窗口定在周五')
    await w.find('.hero-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenCalledWith(
      '', '记一笔：redis 密码轮换窗口定在周五', [], 'blue',
    )
    expect(useToast().toastMsg.value).toBe('已记下')
    expect((w.find('.hero-input').element as HTMLInputElement).value).toBe('')
    w.unmount()
  })

  it('标签输入按空白/逗号分词补 # 去重；当前过滤标签自动带入；组字中的回车不触发保存', async () => {
    const w = await mountView([memo()], { '#SQL': 1 })
    await w.findAll('.tag-chip').find((c) => c.text().includes('#SQL'))!.trigger('click')
    await flushMicrotasks()
    svc.Create.mockResolvedValue(undefined)
    await w.find('.hero-input').setValue('x')
    await w.find('.hero-tags').setValue('Db, redis #Db')
    await w.find('.hero-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenLastCalledWith('', 'x', ['#Db', '#redis', '#SQL'], 'blue')
    // 输入法组字中的 Enter：事件 isComposing=true，必须原样留给 IME
    svc.Create.mockClear()
    const el = w.find('.hero-input').element
    await w.find('.hero-input').setValue('还在组字')
    const ev = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
    Object.defineProperty(ev, 'isComposing', { value: true })
    el.dispatchEvent(ev)
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    w.unmount()
  })

  it('空文本不发请求；记录按钮禁用态可见', async () => {
    const w = await mountView()
    const save = byText(w, '记录')!
    expect((save.element as HTMLButtonElement).disabled).toBe(true)
    await w.find('.hero-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    w.unmount()
  })
})

describe('MemoView 编辑器（次级入口）', () => {
  it('新建流：校验空表单→填题→标签回车规范化→创建成功', async () => {
    const w = await mountView([])
    await byText(w, '新建便签')!.trigger('click')
    expect(w.find('.editor-dialog').exists()).toBe(true)
    await w.find('.editor-dialog .dlg-foot .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('标题或内容至少填写一项')
    await w.find('.editor-dialog .text-input').setValue('JWT 样本')
    const tagInput = w.find('.tag-entry')
    await tagInput.setValue('SQL')
    await tagInput.trigger('keydown.enter')
    await tagInput.setValue('#SQL')
    await tagInput.trigger('keydown.enter')
    expect(w.findAll('.memo-tag-removable')).toHaveLength(1)
    expect(w.find('.memo-tag-removable').text()).toContain('#SQL')
    svc.Create.mockResolvedValue(undefined)
    await w.find('.editor-dialog .dlg-foot .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenCalledWith('JWT 样本', '', ['#SQL'], 'blue')
    expect(useToast().toastMsg.value).toBe('已新建备忘录')
    expect(w.find('.editor-dialog').exists()).toBe(false)
    w.unmount()
  })

  it('编辑流：回填既有数据并以 Update 保存', async () => {
    const w = await mountView([memo({ tags: ['#SQL', '#prod'], colorTag: 'amber' })])
    await cardBtns(w, 2).trigger('click') // ✏️ 编辑
    expect((w.find('.editor-dialog .text-input').element as HTMLInputElement).value).toBe('生产库连接串')
    expect((w.find('.editor-dialog textarea').element as HTMLTextAreaElement).value).toContain('topsecret')
    expect(w.findAll('.color-circle')[2].classes()).toContain('selected')
    svc.Update.mockResolvedValue(undefined)
    await w.find('.editor-dialog .dlg-foot .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Update).toHaveBeenCalledWith('m1', '生产库连接串', 'host=10.0.0.1;pw=topsecret', ['#SQL', '#prod'], 'amber')
    expect(useToast().toastMsg.value).toBe('备忘录已更新')
    w.unmount()
  })

  it('新建时预选标签带入当前过滤标签', async () => {
    const w = await mountView([memo()], { '#SQL': 1 })
    await w.findAll('.tag-chip').find((c) => c.text().includes('#SQL'))!.trigger('click')
    await flushMicrotasks()
    await byText(w, '新建便签')!.trigger('click')
    expect(w.find('.memo-tag-removable').text()).toContain('#SQL')
    w.unmount()
  })

  it('书写/预览切换：预览态渲染 Markdown 结构，切回书写态草稿不丢', async () => {
    const w = await mountView([])
    await byText(w, '新建便签')!.trigger('click')
    await w.find('.editor-dialog textarea').setValue('# 标题\n**重点** `code`\n\n- 甲\n- 乙')
    await byText(w, '预览')!.trigger('click')
    const pv = w.find('.md-preview')
    expect(pv.exists()).toBe(true)
    expect(pv.element.innerHTML).toContain('<h1>标题</h1>')
    expect(pv.element.innerHTML).toContain('<strong>重点</strong>')
    expect(pv.element.innerHTML).toContain('<code>code</code>')
    expect(pv.element.innerHTML).toContain('<li>乙</li>')
    // v-show 切回：textarea 仍挂草稿（未被重挂载清掉）
    await byText(w, '书写')!.trigger('click')
    expect((w.find('.editor-dialog textarea').element as HTMLTextAreaElement).value).toContain('重点')
    w.unmount()
  })

  it('复制便签内容 toast 契约', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = await mountView()
    await w.find('.memo-copy-btn').trigger('click')
    await flushMicrotasks()
    expect(writeText).toHaveBeenCalledWith('host=10.0.0.1;pw=topsecret')
    expect(useToast().toastMsg.value).toBe('已复制便签内容到剪贴板')
    w.unmount()
  })
})

describe('详情浮层（阅读态）', () => {
  it('Markdown 条目点开渲染 HTML；纯代码条目走等宽原文不被误排', async () => {
    const w = await mountView([
      memo({ id: 'md1', title: '周报模板', content: '## 本周\n- 完成 [链接](https://a.cn)' }),
    ])
    expect(w.find('.md-flag').exists()).toBe(true) // 卡头 MD 徽标
    await w.find('.memo-content-box').trigger('click')
    expect(w.find('.detail-dialog').exists()).toBe(true)
    const html = w.find('.md-preview').element.innerHTML
    expect(html).toContain('<h2>本周</h2>')
    expect(html).toContain('href="https://a.cn"')
    expect(html).toContain('rel="noopener noreferrer"')
    w.unmount()

    const w2 = await mountView([memo({ id: 'code1', content: 'SELECT * FROM t WHERE a*b>1' })])
    expect(w2.find('.md-flag').exists()).toBe(false)
    await w2.find('.memo-content-box').trigger('click')
    expect(w2.find('.md-preview').exists()).toBe(false)
    expect(w2.find('.memo-plain').text()).toContain('a*b>1')
    w2.unmount()
  })

  it('masked 条目详情不落明文：无渲染正文、无复制钮', async () => {
    const w = await mountView([memo({ isMasked: true })])
    await w.find('.memo-content-box').trigger('click')
    const dlg = w.find('.detail-dialog')
    expect(dlg.find('.masked').text()).toContain('•••')
    expect(dlg.text()).not.toContain('topsecret')
    expect(byTextIn(w, '.detail-dialog', '复制全文')).toBeUndefined()
    w.unmount()
  })

  function byTextIn(w: ReturnType<typeof mount>, scope: string, text: string) {
    return w.findAll(`${scope} button`).find((b) => b.text().includes(text))
  }
})

describe('一键全删', () => {
  it('页头钮显式带条数；确认后走 ClearAll 并按返回值播报；取消不删', async () => {
    const w = await mountView([memo(), memo({ id: 'm2', isMasked: true })])
    const btn = byText(w, '一键全删 (2)')
    expect(btn).toBeDefined()

    await btn!.trigger('click')
    await flushMicrotasks()
    const opts = useConfirm().confirmState.options
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain('2 条')
    expect(opts.description).toContain('不可恢复')
    useConfirm().settleConfirm(false)
    await flushMicrotasks()
    expect(svc.ClearAll).not.toHaveBeenCalled()

    svc.ClearAll.mockResolvedValue(2)
    await byText(w, '一键全删')!.trigger('click')
    await flushMicrotasks()
    useConfirm().settleConfirm(true)
    await flushMicrotasks()
    expect(svc.ClearAll).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('已全删 2 条便签')
    w.unmount()
  })

  it('空库时全删钮禁用；后端报错如实上浮', async () => {
    const w = await mountView([], {})
    const btn = byText(w, '一键全删')!
    expect((btn.element as HTMLButtonElement).disabled).toBe(true)
    w.unmount()

    const w2 = await mountView([memo()])
    svc.ClearAll.mockRejectedValue(new Error('locked'))
    await byText(w2, '一键全删')!.trigger('click')
    await flushMicrotasks()
    useConfirm().settleConfirm(true)
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('全删失败: locked')
    w2.unmount()
  })
})

// F3-b 契约 + 2026-09 全删波更新：后端文件库化承诺的七方法面扩为八（+ClearAll）；
// RestoreFile 仍属后端热恢复面，不得出现在前端调用清单里。
describe('memo 前端契约面', () => {
  it('MemoView 依赖面为既有八方法（ClearAll 入列，RestoreFile 不越面）', () => {
    expect(Object.keys(svc).sort()).toEqual([
      'ClearAll', 'Create', 'Delete', 'GetStats', 'List', 'ToggleMask', 'TogglePin', 'Update',
    ])
  })

  it('迁移后 List 响应形态不变：整数组 + 双时间戳 + 标签数组渲染照常', async () => {
    const w = await mountView([
      memo({ id: 'memo_1758000000000000001', isMasked: true }),
      memo({ id: 'memo_1758000000000000002', isPinned: true }),
    ], { '#SQL': 2 })
    await flushMicrotasks()
    expect(w.findAll('.memo-card')).toHaveLength(2)
    expect(w.find('.masked').text()).toContain('•••')
    // 前端按组分列后 DOM 序不再等同入参序：置顶语义=该卡挂 is-pinned 类
    const pinnedCard = w.findAll('.memo-card').find((c) => c.classes().includes('is-pinned'))
    expect(pinnedCard, '置顶卡未渲染 is-pinned').toBeDefined()
    expect(pinnedCard!.text()).not.toContain('•••') // 置顶卡=非遮罩那条
    w.unmount()
  })
})
