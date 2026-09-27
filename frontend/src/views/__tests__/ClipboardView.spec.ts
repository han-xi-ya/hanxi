// ClipboardView 行为规格（A4 主界面线，契约 docs/plans/2026-09-26-clipboard-contract.md）。
//
// 后端接缝纪律的反证场：kernel 冻结面九方法全部经 setClipboardTransport(fake)
// 注入（afterEach 置 null 摘回），事件用 vi.mock 的 useWailsEvent 捕获 handler
// 手工投喂——本文件零 bindings/ 引用（未生成，静态 import 会炸解析）。
//
// 钉死的契约行为：
//   ① 渲染三态：列表 / 空态两分支 / 错误横幅（adapter 未接线的中文错必须上屏，
//      不许白屏）+ 重试收口；
//   ② 检索 350ms 防抖 + 回车立查 + 序号守卫语义；kind chips 驱动 List 参数，
//      「片段」合成档 = 后端拉 all + 前端 manual 过滤；
//   ③ 事件增量：updated 无过滤态就地顶置（去重先摘后插）不重拉 List；removed
//      摘行并连带收详情；paused 回灌状态灯（托盘翻牌同步）；
//   ④ 详情 Get 记一次使用：回值就地回填行内 useCount，绝不补发全量 List；
//      三分支（text/image/file）与失败重试；
//   ⑤ 危险闸：单条删除与清空都过 useConfirm（清空话术含 blob 与条数）；
//   ⑥ 九方法面对位 + sensitive「不进 AI 通道」徽标 + 状态一行。
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ClipboardView from '../ClipboardView.vue'
import { CLIP_EV, setClipboardTransport } from '../../adapters/clipboard'
import { useConfirm } from '../../composables/useConfirm'
import { usePrompt } from '../../composables/usePrompt'
import { useToast } from '../../composables/useToast'

// ---- 事件捕获桩：任务书指定 vi.mock useWailsEvent（不碰 @wailsio/runtime） ----
const events = vi.hoisted(() => ({
  handlers: {} as Record<string, (data: unknown) => void>,
  unlisten: vi.fn(),
}))
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: (name: string, cb: (data: unknown) => void) => {
    events.handlers[name] = cb
    return events.unlisten
  },
}))

// ---- 冻结九方法面假件 ----
const fake = vi.hoisted(() => ({
  List: vi.fn(),
  Get: vi.fn(),
  Set: vi.fn(),
  CreateText: vi.fn(),
  TogglePin: vi.fn(),
  Delete: vi.fn(),
  ClearAll: vi.fn(),
  SetPaused: vi.fn(),
  GetStatus: vi.fn(),
}))

const NOW = Date.now()
const clip = (over: Record<string, unknown> = {}) => ({
  id: 'c1',
  hash: 'h1',
  kind: 'text',
  preview: 'git commit -m 规范',
  byteSize: 42,
  createdAt: NOW - 5 * 60_000,
  sourceApp: 'Terminal',
  ...over,
})
const status = (over: Record<string, unknown> = {}) => ({
  paused: false,
  entryCount: 1,
  blobBytes: 2048,
  maxEntries: 500,
  maxBlobBytes: 104857600,
  excludedExes: ['1password.exe', 'keepass.exe'],
  ...over,
})

async function flush(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function defaults(items: unknown[] = [clip()]) {
  fake.List.mockResolvedValue(items)
  fake.GetStatus.mockResolvedValue(status({ entryCount: items.length }))
}

async function mountView(items?: unknown[]) {
  defaults(items)
  const w = mount(ClipboardView, { attachTo: document.body })
  await flush()
  return w
}

const byText = (w: ReturnType<typeof mount>, text: string) =>
  w.findAll('button').find((b) => b.text().includes(text))
const chip = (w: ReturnType<typeof mount>, text: string) =>
  w.findAll('.ct-chip').find((c) => c.text().includes(text))!
const rowTool = (w: ReturnType<typeof mount>, nth: number, label: string) =>
  w.findAll('.clip-row')[nth].findAll('.cr-tools button').find((b) => b.attributes('aria-label') === label)!

beforeEach(() => {
  for (const k of Object.keys(events.handlers)) delete events.handlers[k]
  events.unlisten.mockClear()
  for (const fn of Object.values(fake)) fn.mockReset() // 每案从净胎起，跨案调用计数互不污染
  setClipboardTransport(fake as never)
})

afterEach(() => {
  setClipboardTransport(null) // kernel 门面必须摘回假件，防跨案串线
  useConfirm().settleConfirm(false)
  usePrompt().settlePrompt(null)
  useToast().clearToast()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('渲染三态与门面纪律', () => {
  it('挂载即 List("","all",500) 拉满容量 + GetStatus 回灌状态一行与排除提示', async () => {
    const w = await mountView([
      clip({ autoTags: ['url', 'cjk'], useCount: 2 }),
    ])
    expect(fake.List).toHaveBeenCalledTimes(1)
    expect(fake.List).toHaveBeenCalledWith('', 'all', 500)
    expect(fake.GetStatus).toHaveBeenCalled()
    expect(w.findAll('.clip-row')).toHaveLength(1)
    expect(w.find('.cr-preview').text()).toBe('git commit -m 规范')
    expect(w.findAll('.cr-tag').map((t) => t.text())).toEqual(['url', 'cjk'])
    expect(w.find('.clip-stats-line').text()).toBe('条目 1/500 · 图片占 2 KB/100.0 MB')
    expect(w.find('.clip-stats-excl').text()).toBe('排除应用 2 个')
    expect(w.find('.clip-stats-excl').attributes('title')).toContain('1password.exe')
    w.unmount()
  })

  it('adapter 未接线：中文错渲染成错误横幅不白屏，重试成功后横幅收口', async () => {
    setClipboardTransport(null) // 收口前的真实世界：门面 need() 抛中文可读错
    const w = mount(ClipboardView, { attachTo: document.body })
    await flush()
    const banner = w.find('.banner-error')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('加载剪贴板历史失败')
    expect(banner.text()).toContain('剪贴板服务尚未接线')
    expect(w.find('.ct-bar').exists()).toBe(true) // 工具条照常在场，不是白屏
    setClipboardTransport(fake as never)
    defaults()
    await byText(w, '重试')!.trigger('click')
    await flush()
    expect(w.find('.banner-error').exists()).toBe(false)
    expect(w.findAll('.clip-row')).toHaveLength(1)
    w.unmount()
  })

  it('空库态给入册指引；带过滤空态给清除过滤出口并真清', async () => {
    const w = await mountView([])
    expect(w.text()).toContain('还没有记录到任何内容')
    defaults([])
    await w.find('.ct-search-input').setValue('redis')
    await w.find('.ct-search-input').trigger('keydown', { key: 'Enter' })
    await flush()
    expect(fake.List).toHaveBeenCalledWith('redis', 'all', 500)
    expect(w.text()).toContain('没有符合当前检索/过滤')
    await byText(w, '清除过滤')!.trigger('click')
    await flush()
    expect(fake.List.mock.calls.at(-1)![0]).toBe('')
    w.unmount()
  })

  it('相对时间/来源/敏感徽标随行上屏（⚠ 明写不进 AI 通道）', async () => {
    const w = await mountView([
      clip({ sensitive: true, createdAt: NOW - 3 * 3_600_000 }),
      clip({ id: 'c2', pinned: true, manual: true }),
    ])
    const first = w.findAll('.clip-row')[0]
    expect(first.find('.cr-time').text()).toBe('3 小时前')
    expect(first.find('.chip-danger').text()).toContain('敏感')
    expect(first.find('.chip-danger').attributes('title')).toContain('不进 AI')
    const second = w.findAll('.clip-row')[1]
    expect(second.find('.cr-pin').text()).toContain('置顶')
    expect(second.text()).toContain('片段')
    w.unmount()
  })
})

describe('检索：防抖 + 回车 + kind 档', () => {
  it('输入 350ms 防抖后重查；回车不等防抖立查；停顿窗口内不发请求', async () => {
    vi.useFakeTimers()
    try {
      const w = await mountView()
      const before = fake.List.mock.calls.length
      await w.find('.ct-search-input').setValue('redis')
      await vi.advanceTimersByTimeAsync(300)
      expect(fake.List.mock.calls.length).toBe(before) // 防抖窗口内不触发
      await vi.advanceTimersByTimeAsync(60)
      await flush()
      expect(fake.List.mock.calls.at(-1)![0]).toBe('redis')
      await w.find('.ct-search-input').setValue('nginx')
      await w.find('.ct-search-input').trigger('keydown', { key: 'Enter' })
      await flush() // 同拍清抖：回车后的值立即成为唯一查询
      expect(fake.List.mock.calls.at(-1)![0]).toBe('nginx')
      await vi.advanceTimersByTimeAsync(400)
      await flush()
      expect(fake.List.mock.calls.length).toBe(before + 2) // 旧抖已作废不补发
      w.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('图片档把 kind 交给后端；片段档拉 all 后前端按 manual 过滤行', async () => {
    const w = await mountView([
      clip({ id: 't1' }),
      clip({ id: 'm1', preview: '常用命令', manual: true }),
      clip({ id: 'i1', kind: 'image', preview: '' }),
    ])
    await chip(w, '图片').trigger('click')
    await flush()
    expect(fake.List.mock.calls.at(-1)).toEqual(['', 'image', 500])
    expect(chip(w, '图片').classes()).toContain('active')
    await chip(w, '片段').trigger('click')
    await flush()
    expect(fake.List.mock.calls.at(-1)).toEqual(['', 'all', 500])
    const rows = w.findAll('.clip-row')
    expect(rows).toHaveLength(1) // 只有 manual 那条留场
    expect(rows[0].text()).toContain('常用命令')
    await chip(w, '全部').trigger('click')
    await flush()
    expect(w.findAll('.clip-row')).toHaveLength(3)
    w.unmount()
  })
})

describe('事件驱动增量（不重拉全量为常态）', () => {
  it('clipboard:updated 无过滤态就地顶置；同 id 去重先摘后插；List 不补发', async () => {
    const w = await mountView([clip({ id: 'c1' }), clip({ id: 'c2', preview: '第二条' })])
    const listCalls = fake.List.mock.calls.length
    events.handlers[CLIP_EV.updated](clip({ id: 'c3', preview: '新的复制' }))
    await flush()
    expect(w.findAll('.clip-row')).toHaveLength(3)
    expect(w.findAll('.cr-preview')[0].text()).toBe('新的复制')
    events.handlers[CLIP_EV.updated](clip({ id: 'c1', preview: '去重顶置' }))
    await flush()
    expect(w.findAll('.clip-row')).toHaveLength(3)
    expect(w.findAll('.cr-preview')[0].text()).toBe('去重顶置')
    expect(fake.List.mock.calls.length).toBe(listCalls)
    w.unmount()
  })

  it('带过滤态的 updated 重拉一次保查询语义（不盲插脏行）', async () => {
    const w = await mountView([clip()])
    await chip(w, '图片').trigger('click')
    await flush()
    const listCalls = fake.List.mock.calls.length
    events.handlers[CLIP_EV.updated](clip({ id: 'c9' }))
    await flush()
    expect(fake.List.mock.calls.length).toBe(listCalls + 1)
    w.unmount()
  })

  it('clipboard:removed 摘行；正看的那条被摘时详情同收', async () => {
    const w = await mountView([clip({ id: 'c1' }), clip({ id: 'c2', preview: '第二条' })])
    fake.Get.mockResolvedValue(clip())
    await w.findAll('.cr-preview')[0].trigger('click')
    await flush()
    expect(w.find('.cd').exists()).toBe(true)
    events.handlers[CLIP_EV.removed]({ id: 'c1' })
    await flush()
    expect(w.findAll('.clip-row')).toHaveLength(1)
    expect(w.find('.cd').exists()).toBe(false)
    expect(w.find('.clip-idle').exists()).toBe(true)
    w.unmount()
  })

  it('clipboard:paused 回灌状态灯（托盘翻牌主窗同步亮）', async () => {
    const w = await mountView()
    expect(w.find('.ct-pause').text()).toContain('记录中')
    events.handlers[CLIP_EV.paused]({ paused: true })
    await flush()
    expect(w.find('.ct-pause').text()).toContain('已暂停记录')
    w.unmount()
  })
})

describe('暂停开关', () => {
  it('点击走 SetPaused 本地翻牌；GetStatus 实况可带出初始暂停态', async () => {
    const w = await mountView()
    fake.SetPaused.mockResolvedValue(undefined)
    await w.find('.ct-pause').trigger('click')
    await flush()
    expect(fake.SetPaused).toHaveBeenCalledWith(true)
    expect(w.find('.ct-pause').text()).toContain('已暂停记录')
    expect(useToast().toastMsg.value).toContain('已暂停记录')
    w.unmount()

    defaults()
    fake.GetStatus.mockResolvedValue(status({ paused: true }))
    const w2 = mount(ClipboardView, { attachTo: document.body })
    await flush()
    expect(w2.find('.ct-pause').text()).toContain('已暂停记录')
    w2.unmount()
  })

  it('后端报错：开关不翻牌，失败原文上浮 toast', async () => {
    const w = await mountView()
    fake.SetPaused.mockRejectedValue(new Error('gate 拒绝'))
    await w.find('.ct-pause').trigger('click')
    await flush()
    expect(w.find('.ct-pause').text()).toContain('记录中')
    expect(useToast().toastMsg.value).toContain('暂停开关失败: gate 拒绝')
    w.unmount()
  })
})

describe('行内快操作', () => {
  it('复制走后端 Set（回填系统剪贴板）；失败走 error toast 话术', async () => {
    const w = await mountView()
    fake.Set.mockResolvedValue(undefined)
    await rowTool(w, 0, '复制到剪贴板').trigger('click')
    await flush()
    expect(fake.Set).toHaveBeenCalledWith('c1')
    expect(useToast().toastMsg.value).toBe('已回填到系统剪贴板，Ctrl+V 即贴')
    fake.Set.mockRejectedValue(new Error('被占用'))
    await rowTool(w, 0, '复制到剪贴板').trigger('click')
    await flush()
    expect(useToast().toastMsg.value).toContain('复制失败: 被占用')
    w.unmount()
  })

  it('置顶按回执就地换行：徽标翻转、不补发 List、Get 也不碰', async () => {
    const w = await mountView([clip({ pinned: false })])
    const listCalls = fake.List.mock.calls.length
    fake.TogglePin.mockResolvedValue(clip({ pinned: true, text: '不应进账本的明文' }))
    await rowTool(w, 0, '固定置顶').trigger('click')
    await flush()
    expect(fake.TogglePin).toHaveBeenCalledWith('c1')
    expect(w.findAll('.clip-row')[0].find('.cr-pin').exists()).toBe(true)
    expect(fake.List.mock.calls.length).toBe(listCalls)
    expect(fake.Get).not.toHaveBeenCalled()
    w.unmount()
  })

  it('删除过确认闸：取消一子不动，确认后 Delete+摘行+重取状态', async () => {
    const w = await mountView([clip({ id: 'c1' }), clip({ id: 'c2', preview: '第二条' })])
    fake.Delete.mockResolvedValue(undefined)
    const statusCalls = fake.GetStatus.mock.calls.length
    await rowTool(w, 0, '删除此条').trigger('click')
    await flush()
    expect(useConfirm().confirmState.open).toBe(true)
    expect(useConfirm().confirmState.options.tone).toBe('danger')
    useConfirm().settleConfirm(false)
    await flush()
    expect(fake.Delete).not.toHaveBeenCalled()
    expect(w.findAll('.clip-row')).toHaveLength(2)

    await rowTool(w, 0, '删除此条').trigger('click')
    await flush()
    useConfirm().settleConfirm(true)
    await flush()
    expect(fake.Delete).toHaveBeenCalledWith('c1')
    expect(w.findAll('.clip-row').map((r) => r.text().includes('git commit'))).toEqual([false])
    expect(fake.GetStatus.mock.calls.length).toBe(statusCalls + 1)
    w.unmount()
  })
})

describe('清空强确认', () => {
  it('空库禁用；确认话术含条数与 blob 全清；确认后清场并重取状态', async () => {
    const w0 = await mountView([])
    expect((w0.find('.ct-clear-all').element as HTMLButtonElement).disabled).toBe(true)
    w0.unmount()

    const w = await mountView([clip({ id: 'a' }), clip({ id: 'b' }), clip({ id: 'c' })])
    fake.GetStatus.mockResolvedValue(status({ entryCount: 3 }))
    await byText(w, '清空')!.trigger('click')
    await flush()
    const opts = useConfirm().confirmState.options
    expect(opts.tone).toBe('danger')
    expect(opts.description).toContain('3 条')
    expect(opts.description).toContain('blob')
    expect(opts.confirmLabel).toBe('清空 3 条')
    useConfirm().settleConfirm(false)
    await flush()
    expect(fake.ClearAll).not.toHaveBeenCalled()

    fake.ClearAll.mockResolvedValue(undefined)
    await byText(w, '清空')!.trigger('click')
    await flush()
    useConfirm().settleConfirm(true)
    await flush()
    expect(fake.ClearAll).toHaveBeenCalledTimes(1)
    expect(w.findAll('.clip-row')).toHaveLength(0)
    expect(useToast().toastMsg.value).toBe('已清空 3 条剪贴板历史')
    w.unmount()
  })
})

describe('新片段（prompt 通道）', () => {
  it('开框→trim 送 CreateText→回执顶置并带片段身份；取消与纯空白不入库', async () => {
    const w = await mountView([])
    fake.CreateText.mockResolvedValue(clip({ id: 'snip', preview: '常用命令', manual: true }))
    await w.find('.ct-create').trigger('click')
    await flush()
    expect(usePrompt().promptState.open).toBe(true)
    usePrompt().settlePrompt('  常用命令  ')
    await flush()
    expect(fake.CreateText).toHaveBeenCalledWith('常用命令')
    expect(w.findAll('.clip-row')).toHaveLength(1)
    expect(w.findAll('.clip-row')[0].text()).toContain('片段')
    expect(useToast().toastMsg.value).toBe('已新建固定片段')

    fake.CreateText.mockClear()
    await w.find('.ct-create').trigger('click')
    await flush()
    usePrompt().settlePrompt(null) // 取消
    await flush()
    expect(fake.CreateText).not.toHaveBeenCalled()

    await w.find('.ct-create').trigger('click')
    await flush()
    usePrompt().settlePrompt('   ') // 纯空白
    await flush()
    expect(fake.CreateText).not.toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('片段内容为空，未保存')
    w.unmount()
  })
})

describe('详情：Get 计数回填三分支与失败面', () => {
  it('打开详情走 Get；回值就地回填行内 useCount——绝不补发全量 List', async () => {
    const w = await mountView([clip({ useCount: 0 })])
    const listCalls = fake.List.mock.calls.length
    fake.Get.mockResolvedValue(clip({ text: '完整正文\n第二行', useCount: 3, lastUsedAt: NOW }))
    await w.find('.cr-preview').trigger('click')
    await flush()
    expect(fake.Get).toHaveBeenCalledWith('c1')
    expect(fake.Get).toHaveBeenCalledTimes(1)
    expect(w.find('.cd-plain').text()).toContain('完整正文')
    expect(w.findAll('.clip-row')[0].find('.cr-count').text()).toBe('用 3 次')
    expect(fake.List.mock.calls.length).toBe(listCalls) // 省一次整库读是契约行为
    w.unmount()
  })

  it('图片分支渲染 blob 预览与尺寸注记；文件分支逐条路径可复制', async () => {
    const w = await mountView([clip({ id: 'i1', kind: 'image', preview: '' })])
    fake.Get.mockResolvedValue(
      clip({ id: 'i1', kind: 'image', preview: '', blobData: 'iVBORw0KG', width: 800, height: 600 }),
    )
    await w.find('.cr-preview').trigger('click')
    await flush()
    expect(w.find('.cd-img').attributes('src')).toBe('data:image/png;base64,iVBORw0KG')
    expect(w.find('.cd-img-meta').text()).toContain('800×600')
    w.unmount()

    const w2 = await mountView([clip({ id: 'f1', kind: 'file', preview: '', files: ['x.txt'] })])
    fake.Get.mockResolvedValue(
      clip({ id: 'f1', kind: 'file', preview: '', files: ['C:\\a.txt', 'D:\\b.pdf'] }),
    )
    await w2.find('.cr-preview').trigger('click')
    await flush()
    expect(w2.findAll('.cd-file')).toHaveLength(2)
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    await w2.findAll('.cd-file-copy')[1].trigger('click')
    await flush()
    expect(writeText).toHaveBeenCalledWith('D:\\b.pdf')
    // 页脚「复制到剪贴板」走后端 Set 整条回填
    await w2.find('.cd-copy-all').trigger('click')
    await flush()
    expect(fake.Set).toHaveBeenCalledWith('f1')
    w2.unmount()
  })

  it('Get 失败：详情区错误横幅可重试；关详情/Esc 收页并作废在飞请求', async () => {
    const w = await mountView()
    fake.Get.mockRejectedValueOnce(new Error('gate 拒绝'))
    fake.Get.mockResolvedValueOnce(clip({ text: '重试后到了' }))
    await w.find('.cr-preview').trigger('click')
    await flush()
    expect(w.find('.cd-error').text()).toContain('读取详情失败')
    await w.find('.cd-error .btn').trigger('click')
    await flush()
    expect(w.find('.cd-plain').text()).toContain('重试后到了')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flush()
    expect(w.find('.cd').exists()).toBe(false)
    expect(w.find('.clip-idle').exists()).toBe(true)
    // 在飞详情被卸载/关闭作废：晚到的成功不复活已关的页
    let resolveLate: (v: unknown) => void = () => {}
    fake.Get.mockImplementationOnce(() => new Promise((r) => { resolveLate = r }))
    await w.find('.cr-preview').trigger('click')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flush()
    resolveLate(clip({ text: '迟到的明文' }))
    await flush()
    expect(w.find('.cd').exists()).toBe(false)
    expect(w.text()).not.toContain('迟到的明文')
    w.unmount()
  })
})

describe('门面九方法面对位与清理', () => {
  it('假件恰为契约冻结九方法（多一少一都算越面）', () => {
    expect(Object.keys(fake).sort()).toEqual([
      'ClearAll', 'CreateText', 'Delete', 'Get', 'GetStatus', 'List', 'Set', 'SetPaused', 'TogglePin',
    ])
  })

  it('三事件族 setup 期即订阅；卸载后全局 Esc 不再炸已关页面', async () => {
    const w = await mountView()
    expect(Object.keys(events.handlers).sort()).toEqual([
      'clipboard:paused', 'clipboard:removed', 'clipboard:updated',
    ])
    w.unmount()
    expect(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      events.handlers[CLIP_EV.updated](clip()) // 孤儿事件不得再改已卸载组件状态
    }).not.toThrow()
  })
})
