// 剪贴板浮层（#clipboardoverlay 独立窗视图，A5 线）行为规格：
// 后端只经 @/adapters/clipboard 门面——本 spec 用 setClipboardTransport 注假件、
// afterEach 卸回 null（即 kernel 设计的测试 seam 本身，不 import 生成绑定）；
// 事件走 useWailsEvent（@wailsio/runtime 打桩收 handler，载荷 {data} 包装与真谱一致）。
// 覆盖：挂载取数（List 空词全量 + GetStatus）与置顶组顶置排序、行内元素（序号徽标
// 前 9 位/kind 图标/image·file 只显元数据/相对时间 lastUsedAt 优先）、opening 清稿
// 重聚焦重播入场、空词裸数字直选与 Alt+数字搜索中直选、有词数字放行给搜索词、
// ↑↓ 选中环夹紧 + Enter 直选、Esc 与行点击走 CollapseOverlay 显式 RPC 收窗（H1：
// blur 唤不动 Go 失焦，blur 断言作废）/ 复制不关窗 / 置顶就地回排、IME 三重盾
// （isComposing + compositionstart/end 本地旗标 + Process 键）、未知 kind 兜底 '?'
// 回显原文、paused 角标双喂（GetStatus + clipboard:paused）、updated 乐观顶置去重
// 与带词静默重拉、removed 摘行、Set 失败保窗报错、刷新失败不掀已用列表、adapter
// 未接线中文错误条不白屏且恢复接线后 opening 自愈、空态两式文案。
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ClipboardOverlay from '../ClipboardOverlay.vue'
import { setClipboardTransport } from '../../adapters/clipboard'
import type { ClipboardTransport } from '../../adapters/clipboard'
import type { ClipEntry, ClipStatus } from '../../types/clipboard'

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

function entry(over: Partial<ClipEntry> & { id: string }): ClipEntry {
  return {
    hash: `${over.id}-h`,
    kind: 'text',
    preview: `p-${over.id}`,
    byteSize: 10,
    createdAt: Date.now(),
    ...over,
  }
}

function status(over: Partial<ClipStatus> = {}): ClipStatus {
  return {
    paused: false,
    entryCount: 3,
    blobBytes: 0,
    maxEntries: 500,
    maxBlobBytes: 104857600,
    excludedExes: [],
    ...over,
  }
}

const MIN = 60_000

// —— 假件数据面：mock 身份模块级恒定（vi.fn 集合一次成型，beforeEach 清调用史并
// 重钉「读变量」的默认实现），测试只翻数据变量、不换 mock 引用——杜绝 transport
// 闭包仍指旧件的脏缝。收窗口径统一断 CollapseOverlay RPC（H1 审查后 blur 探针作废）。
let entries: ClipEntry[] = []
let statusSeed: Partial<ClipStatus> = {}
let listErr: Error | null = null
let setErr: Error | null = null
let statusErr: Error | null = null
const pinnedSet = new Set<string>() // TogglePin 真翻转语义的假账

const fake = {
  List: vi.fn(),
  Get: vi.fn(),
  Set: vi.fn(),
  CreateText: vi.fn(),
  TogglePin: vi.fn(),
  Delete: vi.fn(),
  ClearAll: vi.fn(),
  SetPaused: vi.fn(),
  GetStatus: vi.fn(),
  CollapseOverlay: vi.fn(),
} satisfies ClipboardTransport

function seedDefaults(): void {
  const now = Date.now()
  entries = [
    entry({ id: 'a1', preview: 'grep -rn foo', createdAt: now - 2 * MIN }),
    entry({ id: 'a2', preview: 'https://example.com/x', createdAt: now - 3 * MIN }),
    entry({ id: 'a3', preview: 'win11 更新说明', createdAt: now - 4 * MIN }),
  ]
  statusSeed = {}
  listErr = null
  setErr = null
  statusErr = null
  pinnedSet.clear()
}

async function flushMicrotasks(times = 25) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function mountOverlay() {
  return mount(defineComponent({ render: () => h(ClipboardOverlay) }), { attachTo: document.body })
}

function previews(w: ReturnType<typeof mountOverlay>): string[] {
  return w.findAll('.clip-preview').map((n) => n.text())
}

function press(key: string, mods: Partial<KeyboardEventInit> = {}): KeyboardEvent {
  const ev = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...mods })
  window.dispatchEvent(ev)
  return ev
}

beforeEach(() => {
  seedDefaults()
  vi.clearAllMocks()
  // 收窗断言恒以 CollapseOverlay RPC 调用为准——顶层窗 window.blur() 是 WHATWG
  // no-op（A8 审查 H1），spy 它只会假绿，探针彻底撤制
  fake.CollapseOverlay.mockImplementation(async () => undefined)
  fake.List.mockImplementation(async (q: string) => {
    if (listErr) throw listErr
    return q ? entries.filter((e) => e.preview.includes(q)) : [...entries]
  })
  fake.Set.mockImplementation(async (_id: string) => {
    if (setErr) throw setErr
  })
  fake.Get.mockImplementation(async (id: string) => entry({ id }))
  fake.TogglePin.mockImplementation(async (id: string) => {
    const cur = entries.find((e) => e.id === id) ?? entry({ id })
    const pinned = !pinnedSet.has(id)
    if (pinned) pinnedSet.add(id)
    else pinnedSet.delete(id)
    return { ...cur, pinned }
  })
  fake.GetStatus.mockImplementation(async () => {
    if (statusErr) throw statusErr
    return status(statusSeed)
  })
  setClipboardTransport(fake)
})

afterEach(() => {
  setClipboardTransport(null) // 卸载假件：transport 是模块级单例，不留脏
  vi.restoreAllMocks() // 收 focus 探针（blur 探针已随 H1 撤制）
  for (const k of Object.keys(runtime.handlers)) delete runtime.handlers[k]
  document.body.innerHTML = ''
})

describe('ClipboardOverlay 取数与呈现', () => {
  it('挂载即拉全量（List("", "", 100) 空词不过滤）+ GetStatus；计数与 kind 图标随行', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    expect(fake.List).toHaveBeenCalledWith('', '', 100)
    expect(fake.GetStatus).toHaveBeenCalledTimes(1)
    expect(previews(w)).toEqual(['grep -rn foo', 'https://example.com/x', 'win11 更新说明'])
    expect(w.find('.clip-count').text()).toBe('3 条')
    expect(w.findAll('.clip-kind').map((n) => n.text())).toEqual(['文', '文', '文'])
    expect(w.find('.clip-paused').exists()).toBe(false)
    w.unmount()
  })

  it('置顶组在前：pinned 与 manual 同组顶置挂徽标（manual 显「定」），组内与近期组各保服务端新→旧序', async () => {
    entries = [
      entry({ id: 'n1' }),
      entry({ id: 'p1', pinned: true }),
      entry({ id: 'n2' }),
      entry({ id: 'm1', manual: true, pinned: true }),
    ]
    const w = mountOverlay()
    await flushMicrotasks()
    expect(previews(w)).toEqual(['p-p1', 'p-m1', 'p-n1', 'p-n2'])
    expect(w.findAll('.clip-row')[0].find('.clip-badge').text()).toBe('顶')
    expect(w.findAll('.clip-row')[1].find('.clip-badge').text()).toBe('定')
    w.unmount()
  })

  it('行内元素：前 9 行数字徽标（第 10 行起留空）、image/file 只显元数据、时间取 lastUsedAt 优先', async () => {
    const now = Date.now()
    entries = [
      entry({ id: 't1', createdAt: now - 5 * MIN }),
      entry({ id: 't2', createdAt: now - 3 * 60 * MIN, lastUsedAt: now - 50 * MIN }),
      ...Array.from({ length: 7 }, (_, i) => entry({ id: `t${i + 3}`, createdAt: now })),
      entry({ id: 't10', preview: '第十行不留号', createdAt: now }),
      entry({ id: 'img', kind: 'image', preview: '', width: 1280, height: 720, byteSize: 245760, createdAt: now - 3 * 24 * 60 * MIN }),
      entry({ id: 'file', kind: 'file', preview: 'D:\\a.zip', files: ['D:\\a.zip', 'D:\\b.zip', 'D:\\c.zip'], byteSize: 7, createdAt: now }),
    ]
    const w = mountOverlay()
    await flushMicrotasks()
    const idx = w.findAll('.clip-idx').map((n) => n.text())
    expect(idx.slice(0, 9)).toEqual(['1', '2', '3', '4', '5', '6', '7', '8', '9'])
    expect(idx[9]).toBe('') // 1-9 直选覆盖不到的行不留徽标
    expect(w.findAll('.clip-kind').map((n) => n.text())).toEqual([
      '文', '文', '文', '文', '文', '文', '文', '文', '文', '文', '图', '件',
    ])
    expect(w.findAll('.clip-meta').map((n) => n.text())).toEqual(['1280×720 · 240 KB', '共 3 项'])
    const times = w.findAll('.clip-time').map((n) => n.text())
    expect(times[0]).toBe('5 分钟前')
    expect(times[1]).toBe('50 分钟前') // 去重顶置后展示最近活跃而非首复制时间
    const d3 = new Date(now - 3 * 24 * 60 * MIN)
    const pad = (n: number) => String(n).padStart(2, '0')
    expect(times[10]).toBe(`${pad(d3.getMonth() + 1)}-${pad(d3.getDate())}`) // 超 24h 回落月日
    expect(w.findAll('.clip-preview')[10].text()).toBe('(图片)') // image 无 preview 的兜底行话
    w.unmount()
  })

  it('未知 kind 兜底（L4）：wire 漂移出新类别时显「?」通用字形、tooltip 回显原文，不渲空位', async () => {
    entries = [
      entry({ id: 'k1' }),
      // 编译期联合挡不住服务端版本错位：按未来 wire 实况造型外 kind
      entry({ id: 'k2', kind: 'html' as unknown as ClipEntry['kind'], preview: '<b>x</b>' }),
    ]
    const w = mountOverlay()
    await flushMicrotasks()
    const kinds = w.findAll('.clip-kind')
    expect(kinds[0].text()).toBe('文')
    expect(kinds[0].attributes('title')).toBe('文本')
    expect(kinds[1].text()).toBe('?') // 不是空串：行首图标位永远有得看
    expect(kinds[1].attributes('title')).toContain('html') // 原文 kind 进 tooltip 供排障
    expect(previews(w)).toEqual(['p-k1', '<b>x</b>']) // 兜底不影响该行其余呈现
    w.unmount()
  })

  it('空态两式：无历史一行字；带搜索词无匹配另说', async () => {
    entries = []
    const w = mountOverlay()
    await flushMicrotasks()
    expect(w.find('.clip-empty').text()).toBe('剪贴板历史还是空的')
    await w.find('.clip-search').setValue('nada')
    await flushMicrotasks()
    expect(w.find('.clip-empty').text()).toBe('没有匹配的条目')
    w.unmount()
  })
})

describe('ClipboardOverlay 键盘动线', () => {
  it('opening 事件：清搜索词、重拉全量、重聚焦搜索框、重播 is-enter 入场钩子', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    await w.find('.clip-search').setValue('grep')
    await flushMicrotasks()
    await nextTick()
    expect(w.find('.clip-card').classes()).not.toContain('is-enter')
    const focusSpy = vi.spyOn(w.get('input.clip-search').element as HTMLInputElement, 'focus')

    runtime.handlers['clipboard:overlay:opening']({})
    await flushMicrotasks()
    await nextTick()
    expect((w.find('.clip-search').element as HTMLInputElement).value).toBe('')
    expect(fake.List).toHaveBeenLastCalledWith('', '', 100)
    expect(focusSpy).toHaveBeenCalled()
    expect(w.find('.clip-card').classes()).toContain('is-enter')
    w.unmount()
  })

  it('空搜索词裸数字直选：2 → Set(a2) 成功即 blur 让焦收窗，数字不落进搜索框', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    const ev = press('2')
    await flushMicrotasks()
    expect(ev.defaultPrevented).toBe(true)
    expect(fake.Set).toHaveBeenCalledWith('a2')
    expect(fake.CollapseOverlay).toHaveBeenCalledTimes(1) // 收窗 = 显式 RPC，非 blur 假绿
    expect((w.find('.clip-search').element as HTMLInputElement).value).toBe('')
    w.unmount()
  })

  it('搜索联动：有词时 List 收 q 且行集跟随后端过滤；此时裸数字放行给搜索词、Alt+数字仍直选', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    await w.find('.clip-search').setValue('grep')
    await flushMicrotasks()
    expect(fake.List).toHaveBeenLastCalledWith('grep', '', 100)
    expect(previews(w)).toEqual(['grep -rn foo'])

    const plain = press('1')
    await flushMicrotasks()
    expect(plain.defaultPrevented).toBe(false) // 数字是搜索词的一部分，不劫持
    expect(fake.Set).not.toHaveBeenCalled()

    press('1', { altKey: true })
    await flushMicrotasks()
    expect(fake.Set).toHaveBeenCalledWith('a1')
    expect(fake.CollapseOverlay).toHaveBeenCalledTimes(1) // Alt 直选同样 RPC 收窗
    w.unmount()
  })

  it('↑↓ 移动选中环（首尾夹紧不环绕），Enter 直选环上项', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    expect(w.findAll('.clip-row')[0].classes()).toContain('is-active')
    press('ArrowDown')
    await nextTick()
    expect(w.findAll('.clip-row')[1].classes()).toContain('is-active')
    press('ArrowUp')
    press('ArrowUp')
    await nextTick()
    expect(w.findAll('.clip-row')[0].classes()).toContain('is-active') // 顶行再上不去 = 夹紧
    press('ArrowDown')
    await nextTick()
    press('Enter')
    await flushMicrotasks()
    expect(fake.Set).toHaveBeenCalledWith('a2')
    expect(fake.CollapseOverlay).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('Esc 收窗 = CollapseOverlay 显式 RPC：不收页面自己、不叫 Set、绝不 window.close', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    const ev = press('Escape')
    await flushMicrotasks()
    expect(ev.defaultPrevented).toBe(true)
    expect(fake.CollapseOverlay).toHaveBeenCalledTimes(1)
    expect(fake.Set).not.toHaveBeenCalled()
    expect(w.find('.clip-card').exists()).toBe(true)
    w.unmount()
  })

  it('输入法组字中的数字键只属于选词，不劫持', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    const ev = new KeyboardEvent('keydown', { key: '1', bubbles: true, cancelable: true })
    Object.defineProperty(ev, 'isComposing', { value: true })
    window.dispatchEvent(ev)
    await flushMicrotasks()
    expect(ev.defaultPrevented).toBe(false)
    expect(fake.Set).not.toHaveBeenCalled()
    w.unmount()
  })

  it('IME 三重盾（M1）：事件旗标丢失时 compositionstart 本地旗标仍挡住劫持，end 后恢复直选；Process 键恒放行', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    await w.find('.clip-search').trigger('compositionstart')
    // 模拟部分内核组字中 keydown 不再带 isComposing 的漂移形态：仅剩本地旗标这面盾
    const ev = press('1') // isComposing=false、key 非 Process——只有 composing.value 拦得住
    await flushMicrotasks()
    expect(ev.defaultPrevented).toBe(false)
    expect(fake.Set).not.toHaveBeenCalled()
    expect(fake.CollapseOverlay).not.toHaveBeenCalled()

    const proc = press('Process') // 组字期统一上报形态：恒空转
    expect(proc.defaultPrevented).toBe(false)
    expect(fake.Set).not.toHaveBeenCalled()

    await w.find('.clip-search').trigger('compositionend')
    press('1')
    await flushMicrotasks()
    expect(fake.Set).toHaveBeenCalledWith('a1') // 上屏后劫持恢复
    w.unmount()
  })

  it('第 9 行外的数字键与空列表的 Enter 均为无害空转', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    press('9') // 只有 3 行
    await flushMicrotasks()
    expect(fake.Set).not.toHaveBeenCalled()
    entries = []
    await w.find('.clip-search').setValue('zzz')
    await flushMicrotasks()
    press('Enter')
    await flushMicrotasks()
    expect(fake.Set).not.toHaveBeenCalled()
    w.unmount()
  })
})

describe('ClipboardOverlay 鼠标快操作', () => {
  it('点击行 = Set + RPC 收窗；复制钮 = Set 不关窗给轻提示', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    await w.findAll('.clip-row')[2].trigger('click')
    await flushMicrotasks()
    expect(fake.Set).toHaveBeenCalledWith('a3')
    expect(fake.CollapseOverlay).toHaveBeenCalledTimes(1)

    await w.findAll('.clip-act')[1].trigger('click') // 第 0 行「钉」「复」中的「复」
    await flushMicrotasks()
    expect(fake.Set).toHaveBeenCalledWith('a1')
    expect(fake.CollapseOverlay).toHaveBeenCalledTimes(1) // 复制不追加收窗
    expect(w.find('.clip-tip').text()).toContain('已复制，浮层不关')
    expect(w.find('.clip-tip').classes()).toContain('tip-ok')
    w.unmount()
  })

  it('置顶钮：TogglePin 就地回排升组首挂「顶」徽标不关窗（CollapseOverlay 零调用）；再点取消回落原位', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    await w.findAll('.clip-act')[4].trigger('click') // 第 2 行 a3 的「钉」
    await flushMicrotasks()
    expect(fake.TogglePin).toHaveBeenCalledWith('a3')
    expect(previews(w)).toEqual(['win11 更新说明', 'grep -rn foo', 'https://example.com/x'])
    expect(w.findAll('.clip-row')[0].find('.clip-badge').text()).toBe('顶')
    expect(w.find('.clip-tip').text()).toBe('已置顶')
    expect(fake.CollapseOverlay).not.toHaveBeenCalled()

    await w.findAll('.clip-act')[0].trigger('click') // 回到组首的 a3 再点「解」
    await flushMicrotasks()
    expect(w.find('.clip-tip').text()).toBe('已取消置顶')
    expect(previews(w)).toEqual(['grep -rn foo', 'https://example.com/x', 'win11 更新说明'])
    w.unmount()
  })

  it('行 hover 同步选中环（Enter 语义与所见一致）', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    await w.findAll('.clip-row')[2].trigger('mouseenter')
    await nextTick()
    expect(w.findAll('.clip-row')[2].classes()).toContain('is-active')
    expect(w.findAll('.clip-row')[0].classes()).not.toContain('is-active')
    w.unmount()
  })
})

describe('ClipboardOverlay 状态与事件流', () => {
  it('paused 角标双喂：GetStatus 初始态 + clipboard:paused 事件即时翻牌不等重拉', async () => {
    statusSeed = { paused: true }
    const w = mountOverlay()
    await flushMicrotasks()
    expect(w.find('.clip-paused').text()).toBe('记录已暂停')
    runtime.handlers['clipboard:paused']({ data: { paused: false } })
    await nextTick()
    expect(w.find('.clip-paused').exists()).toBe(false)
    runtime.handlers['clipboard:paused']({ data: { paused: true } })
    await nextTick()
    expect(w.find('.clip-paused').exists()).toBe(true)
    w.unmount()
  })

  it('clipboard:updated：空词乐观顶置去重（同 id 重发只挪不 dup），带词改为静默重拉', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    runtime.handlers['clipboard:updated']({ data: entry({ id: 'a2', preview: 'https://example.com/x' }) })
    await nextTick()
    expect(previews(w)).toEqual(['https://example.com/x', 'grep -rn foo', 'win11 更新说明'])
    runtime.handlers['clipboard:updated']({ data: entry({ id: 'z9', preview: '新条目' }) })
    await nextTick()
    expect(previews(w)[0]).toBe('新条目')
    expect(w.findAll('.clip-row').length).toBe(4)
    expect(fake.List).toHaveBeenCalledTimes(1) // 空词两次 updated 都是本地合流零重拉

    await w.find('.clip-search').setValue('win')
    await flushMicrotasks()
    runtime.handlers['clipboard:updated']({ data: entry({ id: 'z8', preview: 'win11 补丁' }) })
    await flushMicrotasks()
    expect(fake.List).toHaveBeenCalledTimes(3) // 挂载 + 搜索词 + 带词重拉各一次
    expect(previews(w)).toEqual(['win11 更新说明']) // 新条目是否命中由后端裁，本地不瞎插
    w.unmount()
  })

  it('clipboard:removed 按 id 摘行；畸形载荷忽略', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    runtime.handlers['clipboard:removed']({ data: { id: 'a2' } })
    runtime.handlers['clipboard:removed']({ data: undefined })
    runtime.handlers['clipboard:removed']({})
    await nextTick()
    expect(previews(w)).toEqual(['grep -rn foo', 'win11 更新说明'])
    w.unmount()
  })
})

describe('ClipboardOverlay 错误态（不白屏纪律）', () => {
  it('Set 失败保窗：轻提示中文报错、不请藏窗（换一条还能重试）', async () => {
    setErr = new Error('disk locked')
    const w = mountOverlay()
    await flushMicrotasks()
    await w.findAll('.clip-row')[0].trigger('click')
    await flushMicrotasks()
    expect(fake.CollapseOverlay).not.toHaveBeenCalled()
    expect(w.find('.clip-tip').text()).toContain('回填失败: disk locked')
    expect(w.find('.clip-tip').classes()).toContain('tip-err')
    expect(w.findAll('.clip-row').length).toBe(3)
    w.unmount()
  })

  it('adapter 未接线（transport 为 null）：中文可读错误进浮层错误条，卡体与搜索框照常；恢复接线后 opening 自愈', async () => {
    setClipboardTransport(null)
    const w = mountOverlay()
    await flushMicrotasks()
    const bar = w.find('.clip-errbar')
    expect(bar.exists()).toBe(true)
    expect(bar.attributes('role')).toBe('alert')
    expect(bar.text()).toContain('剪贴板服务不可用')
    expect(bar.text()).toContain('剪贴板服务尚未接线') // adapter need() 的中文原话，不白屏
    expect(w.find('.clip-card').exists()).toBe(true)
    expect(w.find('.clip-search').exists()).toBe(true)

    setClipboardTransport(fake)
    runtime.handlers['clipboard:overlay:opening']({})
    await flushMicrotasks()
    expect(w.find('.clip-errbar').exists()).toBe(false)
    expect(previews(w)).toEqual(['grep -rn foo', 'https://example.com/x', 'win11 更新说明'])
    w.unmount()
  })

  it('刷新失败不掀已用列表：带词重拉炸只给小字轻提示，错误条不上台', async () => {
    const w = mountOverlay()
    await flushMicrotasks()
    await w.find('.clip-search').setValue('win')
    await flushMicrotasks()
    expect(previews(w)).toEqual(['win11 更新说明'])
    listErr = new Error('ipc timeout')
    runtime.handlers['clipboard:updated']({ data: entry({ id: 'q9' }) })
    await flushMicrotasks()
    expect(previews(w)).toEqual(['win11 更新说明'])
    expect(w.find('.clip-errbar').exists()).toBe(false)
    expect(w.find('.clip-tip').text()).toContain('刷新失败: ipc timeout')
    w.unmount()
  })

  it('GetStatus 失败同口径：空账上错误条、有账只轻提示不掀桌', async () => {
    entries = []
    statusErr = new Error('status ipc dead')
    const w = mountOverlay()
    await flushMicrotasks()
    expect(w.find('.clip-errbar').text()).toContain('status ipc dead')
    w.unmount()

    entries = [entry({ id: 'solo', preview: '已有在屏账' })]
    statusErr = null
    const w2 = mountOverlay()
    await flushMicrotasks()
    expect(previews(w2)).toEqual(['已有在屏账'])
    statusErr = new Error('late status dead')
    runtime.handlers['clipboard:overlay:opening']({}) // 先拉列表（成）再拉状态（炸），顺序确定
    await flushMicrotasks()
    expect(w2.find('.clip-errbar').exists()).toBe(false)
    expect(w2.find('.clip-tip').text()).toContain('刷新失败: late status dead')
    w2.unmount()
  })
})
