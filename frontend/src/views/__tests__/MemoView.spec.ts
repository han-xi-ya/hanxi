// MemoView 行为规格（2026-09-26 v2「摊开的笔记本」重做同步迁移；金标准语义一条不丢，
// 断言随结构换形——详情浮层/编辑模态并入右页面板，页头四连钮退入工具条溢出菜单）：
// 钉死既有纪律——搜索 350ms 防抖 + 序号守卫、标签自动补 #、删除强确认、
// 遮罩渲染语义（masked 行/页面板不落明文）、memo:changed 重拉与卸载注销、
// toast 文案契约逐字不变、速记回车即存（含输入法组字豁免）、
// 书写/预览分段（Markdown 渲染进 DOM、切回草稿不丢）、一键全删强确认
// （danger tone、条数入文案、走 ClearAll）、导出面（单条 .md + 全库汇总）与
// 悬浮速记卡配置（GetQuickSheetState/SetQuickSheetHotkey/ShowQuickSheet）全家。
// 契约测试维持十一方法面（RestoreFile 不得出现在前端面）。
// 结构迁移注：旧「卡头 MD 徽标」断言改为「页面板自动分流渲染」+徽标退役防回潮锁；
// 旧卡面四钮位次（遮罩/置顶/编辑/删除）拆为行内双钮（遮罩/置顶）+页脚动作（其余）。
// M4 接线收口注（2026-09-26）：行式索引 = MemoCard(list) 组件（.memo-row 钩子类保留，
// 行内件换 .mc-title/.mc-preview/.mc-btn/.mc-tag；masked 严口径含标题不渲染，哨兵
// 反证新增）；顶栏 = MemoToolbar（.mt-search-input/.mt-more-btn/.mt-menu，点外收合
// 由旧 mousedown 口径统一为其 document click capture 纪律——③号差异按组件侧收口）。
// 金标准 36 例语义逐条在案、断言语义未动，仅选择器随接线迁移；新增 3 例为收口补锁。
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
  GetQuickSheetState: vi.fn(),
  SetQuickSheetHotkey: vi.fn(),
  ShowQuickSheet: vi.fn(),
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

// 导出 util 部分打桩：只掐下载通道（可编排成功/降级两路），builder 留真身——
// 这样"masked 明文不进导出文件"是拿真实产出文本反证，不是断言一句假想调用。
const exportUtil = vi.hoisted(() => ({ downloadTextFile: vi.fn() }))
vi.mock('../../utils/memoexport', async (importOriginal) => {
  const mod = await importOriginal<typeof import('../../utils/memoexport')>()
  return { ...mod, downloadTextFile: exportUtil.downloadTextFile }
})

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

function defaults(
  items: unknown[] = [memo()],
  tagCloud: Record<string, number> = { '#SQL': 1 },
  sheet: { hotkey: string; hotkeyActive: boolean } | 'fail' = { hotkey: 'Ctrl+Alt+N', hotkeyActive: true },
) {
  svc.List.mockResolvedValue(items)
  svc.GetStats.mockResolvedValue({ totalCount: items.length, pinnedCount: 0, tagCloud })
  if (sheet === 'fail') svc.GetQuickSheetState.mockRejectedValue(new Error('gate refused'))
  else svc.GetQuickSheetState.mockResolvedValue(sheet)
}

async function mountView(
  items?: unknown[],
  tagCloud?: Record<string, number>,
  sheet?: { hotkey: string; hotkeyActive: boolean } | 'fail',
) {
  defaults(items, tagCloud, sheet)
  const w = mount(defineComponent({ render: () => h(MemoView) }), { attachTo: document.body })
  await flushMicrotasks()
  return w
}

// 行内微操作钮的固定位次（MemoCard 契约工具区）：0=遮罩 1=置顶
// （编辑/删除/复制/导出迁页脚，MD 徽标退役由 :md-flag=false 守住）
function rowBtns(w: ReturnType<typeof mount>, nth: number) {
  return w.findAll('.memo-row .mc-btn')[nth]
}

// 点第一行标题 = 翻开该条进阅读页（旧"点内容开详情浮层"的等价入口）
async function openRow(w: ReturnType<typeof mount>, nth = 0) {
  await w.findAll('.memo-row .mc-title')[nth].trigger('click')
  await flushMicrotasks()
}

// 工具条右端溢出菜单（MemoToolbar #more）：低频动作（浮窗速记/导出全库/一键全删）的唯一入口
async function openMenu(w: ReturnType<typeof mount>) {
  await w.find('.mt-more-btn').trigger('click')
  await nextTick()
}

// 速记热键折叠区：点栏底开关展开
async function openSheetSettings(w: ReturnType<typeof mount>) {
  await w.find('.sheet-disclosure').trigger('click')
  await nextTick()
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
  it('挂载拉取列表与统计，渲染索引行与标签过滤计数', async () => {
    const w = await mountView()
    expect(svc.List).toHaveBeenCalledTimes(1)
    expect(svc.GetStats).toHaveBeenCalled()
    expect(svc.List.mock.calls[0][0]).toMatchObject({
      keyword: '', tag: '', pinned: null, sortBy: 'updated', sortDesc: true,
    })
    expect(w.text()).toContain('生产库连接串')
    expect(w.find('.memo-row .mc-tag').text()).toBe('#SQL')
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
    expect(groups[0].find('.memo-row').classes()).toContain('is-pinned')
    w.unmount()
  })

  it('空库态指向速记入口；带过滤空态给清除过滤出口', async () => {
    const w = await mountView([], {})
    expect(w.text()).toContain('还没有一条便签')
    expect(byText(w, '清除过滤')).toBeUndefined()
    await w.find('.mt-search-input').setValue('redis')
    await flushMicrotasks()
    // 防抖未到窗口，先按"有过滤条件"渲染（清除过滤入口在工具条与空态两处备好）
    expect(byText(w, '清除过滤')).toBeDefined()
    w.unmount()
  })

  it('搜索框输入 350ms 防抖后重查（keyword 入参），停顿窗口内不发请求', async () => {
    vi.useFakeTimers()
    try {
      const w = await mountView()
      const before = svc.List.mock.calls.length
      await w.find('.mt-search-input').setValue('redis')
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
    await rowBtns(w, 1).trigger('click') // 第 1 枚即置顶钮
    await flushMicrotasks()
    expect(svc.TogglePin).toHaveBeenCalledWith('m1')
    expect(useToast().toastMsg.value).toBe('已置顶便签')
    expect(svc.List.mock.calls.length).toBe(before + 1)
    w.unmount()
  })

  it('脱敏遮罩：masked 行渲染圆点且可揭示', async () => {
    const w = await mountView([memo({ isMasked: true })])
    expect(w.find('.mc-preview').classes()).toContain('masked')
    expect(w.find('.mc-preview').text()).toContain('•••')
    svc.ToggleMask.mockResolvedValue(false)
    await rowBtns(w, 0).trigger('click') // 第 0 枚即遮罩钮
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('已揭示明文')
    w.unmount()
  })

  // 接线收口裁决（2026-09-26）哨兵反证：masked 严口径统一后，行（MemoCard 硬
  // 纪律）与页面板（本视图改吃严口径）两面都不落标题明文，属性位同扫。
  it('masked 严口径哨兵反证：行与页面板整棵子树不落标题明文', async () => {
    const w = await mountView([
      memo({ isMasked: true, title: 'SENTINEL-密题', content: 'pw=SENTINEL-密文' }),
    ])
    const row = w.find('.memo-row')
    expect(row.text()).toContain('敏感便签') // 标题位换固定文案
    expect(row.text()).toContain('•••') // 正文位圆点照旧
    const rowHtml = row.html()
    expect(rowHtml).not.toContain('SENTINEL-密题')
    expect(rowHtml).not.toContain('SENTINEL-密文')
    for (const node of [row, ...row.findAll('*')]) {
      for (const v of Object.values(node.attributes())) {
        expect(String(v)).not.toContain('SENTINEL')
      }
    }
    await openRow(w)
    const pane = w.find('.pane-read')
    expect(pane.find('.pane-title').text()).toBe('敏感便签')
    expect(pane.text()).not.toContain('SENTINEL-密题')
    expect(pane.text()).not.toContain('SENTINEL-密文')
    expect(pane.text()).toContain('•••')
    w.unmount()
  })

  it('删除须确认：取消不删，确认后才调用后端', async () => {
    const w = await mountView()
    svc.Delete.mockResolvedValue(undefined)
    await openRow(w)
    const deleteButton = w.find('.memo-del-btn')

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

describe('速记入口（一行克制输入，回车即存语义原样）', () => {
  it('回车即存：Create 以正文为全部内容、蓝默认色，成功后清空输入并刷新', async () => {
    const w = await mountView([])
    svc.Create.mockResolvedValue(memo({ id: 'new1' }))
    await w.find('.quick-input').setValue('记一笔：redis 密码轮换窗口定在周五')
    await w.find('.quick-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenCalledWith(
      '', '记一笔：redis 密码轮换窗口定在周五', [], 'blue',
    )
    expect(useToast().toastMsg.value).toBe('已记下')
    expect((w.find('.quick-input').element as HTMLInputElement).value).toBe('')
    w.unmount()
  })

  it('标签输入按空白/逗号分词补 # 去重；当前过滤标签自动带入；组字中的回车不触发保存', async () => {
    const w = await mountView([memo()], { '#SQL': 1 })
    await w.findAll('.tag-chip').find((c) => c.text().includes('#SQL'))!.trigger('click')
    await flushMicrotasks()
    svc.Create.mockResolvedValue(undefined)
    await w.find('.quick-input').setValue('x')
    await w.find('.quick-tags').setValue('Db, redis #Db')
    await w.find('.quick-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenLastCalledWith('', 'x', ['#Db', '#redis', '#SQL'], 'blue')
    // 输入法组字中的 Enter：事件 isComposing=true，必须原样留给 IME
    svc.Create.mockClear()
    const el = w.find('.quick-input').element
    await w.find('.quick-input').setValue('还在组字')
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
    await w.find('.quick-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    w.unmount()
  })

  // M3 遗留防漂移（接线收口轮）：速记卡「⏎ 存并留场 · Ctrl+⏎ 存并收 · 多行粘贴
  // 并条」三语义在主窗速记行对齐——主窗「收」的等价落点为"存下但不翻页抢阅读位"。
  it('速记行裸回车存并翻到该条；Ctrl+Enter 存而不翻页（对齐速记卡存并收口径）', async () => {
    const w = await mountView([memo({ id: 'm1' })])
    svc.Create.mockResolvedValue(memo({ id: 'm1' }))
    await w.find('.quick-input').setValue('裸回车一条')
    await w.find('.quick-input').trigger('keydown.enter')
    await flushMicrotasks()
    expect(w.find('.pane-read').exists()).toBe(true) // 原语义：新条翻给右页看
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(w.find('.pane-read').exists()).toBe(false)
    await w.find('.quick-input').setValue('存完就走')
    await w.find('.quick-input').trigger('keydown.enter.ctrl')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenCalledTimes(2)
    expect(useToast().toastMsg.value).toBe('已记下')
    expect(w.find('.pane-read').exists()).toBe(false) // 存而不抢翻页 = 主窗「收」
    w.unmount()
  })

  it('速记行粘贴多行并成一条并如实报数（与速记卡共用 flattenPastedLines）；单行零干预', async () => {
    const w = await mountView()
    await w.find('.quick-input').trigger('paste', {
      clipboardData: { getData: (fmt: string) => (fmt === 'text/plain' ? '甲\n乙\n \n丙' : '') },
    })
    await nextTick()
    expect((w.find('.quick-input').element as HTMLInputElement).value).toBe('甲 乙 丙')
    expect(useToast().toastMsg.value).toContain('已粘贴 3 行并并为一条速记')
    useToast().clearToast()
    await w.find('.quick-input').trigger('paste', {
      clipboardData: { getData: () => '本就一行' },
    })
    await nextTick()
    expect(useToast().toastMsg.value).toBe('') // 单行零干预：不吭声
    expect((w.find('.quick-input').element as HTMLInputElement).value).toBe('甲 乙 丙') // 也不改写草稿
    w.unmount()
  })
})

describe('页面板书写态（详情/编辑合并后的唯一书写页）', () => {
  it('新建流：校验空表单→填题→标签回车规范化→创建成功', async () => {
    const w = await mountView([])
    await byText(w, '长便签')!.trigger('click')
    expect(w.find('.pane-write').exists()).toBe(true)
    await w.find('.pane-write .pane-foot .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Create).not.toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('标题或内容至少填写一项')
    await w.find('.pane-write .pane-title-input').setValue('JWT 样本')
    const tagInput = w.find('.tag-entry')
    await tagInput.setValue('SQL')
    await tagInput.trigger('keydown.enter')
    await tagInput.setValue('#SQL')
    await tagInput.trigger('keydown.enter')
    expect(w.findAll('.memo-tag-removable')).toHaveLength(1)
    expect(w.find('.memo-tag-removable').text()).toContain('#SQL')
    svc.Create.mockResolvedValue(undefined)
    await w.find('.pane-write .pane-foot .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Create).toHaveBeenCalledWith('JWT 样本', '', ['#SQL'], 'blue')
    expect(useToast().toastMsg.value).toBe('已新建备忘录')
    expect(w.find('.pane-write').exists()).toBe(false)
    w.unmount()
  })

  it('编辑流：行点开阅读页→编辑钮回填→Update 保存', async () => {
    const w = await mountView([memo({ tags: ['#SQL', '#prod'], colorTag: 'amber' })])
    await openRow(w)
    expect(w.find('.pane-read').exists()).toBe(true)
    await byText(w, '编辑')!.trigger('click')
    expect((w.find('.pane-write .pane-title-input').element as HTMLInputElement).value).toBe('生产库连接串')
    expect((w.find('.pane-write textarea').element as HTMLTextAreaElement).value).toContain('topsecret')
    expect(w.findAll('.color-circle')[2].classes()).toContain('selected')
    svc.Update.mockResolvedValue(undefined)
    await w.find('.pane-write .pane-foot .btn-primary').trigger('click')
    await flushMicrotasks()
    expect(svc.Update).toHaveBeenCalledWith('m1', '生产库连接串', 'host=10.0.0.1;pw=topsecret', ['#SQL', '#prod'], 'amber')
    expect(useToast().toastMsg.value).toBe('备忘录已更新')
    w.unmount()
  })

  it('新建时预选标签带入当前过滤标签', async () => {
    const w = await mountView([memo()], { '#SQL': 1 })
    await w.findAll('.tag-chip').find((c) => c.text().includes('#SQL'))!.trigger('click')
    await flushMicrotasks()
    await byText(w, '长便签')!.trigger('click')
    expect(w.find('.memo-tag-removable').text()).toContain('#SQL')
    w.unmount()
  })

  it('书写/预览切换：预览态渲染 Markdown 结构，切回书写态草稿不丢', async () => {
    const w = await mountView([])
    await byText(w, '长便签')!.trigger('click')
    await w.find('.pane-write textarea').setValue('# 标题\n**重点** `code`\n\n- 甲\n- 乙')
    await byText(w, '预览')!.trigger('click')
    const pv = w.find('.md-preview')
    expect(pv.exists()).toBe(true)
    expect(pv.element.innerHTML).toContain('<h1>标题</h1>')
    expect(pv.element.innerHTML).toContain('<strong>重点</strong>')
    expect(pv.element.innerHTML).toContain('<code>code</code>')
    expect(pv.element.innerHTML).toContain('<li>乙</li>')
    // v-show 切回：textarea 仍挂草稿（未被重挂载清掉）
    await byText(w, '书写')!.trigger('click')
    expect((w.find('.pane-write textarea').element as HTMLTextAreaElement).value).toContain('重点')
    w.unmount()
  })

  it('复制便签内容 toast 契约（页脚入口）', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = await mountView()
    await openRow(w)
    await w.find('.memo-copy-btn').trigger('click')
    await flushMicrotasks()
    expect(writeText).toHaveBeenCalledWith('host=10.0.0.1;pw=topsecret')
    expect(useToast().toastMsg.value).toBe('已复制便签内容到剪贴板')
    w.unmount()
  })
})

describe('页面板阅读态（旧详情浮层的所得全数迁入）', () => {
  it('Markdown 条目自动渲染 HTML；纯代码条目走等宽原文不被误排；MD 徽标已退役', async () => {
    const w = await mountView([
      memo({ id: 'md1', title: '周报模板', content: '## 本周\n- 完成 [链接](https://a.cn)' }),
    ])
    expect(w.findAll('.md-flag, .mc-md')).toHaveLength(0) // 徽标退役防回潮（行换 MemoCard 仍由 :md-flag=false 熄徽）
    await openRow(w)
    expect(w.find('.pane-read').exists()).toBe(true)
    const html = w.find('.md-preview').element.innerHTML
    expect(html).toContain('<h2>本周</h2>')
    expect(html).toContain('href="https://a.cn"')
    expect(html).toContain('rel="noopener noreferrer"')
    w.unmount()

    const w2 = await mountView([memo({ id: 'code1', content: 'SELECT * FROM t WHERE a*b>1' })])
    expect(w2.findAll('.md-flag, .mc-md')).toHaveLength(0)
    await openRow(w2)
    expect(w2.find('.md-preview').exists()).toBe(false)
    expect(w2.find('.memo-plain').text()).toContain('a*b>1')
    w2.unmount()
  })

  it('masked 条目阅读页不落明文：无渲染正文、无复制钮', async () => {
    const w = await mountView([memo({ isMasked: true })])
    await openRow(w)
    const pane = w.find('.pane-read')
    expect(pane.find('.masked').text()).toContain('•••')
    expect(pane.text()).not.toContain('topsecret')
    expect(byTextIn(w, '.pane-read', '复制全文')).toBeUndefined()
    w.unmount()
  })

  function byTextIn(w: ReturnType<typeof mount>, scope: string, text: string) {
    return w.findAll(`${scope} button`).find((b) => b.text().includes(text))
  }
})

describe('溢出菜单与一键全删', () => {
  it('菜单项显式带条数；确认后走 ClearAll 并按返回值播报；取消不删', async () => {
    const w = await mountView([memo(), memo({ id: 'm2', isMasked: true })])
    await openMenu(w)
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
    await openMenu(w)
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
    await openMenu(w)
    const btn = byText(w, '一键全删')!
    expect((btn.element as HTMLButtonElement).disabled).toBe(true)
    w.unmount()

    const w2 = await mountView([memo()])
    svc.ClearAll.mockRejectedValue(new Error('locked'))
    await openMenu(w2)
    await byText(w2, '一键全删')!.trigger('click')
    await flushMicrotasks()
    useConfirm().settleConfirm(true)
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toBe('全删失败: locked')
    w2.unmount()
  })

  it('菜单开合纪律：点外收合，选中项即收（低频件不常驻台面）', async () => {
    const w = await mountView()
    expect(w.find('.mt-menu').exists()).toBe(false)
    await openMenu(w)
    expect(w.find('.mt-menu').exists()).toBe(true)
    // 模拟点外：click 落在工具条容器之外（M2 的 mousedown 口径已按
    // MemoToolbar 纪律统一收口为 document click capture）
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushMicrotasks()
    expect(w.find('.mt-menu').exists()).toBe(false)
    await openMenu(w)
    await byText(w, '浮窗速记')!.trigger('click') // 选中即收 + 直连 ShowQuickSheet（不依赖热键的唤出通道）
    await flushMicrotasks()
    expect(w.find('.mt-menu').exists()).toBe(false)
    expect(svc.ShowQuickSheet).toHaveBeenCalledTimes(1)
    w.unmount()
  })
})

// F3-b 契约 + 2026-09 全删波更新 + N16 B 批速记卡扩面：后端文件库化承诺的
// 七方法面扩为八（+ClearAll）再扩为十一（+GetQuickSheetState/SetQuickSheetHotkey/
// ShowQuickSheet）；RestoreFile 仍属后端热恢复面，不得出现在前端调用清单里。
describe('memo 前端契约面', () => {
  it('MemoView 依赖面为既有十一方法（ClearAll 与速记卡三件入列，RestoreFile 不越面）', () => {
    expect(Object.keys(svc).sort()).toEqual([
      'ClearAll', 'Create', 'Delete', 'GetQuickSheetState', 'GetStats', 'List',
      'SetQuickSheetHotkey', 'ShowQuickSheet', 'ToggleMask', 'TogglePin', 'Update',
    ])
  })

  it('迁移后 List 响应形态不变：整数组 + 双时间戳 + 标签数组渲染照常', async () => {
    const w = await mountView([
      memo({ id: 'memo_1758000000000000001', isMasked: true }),
      memo({ id: 'memo_1758000000000000002', isPinned: true }),
    ], { '#SQL': 2 })
    await flushMicrotasks()
    expect(w.findAll('.memo-row')).toHaveLength(2)
    expect(w.find('.masked').text()).toContain('•••')
    // 前端按组贴线后 DOM 序不再等同入参序：置顶语义=该行挂 is-pinned 类
    const pinnedRow = w.findAll('.memo-row').find((r) => r.classes().includes('is-pinned'))
    expect(pinnedRow, '置顶行未渲染 is-pinned').toBeDefined()
    expect(pinnedRow!.text()).not.toContain('•••') // 置顶行=非遮罩那条
    w.unmount()
  })
})

// N16 B 批：悬浮速记卡配置面（菜单唤出钮 + 栏底折叠热键区）。冲突回滚是后端
// 事务职责，前端只钉死"成功刷新回显 + 失败草稿对齐生效值 + 未在位如实提示"。
describe('悬浮速记卡配置', () => {
  it('折叠区回显生效键位；展开后保存走 SetQuickSheetHotkey 并重拉实况', async () => {
    const w = await mountView()
    await openSheetSettings(w)
    const input = w.get('input[aria-label="速记卡全局热键"]')
    expect((input.element as HTMLInputElement).value).toBe('Ctrl+Alt+N')
    svc.SetQuickSheetHotkey.mockResolvedValue(undefined)

    await input.setValue(' Ctrl+Alt+M ')
    await byText(w, '保存热键')!.trigger('click')
    await flushMicrotasks()
    expect(svc.SetQuickSheetHotkey).toHaveBeenCalledWith('Ctrl+Alt+M') // 送后端前 trim
    expect(svc.GetQuickSheetState.mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(useToast().toastMsg.value).toBe('速记热键已更新')
    w.unmount()
  })

  it('后端冲突报错：失败 toast 原文上浮，草稿对齐回滚后的生效值', async () => {
    const w = await mountView()
    svc.SetQuickSheetHotkey.mockRejectedValue(
      new Error('热键 "Ctrl+Alt+J" 设置失败（留空表示停用热键）：组合键 Ctrl+Alt+J 已被占用（可能被其他软件抢注），请到设置页改键'),
    )
    await openSheetSettings(w)
    const input = w.get('input[aria-label="速记卡全局热键"]')
    await input.setValue('Ctrl+Alt+J')
    await byText(w, '保存热键')!.trigger('click')
    await flushMicrotasks()
    expect(useToast().toastMsg.value).toContain('热键设置失败')
    expect(useToast().toastMsg.value).toContain('已被占用')
    expect((input.element as HTMLInputElement).value).toBe('Ctrl+Alt+N')
    w.unmount()
  })

  it('热键未在位给警示横幅（收起态也外露）；停用态给中性芯片；实况拉不到整段收起', async () => {
    const w = await mountView([memo()], { '#SQL': 1 }, { hotkey: 'Ctrl+Alt+J', hotkeyActive: false })
    expect(w.find('.banner-warn').text()).toContain('未在位')
    w.unmount()

    const w2 = await mountView([memo()], { '#SQL': 1 }, { hotkey: '', hotkeyActive: false })
    expect(w2.find('.memo-hotkey-off').text()).toContain('热键已停用')
    expect(w2.find('.banner-warn').exists()).toBe(false)
    w2.unmount()

    const w3 = await mountView([memo()], { '#SQL': 1 }, 'fail')
    expect(w3.find('.memo-sheet-settings').exists()).toBe(false)
    w3.unmount()
  })
})

// N16 C 批导出面（单条 .md + 全库汇总 .md，零新后端）：阅读页导出钮与
// 复制全文同挂遮罩纪律；下载通道不可用降级复制原文（同一份文档）。全库导出
// 三钉：取数永远无过滤重拉（视图过滤不得缩面）、确认框明说敏感排除条数、
// masked 明文经真实 builder 产出文本反证不进导出。入口自页头迁溢出菜单，
// 每次经 openMenu 打开——能力语义逐字不变。
describe('导出（单条 .md 与全库汇总）', () => {
  beforeEach(() => {
    exportUtil.downloadTextFile.mockReset() // 本族既编排返回值又数调用次数，须从净胎起
  })

  const dlgBtn = (w: ReturnType<typeof mount>, scope: string, text: string) =>
    w.findAll(`${scope} button`).find((b) => b.text().includes(text))

  it('阅读页「导出 .md」：下载与文件库同格式的 frontmatter 文档并按名播报', async () => {
    const w = await mountView([memo()])
    await openRow(w)
    exportUtil.downloadTextFile.mockReturnValue(true)
    await dlgBtn(w, '.pane-read', '导出 .md')!.trigger('click')
    await flushMicrotasks()
    expect(exportUtil.downloadTextFile).toHaveBeenCalledTimes(1)
    const [fileName, doc] = exportUtil.downloadTextFile.mock.calls[0] as [string, string]
    expect(fileName).toBe('生产库连接串-m1.md')
    expect(doc.startsWith('---\nid: m1\ntitle: 生产库连接串\ntags: ["#SQL"]')).toBe(true)
    expect(doc).toContain('masked: false')
    expect(doc.endsWith('---\nhost=10.0.0.1;pw=topsecret')).toBe(true)
    expect(useToast().toastMsg.value).toBe('已导出 生产库连接串-m1.md')
    w.unmount()
  })

  it('下载通道不可用：降级把同一份原文复制到剪贴板并如实播报', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = await mountView([memo()])
    await openRow(w)
    exportUtil.downloadTextFile.mockReturnValue(false)
    await dlgBtn(w, '.pane-read', '导出 .md')!.trigger('click')
    await flushMicrotasks()
    expect(writeText).toHaveBeenCalledTimes(1)
    expect(writeText.mock.calls[0][0] as string).toContain('id: m1')
    expect(useToast().toastMsg.value).toBe('保存通道不可用，原文已复制到剪贴板')
    w.unmount()
  })

  it('masked 条目阅读页：无复制全文亦无导出钮（明文不落眼不落剪贴板不落文件）', async () => {
    const w = await mountView([memo({ isMasked: true })])
    await openRow(w)
    expect(dlgBtn(w, '.pane-read', '导出 .md')).toBeUndefined()
    expect(dlgBtn(w, '.pane-read', '复制全文')).toBeUndefined()
    w.unmount()
  })

  it('全库导出：无过滤重拉→确认框明说排除条数→汇总文本经反证不含敏感明文', async () => {
    const items = [
      memo({ id: 'k1', title: '甲', content: 'kept-plain', isMasked: false }),
      memo({ id: 's1', title: '密', content: 'pw=topsecret', isMasked: true }),
      memo({ id: 'k2', title: '乙', content: 'kept-plain-2', isMasked: false, isPinned: true }),
    ]
    const w = await mountView(items)
    exportUtil.downloadTextFile.mockReturnValue(true)
    await openMenu(w)
    await byText(w, '导出全库')!.trigger('click')
    await flushMicrotasks()
    // 取数面：最后一次 List 必须是无过滤全集（当前视图若有过滤也不得带入）
    expect(svc.List.mock.calls.at(-1)![0]).toMatchObject({ keyword: '', tag: '', pinned: null })
    const opts = useConfirm().confirmState.options
    expect(opts.tone).toBe('warning')
    expect(opts.confirmLabel).toBe('导出 2 条')
    expect(opts.description).toContain('默认排除')
    expect(opts.details).toEqual([
      { label: '导出条数', value: '2 条' },
      { label: '敏感排除', value: '1 条（明文未写入）' },
    ])
    useConfirm().settleConfirm(true)
    await flushMicrotasks()
    expect(exportUtil.downloadTextFile).toHaveBeenCalledTimes(1)
    const [fileName, digest] = exportUtil.downloadTextFile.mock.calls[0] as [string, string]
    expect(fileName).toMatch(/^hanxi-随手记导出-\d{8}-\d{6}\.md$/)
    expect(digest).toContain('kept-plain')
    expect(digest).toContain('kept-plain-2')
    expect(digest).toContain('1 条已排除，明文未写入本文件')
    expect(digest).not.toContain('pw=topsecret')
    expect(digest).not.toContain('## 密')
    expect(useToast().toastMsg.value).toContain(`已导出 2 条 → ${fileName}`)
    w.unmount()
  })

  it('视图正带过滤时导出仍拉全集；确认取消则一个字都不落', async () => {
    const w = await mountView([memo({ id: 'k1', content: 'a' }), memo({ id: 'k2', content: 'b', isMasked: true })], { '#SQL': 2 })
    await w.find('.mt-search-input').setValue('a')
    exportUtil.downloadTextFile.mockReturnValue(true)
    await openMenu(w)
    await byText(w, '导出全库')!.trigger('click')
    await flushMicrotasks()
    expect(svc.List.mock.calls.at(-1)![0]).toMatchObject({ keyword: '', tag: '', pinned: null })
    useConfirm().settleConfirm(false)
    await flushMicrotasks()
    expect(exportUtil.downloadTextFile).not.toHaveBeenCalled()
    w.unmount()
  })

  it('全是敏感条目：不进确认框直接如实播报；空库钮禁用', async () => {
    const w = await mountView([memo({ isMasked: true })])
    await openMenu(w)
    await byText(w, '导出全库')!.trigger('click')
    await flushMicrotasks()
    expect(useConfirm().confirmState.open).toBe(false)
    expect(useToast().toastMsg.value).toBe('全部条目均为敏感遮罩，默认不导出')
    expect(exportUtil.downloadTextFile).not.toHaveBeenCalled()
    w.unmount()

    const w2 = await mountView([], {})
    await openMenu(w2)
    const btn = byText(w2, '导出全库')!
    expect((btn.element as HTMLButtonElement).disabled).toBe(true)
    w2.unmount()
  })
})
