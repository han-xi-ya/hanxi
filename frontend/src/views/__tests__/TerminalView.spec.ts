// 特征测试：TerminalView + useTerminal（内嵌终端 v0 前端竖切）。
// 后端契约事实（主会话按此实现）钉在本测试里的部分：
//  - RPC（terminalservice 函数式导出）：OpenTerminal(profileId, cols, rows)→TermSessionRow、
//    ListTerminals()、RenameTerminal(id,title)、KillTerminal(id)（杀会话并除账）；
//    profile 固定三档 cmd/powershell/claude（展示名 命令行/PowerShell/Claude Code）。
//  - 字节通道：Stream("terminal:io")，每终端页一条连接；**首字节标签 0x00=JSON 控制帧、
//    0x01=原始终端字节**。连接建立（onopen）后客户端先发 {"op":"open","sessionId"}；
//    服务端回 {"op":"ready"} 后 pty 输出以 0x01 帧推送；子进程退出推 {"op":"exit","code":N}；
//    输入 0x01 帧上行、resize 0x00 帧 {"op":"resize","cols","rows"}。
//  - v0 语义：页卸载/切会话只断通道不杀进程（KillTerminal 永不隐式调用）；断线自动
//    Stream() 重开并重发 open 帧；exit 后不再自动重连。
// 打桩形制：绑定 vi.mock（missingBindingSeam 为未落盘生成物解析空壳，AiChat/JsonFmt
// spec 头注在册同一条路）；@wailsio/runtime 的 Stream 用自造可控 FakeSocket（h.sockets
// 收编）；@xterm/xterm 与四个 addon 整体替换为可记桩（h.terms 收编：写序/reset/focus
// 全是可断言账目）；useConfirm/usePrompt hoisted mock（JsonFmtView.spec 同法）。
// fake timers 下排干 await 一律 flushMicrotasks（JsonFmtView.spec 头注纪律）；
// 组件统一经 mountView 登记、afterEach 兜底卸载——**残件带重连定时器会活到下一用例**，
// 这是本套件比 AiChat 多一道收口的原因（重连退避是本模块的一等公民）。
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import TerminalView from '../TerminalView.vue'
import {
  decodeFrame,
  encodeControlFrame,
  encodeDataFrame,
  FRAME_CONTROL,
  FRAME_DATA,
  MAX_RECONNECT,
  READY_TIMEOUT_MS,
  RESIZE_DEBOUNCE_MS,
} from '../../composables/useTerminal'

// —— 收编仓库（mock 工厂与测试共用；实例经桩类型别后取用）——
const h = vi.hoisted(() => ({
  sockets: [] as unknown[],
  terms: [] as unknown[],
}))

const svc = vi.hoisted(() => ({
  OpenTerminal: vi.fn(),
  ListTerminals: vi.fn(),
  RenameTerminal: vi.fn(),
  KillTerminal: vi.fn(),
}))
vi.mock('../../../bindings/hanxi/internal/modules/terminal/terminalservice', () => svc)

vi.mock('@wailsio/runtime', () => {
  class FakeSocket {
    name: string
    readyState = 0 // CONNECTING（真 WailsSocket 同形：open 前不得发帧）
    binaryType = 'blob' // 故意装成未设置态：验 composable 确实按契约钉 arraybuffer
    sent: Uint8Array[] = []
    closed = false
    onopen: (() => void) | null = null
    onmessage: ((ev: { data: unknown }) => void) | null = null
    onclose: (() => void) | null = null
    onerror: (() => void) | null = null
    constructor(name: string) { this.name = name }
    send(data: Uint8Array): void { this.sent.push(data) }
    close(): void { this.closed = true; this.readyState = 3 }
    // —— 测试播入口 ——
    fireOpen(): void { this.readyState = 1; this.onopen?.() }
    fireMessage(payload: ArrayBuffer): void { this.onmessage?.({ data: payload }) }
    fireClose(): void { this.readyState = 3; this.onclose?.() }
    fireError(): void { this.onerror?.() }
  }
  return {
    Stream: (name: string) => {
      const s = new FakeSocket(name)
      h.sockets.push(s)
      return s
    },
    Browser: { OpenURL: vi.fn() },
  }
})

vi.mock('@xterm/xterm', () => ({
  Terminal: class FakeTerminal {
    options: Record<string, unknown>
    writes: Array<string | Uint8Array> = []
    addons: unknown[] = []
    search: { nextQ: string[]; prevQ: string[] } | null = null
    cols = 80
    rows = 24
    resets = 0
    focusCount = 0
    disposed = false
    onDataCb: ((s: string) => void) | null = null
    onResizeCb: ((d: { cols: number; rows: number }) => void) | null = null
    // 复刻真 xterm 6.0.0 的 proposed 门禁：unicode.activeVersion 未开
    // allowProposedApi 即抛（真机首跑炸过整模块渲染，宽容桩正是漏雷根因）。
    #unicode = { activeVersion: '' }
    get unicode(): { activeVersion: string } {
      if (this.options.allowProposedApi !== true) {
        throw new Error('You must set the allowProposedApi option to true to use proposed API')
      }
      return this.#unicode
    }
    constructor(opts: Record<string, unknown>) {
      this.options = { ...opts }
      h.terms.push(this)
    }
    loadAddon(a: unknown): void {
      this.addons.push(a)
      const maybe = a as { findNext?: unknown }
      if (maybe && typeof maybe.findNext === 'function') {
        this.search = a as { nextQ: string[]; prevQ: string[] }
      }
    }
    open(): void { /* happy-dom 无布局；真 fit 在 mountTerminal 里由 try/catch 兜住 */ }
    write(data: string | Uint8Array): void { this.writes.push(data) }
    reset(): void { this.resets++ }
    focus(): void { this.focusCount++ }
    resize(cols: number, rows: number): void {
      this.cols = cols; this.rows = rows
      this.onResizeCb?.({ cols, rows })
    }
    onData(cb: (s: string) => void): { dispose(): void } { this.onDataCb = cb; return { dispose() {} } }
    onResize(cb: (d: { cols: number; rows: number }) => void): { dispose(): void } { this.onResizeCb = cb; return { dispose() {} } }
    attachCustomKeyEventHandler(_cb: (e: KeyboardEvent) => boolean): void { /* 收集但不断言：Ctrl+F 行为由搜索条用例经 UI 路径覆盖 */ }
    dispose(): void { this.disposed = true }
    // —— 测试播入口 ——
    fireData(text: string): void { this.onDataCb?.(text) }
  },
}))

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class { fitCount = 0; activate(): void {} dispose(): void {} fit(): void { this.fitCount++ } },
}))
vi.mock('@xterm/addon-search', () => ({
  SearchAddon: class {
    nextQ: string[] = []
    prevQ: string[] = []
    activate(): void {} dispose(): void {}
    findNext(q: string): void { this.nextQ.push(q) }
    findPrevious(q: string): void { this.prevQ.push(q) }
  },
}))
vi.mock('@xterm/addon-unicode11', () => ({
  Unicode11Addon: class { activate(): void {} dispose(): void {} },
}))
vi.mock('@xterm/addon-web-links', () => ({
  WebLinksAddon: class {
    constructor(public handler: unknown) {}
    activate(): void {} dispose(): void {}
  },
}))

const confirmBox = vi.hoisted(() => ({ confirm: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => confirmBox }))

const promptBox = vi.hoisted(() => ({ prompt: vi.fn() }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => promptBox }))

// —— 桩实例的类型别名（收编数组按 unknown 存，测试侧按形制取）——
type SocketStub = {
  name: string
  sent: Uint8Array[]
  closed: boolean
  binaryType: string
  fireOpen(): void
  fireMessage(p: ArrayBuffer): void
  fireClose(): void
  fireError(): void
}
type TermStub = {
  writes: Array<string | Uint8Array>
  addons: unknown[]
  search: { nextQ: string[]; prevQ: string[] } | null
  unicode: { activeVersion: string }
  options: Record<string, unknown>
  resets: number
  focusCount: number
  disposed: boolean
  fireData(s: string): void
  resize(c: number, r: number): void
}

function socket(i: number): SocketStub { return h.sockets[i] as SocketStub }
function lastSocket(): SocketStub { return h.sockets[h.sockets.length - 1] as SocketStub }
function term(): TermStub { return h.terms[0] as TermStub }

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

// —— 帧构造小工具（ArrayBuffer 形态保真：真 ev.data 就是 ArrayBuffer）——
// 刚 new 出来的 Uint8Array，buffer 必为 ArrayBuffer（TS 5.9 lib 的 ArrayBufferLike 联合
// 含 SharedArrayBuffer，这里按事实收窄，不是强转糊弄）
function controlPayload(obj: Record<string, unknown>): ArrayBuffer {
  return encodeControlFrame(obj).buffer as ArrayBuffer
}
function dataPayload(text: string): ArrayBuffer {
  return encodeDataFrame(text).buffer as ArrayBuffer
}
function decodeBytes(bytes: Uint8Array): string {
  return new TextDecoder().decode(bytes)
}
type Frame = ReturnType<typeof decodeFrame>
function sentFrames(s: { sent: Uint8Array[] }): Frame[] {
  return s.sent.map((f) => decodeFrame(f))
}
function writeAt(t: TermStub, i: number): string {
  const w = t.writes[i]
  return typeof w === 'string' ? w : decodeBytes(w)
}

// —— 后端形状工厂（TermSessionRow{id,title,profileId,pid,createdAt,alive}）——
let seq = 0
function makeWireRow(over: Record<string, unknown> = {}) {
  return {
    id: `t${++seq}`,
    title: '命令行',
    profileId: 'cmd',
    pid: 4000 + seq,
    createdAt: 1730000000000 + seq,
    alive: true,
    ...over,
  }
}

// 组件统一登记卸载：残件的重连退避定时器不得活到下一用例（本套件头注纪律）
let current: VueWrapper | null = null

async function mountView(rows: ReturnType<typeof makeWireRow>[] = []) {
  svc.ListTerminals.mockResolvedValue(rows)
  const w = mount(TerminalView, { attachTo: document.body })
  current = w
  await flushMicrotasks()
  await nextTick()
  return w
}

/** 装配并跑到 live：名录→自动接入首会话→onopen→ready。前提 rows 首项存活。 */
async function mountLive(rows: ReturnType<typeof makeWireRow>[]) {
  const w = await mountView(rows)
  lastSocket().fireOpen()
  lastSocket().fireMessage(controlPayload({ op: 'ready' }))
  await nextTick()
  return w
}

async function openMenu(w: VueWrapper) {
  await w.find('.tc-create').trigger('click')
  await nextTick()
}

beforeEach(() => {
  seq = 0
  h.sockets.length = 0
  h.terms.length = 0
  current = null
  confirmBox.confirm.mockResolvedValue(true)
  promptBox.prompt.mockResolvedValue(null)
  svc.ListTerminals.mockResolvedValue([])
  svc.OpenTerminal.mockImplementation((profileId: string) => Promise.resolve(
    makeWireRow({ id: `new${++seq}`, profileId, title: '新终端' }),
  ))
  svc.RenameTerminal.mockResolvedValue(undefined)
  svc.KillTerminal.mockResolvedValue(undefined)
})

afterEach(() => {
  current?.unmount()
  current = null
  vi.clearAllMocks()
  vi.restoreAllMocks()
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('TerminalView 标签帧编解码（0x00/0x01 混帧不错序）', () => {
  it('控制帧往返：标签 0x00+JSON 字段逐字保留；数据帧标签 0x01+UTF-8 一字不差（多字节中文 é 不位移）', () => {
    const ctl = encodeControlFrame({ op: 'resize', cols: 132, rows: 43 })
    expect(ctl[0]).toBe(FRAME_CONTROL)
    expect(decodeFrame(ctl.buffer)).toMatchObject({ kind: 'control', op: 'resize', cols: 132, rows: 43 })
    const data = encodeDataFrame('ls 目录 é\r\n')
    expect(data[0]).toBe(FRAME_DATA)
    const decoded = decodeFrame(data.buffer)
    expect(decoded?.kind === 'data' && decodeBytes(decoded.bytes)).toBe('ls 目录 é\r\n')
    // 数据帧体必须是纯 UTF-8：0x01 之后零包装（契约"原始终端字节"）
    expect(data.subarray(1)).toEqual(new TextEncoder().encode('ls 目录 é\r\n'))
  })

  it('畸形帧整体丢弃不吃字节：空帧/未知标签/坏 JSON/无 op 的 JSON 一律 null（宁可丢一帧不喂垃圾给渲染器）', () => {
    expect(decodeFrame(new Uint8Array(0).buffer)).toBeNull()
    expect(decodeFrame(new Uint8Array([0x7f, 1, 2, 3]).buffer)).toBeNull()
    expect(decodeFrame(new Uint8Array([FRAME_CONTROL, ...new TextEncoder().encode('{坏')]).buffer)).toBeNull()
    expect(decodeFrame(new Uint8Array([FRAME_CONTROL, ...new TextEncoder().encode('{"x":1}')]).buffer)).toBeNull()
    expect(decodeFrame('不是缓冲')).toBeNull()
  })

  it('入站混帧不错序：data→未知 control→data 三帧，渲染器按到达序收到 a、b，未知 op 静默不弹错', async () => {
    const w = await mountLive([makeWireRow()])
    const t = term()
    const base = t.writes.length
    lastSocket().fireMessage(dataPayload('a'))
    lastSocket().fireMessage(controlPayload({ op: 'ping', unknown: true }))
    lastSocket().fireMessage(dataPayload('b'))
    await nextTick()
    expect(writeAt(t, base)).toBe('a')
    expect(writeAt(t, base + 1)).toBe('b')
    expect(w.find('.tm-banner').exists()).toBe(false)
    expect(w.find('.tc-slot-current .chip').text()).toBe('已连接')
  })

  it('composable 按契约钉 binaryType=arraybuffer 与流名 terminal:io（真 runtime 默认即 arraybuffer，这里防实现漂移）', async () => {
    await mountLive([makeWireRow()])
    expect(socket(0).binaryType).toBe('arraybuffer')
    expect(socket(0).name).toBe('terminal:io')
  })
})

describe('TerminalView 通道握手、重连与 v0 进程语义', () => {
  it('有名录挂载即自动接入首会话：连接未建立零帧抢发，onopen 后首帧即 {"op":"open","sessionId"}', async () => {
    const w = await mountView([makeWireRow({ id: 'z1', title: '我的终端' })])
    expect(h.sockets).toHaveLength(1)
    expect(socket(0).sent).toHaveLength(0) // open 帧只在连接建立后发（契约顺序）
    expect(w.find('.tc-slot-current .chip').text()).toBe('连接中')
    socket(0).fireOpen()
    await nextTick()
    expect(w.find('.tc-slot-current .chip').text()).toBe('握手中')
    expect(sentFrames(socket(0))).toEqual([
      { kind: 'control', op: 'open', sessionId: 'z1' },
    ])
  })

  it('ready 才算活：聚焦终端、尺寸对账（账本无尺寸记录的会话 ready 后补一发 resize 把 pty 拽齐）', async () => {
    const w = await mountLive([makeWireRow({ id: 'z2' })])
    const frames = sentFrames(socket(0))
    expect(frames[0]).toMatchObject({ op: 'open' })
    expect(frames[1]).toMatchObject({ kind: 'control', op: 'resize', cols: 80, rows: 24 })
    expect(term().focusCount).toBe(1)
    expect(w.find('.tm-note').text()).toContain('显示通道在线')
  })

  it('resize 防抖 150ms 只发末值：连改三次到点前零帧、到点只一发 cols=20 rows=4（拖窗口不轰炸）', async () => {
    vi.useFakeTimers()
    await mountLive([makeWireRow()])
    const s = socket(0)
    const before = s.sent.length
    term().resize(10, 2)
    term().resize(12, 3)
    term().resize(20, 4)
    expect(s.sent.length).toBe(before) // 帧前不可见
    vi.advanceTimersByTime(RESIZE_DEBOUNCE_MS)
    const fresh = sentFrames(s).slice(before)
    expect(fresh).toHaveLength(1) // 只发末值
    expect(fresh[0]).toMatchObject({ kind: 'control', op: 'resize', cols: 20, rows: 4 })
  })

  it('exit 帧渲染在屏尾（排在已收字节之后不错序），进程死则不再自动重连，chip 报「已退出(3)」', async () => {
    vi.useFakeTimers()
    const w = await mountLive([makeWireRow()])
    const s = socket(0)
    const t = term()
    s.fireMessage(dataPayload('exit\r'))
    s.fireMessage(controlPayload({ op: 'exit', code: 3 }))
    await nextTick()
    expect(writeAt(t, t.writes.length - 1)).toContain('进程已退出')
    expect(writeAt(t, t.writes.length - 1)).toContain('3')
    expect(s.closed).toBe(true)
    expect(w.find('.tc-slot-current .chip').text()).toBe('已退出(3)')
    // 退出的进程续它没有意义：把退避钟整烧完也不许冒出新连接
    vi.advanceTimersByTime(60_000)
    expect(h.sockets).toHaveLength(1)
    expect(w.find('.tm-reconnect').exists()).toBe(false)
    expect(w.find('.tm-note').text()).toContain('Kill 可清账')
  })

  it('断线自动重连重发 open 帧：退避 1s 后 Stream() 重开，画面不清不重放（reset 只属主动切会话）', async () => {
    vi.useFakeTimers()
    const w = await mountLive([makeWireRow({ id: 'r1' })])
    const t = term()
    socket(0).fireMessage(dataPayload('断线前已渲染'))
    const writesBefore = t.writes.length
    socket(0).fireClose()
    await nextTick()
    expect(w.find('.tc-slot-current .chip').text()).toBe('重连中')
    expect(w.find('.tm-note').text()).toContain('第 1/4 次')
    expect(w.find('.tm-note').text()).toContain('后端进程仍在运行')
    vi.advanceTimersByTime(1000)
    expect(h.sockets).toHaveLength(2)
    expect(lastSocket().sent).toHaveLength(0) // 新连接同样等 onopen 才发 open
    lastSocket().fireOpen()
    expect(sentFrames(lastSocket())[0]).toMatchObject({ kind: 'control', op: 'open', sessionId: 'r1' })
    lastSocket().fireMessage(controlPayload({ op: 'ready' }))
    await nextTick()
    // 不砸画面：无 reset、旧字节原样在场、重连不重放旧帧
    expect(t.resets).toBe(0)
    expect(t.writes.length).toBe(writesBefore)
    expect(writeAt(t, writesBefore - 1)).toBe('断线前已渲染')
    expect(w.find('.tc-slot-current .chip').text()).toBe('已连接')
  })

  it('重连预算用尽显式报（退避 1/2/4/8s 全灭后「已断线」带原因），「立即重连」重置预算重走握手', async () => {
    vi.useFakeTimers()
    const w = await mountLive([makeWireRow()])
    socket(0).fireClose()
    for (const delay of [1000, 2000, 4000, 8000]) {
      vi.advanceTimersByTime(delay)
      lastSocket().fireError() // 混用 error 通道验证 onerror 与 onclose 同路计数
    }
    expect(h.sockets).toHaveLength(MAX_RECONNECT + 1)
    await nextTick()
    expect(w.find('.tc-slot-current .chip').text()).toBe('已断线')
    expect(w.find('.tm-note').text()).toContain(`自动重连 ${MAX_RECONNECT} 次未果`)
    expect(w.find('.tm-note').text()).toContain('传输层报错')
    const btn = w.find('.tm-reconnect')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(h.sockets).toHaveLength(MAX_RECONNECT + 2)
    lastSocket().fireOpen()
    lastSocket().fireMessage(controlPayload({ op: 'ready' }))
    await nextTick()
    expect(w.find('.tc-slot-current .chip').text()).toBe('已连接')
  })

  it('ready 超时视作一次断线入重连通道（不永远挂着「握手中」骗用户）', async () => {
    vi.useFakeTimers()
    const w = await mountView([makeWireRow({ id: 'rt' })])
    socket(0).fireOpen()
    await nextTick()
    expect(w.find('.tc-slot-current .chip').text()).toBe('握手中')
    vi.advanceTimersByTime(READY_TIMEOUT_MS)
    await nextTick()
    expect(w.find('.tc-slot-current .chip').text()).toBe('重连中')
    expect(w.find('.tm-note').text()).toContain('握手超时')
    vi.advanceTimersByTime(1000)
    expect(h.sockets).toHaveLength(2)
    lastSocket().fireOpen()
    expect(sentFrames(lastSocket())[0]).toMatchObject({ op: 'open', sessionId: 'rt' })
  })

  it('切换会话不销毁后端进程：KillTerminal 零调用，旧连接 closed，画面清恰好一次（v0 连接即断）', async () => {
    const w = await mountLive([
      makeWireRow({ id: 'a', title: '会话A' }),
      makeWireRow({ id: 'b', title: '会话B' }),
    ])
    await w.findAll('.tc-chip')[1].trigger('click')
    await nextTick()
    expect(socket(0).closed).toBe(true)
    expect(h.sockets).toHaveLength(2)
    expect(svc.KillTerminal).not.toHaveBeenCalled()
    expect(term().resets).toBe(1) // 切会话=换画面归属，恰好一次
    expect(w.find('.tc-slot-current .tc-title').text()).toBe('会话B')
  })

  it('页卸载只断通道不杀进程：socket closed、term disposed，全程零 KillTerminal', async () => {
    const w = await mountLive([makeWireRow()])
    w.unmount()
    current = null
    expect(socket(0).closed).toBe(true)
    expect(term().disposed).toBe(true)
    expect(svc.KillTerminal).not.toHaveBeenCalled()
  })
})

describe('TerminalView 名录三通道与入口', () => {
  it('新建下拉三档固定（命令行/PowerShell/Claude Code），逐档 OpenTerminal(profileId, cols, rows) 且建号会话 ready 后不再补发尺寸帧', async () => {
    const w = await mountView([])
    const expected: Array<[number, string, string]> = [
      [0, 'cmd', '命令行'],
      [1, 'powershell', 'PowerShell'],
      [2, 'claude', 'Claude Code'],
    ]
    for (const [idx, id, label] of expected) {
      svc.OpenTerminal.mockImplementationOnce((profileId: string) => Promise.resolve(
        makeWireRow({ id: `p${id}`, profileId, title: label }),
      ))
      await openMenu(w)
      const items = w.findAll('.tc-menu-item')
      expect(items.map((n) => n.text())).toEqual(['命令行', 'PowerShell', 'Claude Code'])
      await items[idx].trigger('click')
      await flushMicrotasks()
      expect(svc.OpenTerminal).toHaveBeenLastCalledWith(id, 80, 24) // 建号即带当前尺寸（fake term 80×24）
      expect(lastSocket().sent).toHaveLength(0) // 新会话等 onopen
      lastSocket().fireOpen()
      lastSocket().fireMessage(controlPayload({ op: 'ready' }))
      await nextTick()
      // 建号已把尺寸交给后端（RPC 参数），ready 对账无欠账 → 通道里只有 open 帧
      expect(sentFrames(lastSocket())).toHaveLength(1)
    }
    expect(svc.OpenTerminal).toHaveBeenCalledTimes(3)
    expect(w.findAll('.tc-title').map((n) => n.text())).toEqual(['命令行', 'PowerShell', 'Claude Code'])
  })

  it('新建在途连点只许一发（钮禁用+creating 双闸），落账挂尾并自动接入', async () => {
    const w = await mountView([])
    const d = deferred<ReturnType<typeof makeWireRow>>()
    svc.OpenTerminal.mockReturnValueOnce(d.promise)
    await openMenu(w)
    await w.findAll('.tc-menu-item')[0].trigger('click')
    await flushMicrotasks()
    expect(svc.OpenTerminal).toHaveBeenCalledTimes(1)
    // creating 期间新建钮禁用（trigger 对 disabled 不发事件→菜单根本开不了）；
    // 即便绕过视图直调，composable 的 creating 闸也在（双闸，照 aiAsking 先例）
    expect(w.find('.tc-create').attributes('disabled')).toBeDefined()
    expect(w.find('.tc-create').text()).toContain('创建中')
    await openMenu(w)
    expect(w.findAll('.tc-menu-item')).toHaveLength(0)
    await flushMicrotasks()
    expect(svc.OpenTerminal).toHaveBeenCalledTimes(1)
    d.resolve(makeWireRow({ id: 'cr1', title: '新开的' }))
    await flushMicrotasks()
    expect(w.findAll('.tc-title').map((n) => n.text())).toEqual(['新开的'])
    expect(h.sockets).toHaveLength(1)
  })

  it('OpenTerminal 抛只砸 openError 通道：红条在场、已有会话画面与连接一条不碰，可点「知道了」收掉', async () => {
    const w = await mountLive([makeWireRow({ id: 'keep' })])
    const t = term()
    socket(0).fireMessage(dataPayload('手里的输出'))
    svc.OpenTerminal.mockRejectedValueOnce(new Error('spawn 失败：找不到 powershell'))
    await openMenu(w)
    await w.findAll('.tc-menu-item')[1].trigger('click')
    await flushMicrotasks()
    expect(w.find('.tm-banner').text()).toContain('新建终端失败')
    expect(w.find('.tm-banner').text()).toContain('找不到 powershell')
    expect(writeAt(t, t.writes.length - 1)).toBe('手里的输出')
    expect(w.find('.tc-slot-current .chip').text()).toBe('已连接')
    expect(h.sockets).toHaveLength(1) // 没为失败的建号拉新连接
    await w.find('.tm-banner .btn').trigger('click')
    expect(w.find('.tm-banner').exists()).toBe(false)
  })

  it('名录刷新失败走 listError 横幅（名录在场才挂横幅，空名录由覆盖层接手），当前通道继续渲染', async () => {
    const w = await mountLive([makeWireRow()])
    svc.ListTerminals.mockRejectedValueOnce(new Error('后端模块未启动'))
    await w.find('.tc-refresh').trigger('click')
    await flushMicrotasks()
    expect(w.find('.tm-banner').text()).toContain('名录刷新失败')
    expect(w.find('.tm-banner').text()).toContain('后端模块未启动')
    const t = term()
    socket(0).fireMessage(dataPayload('照常在流'))
    expect(writeAt(t, t.writes.length - 1)).toBe('照常在流')
  })

  it('名录连发两发晚到的旧一代不得整表回灌（reqSeq 门禁，PortScan/AiChat 同族）', async () => {
    const w = await mountLive([makeWireRow({ id: 'm1', title: '初始' })])
    const d1 = deferred<ReturnType<typeof makeWireRow>[]>()
    const d2 = deferred<ReturnType<typeof makeWireRow>[]>()
    svc.ListTerminals.mockReturnValueOnce(d1.promise).mockReturnValueOnce(d2.promise)
    await w.find('.tc-refresh').trigger('click')
    await w.find('.tc-refresh').trigger('click')
    d2.resolve([makeWireRow({ id: 'm1', title: '新一代' })])
    await flushMicrotasks()
    d1.resolve([makeWireRow({ id: 'm1', title: '旧一代' }), makeWireRow({ id: 'ghost', title: '幽灵' })])
    await flushMicrotasks()
    expect(w.findAll('.tc-title').map((n) => n.text())).toEqual(['新一代'])
  })

  it('改名走 usePrompt：trim 后落账不整页重拉；取消(null)与空串都不发 RPC', async () => {
    const w = await mountLive([makeWireRow({ id: 'r9', title: '旧名' })])
    const listCalls = svc.ListTerminals.mock.calls.length
    promptBox.prompt.mockResolvedValueOnce('  新名  ')
    await w.find('.tc-rename').trigger('click')
    await flushMicrotasks()
    expect(svc.RenameTerminal).toHaveBeenCalledWith('r9', '新名')
    expect(w.find('.tc-title').text()).toBe('新名')
    expect(svc.ListTerminals).toHaveBeenCalledTimes(listCalls) // 就地改题，不重拉名录
    promptBox.prompt.mockResolvedValueOnce(null)
    await w.find('.tc-rename').trigger('click')
    await flushMicrotasks()
    promptBox.prompt.mockResolvedValueOnce('  ')
    await w.find('.tc-rename').trigger('click')
    await flushMicrotasks()
    expect(svc.RenameTerminal).toHaveBeenCalledTimes(1)
  })

  it('Kill 先过危险确认：取消零 RPC 通道不动；确认框待决连点只一发；确认后杀会话除账、选中回落前一项', async () => {
    const w = await mountLive([
      makeWireRow({ id: 'k1', title: '一' }),
      makeWireRow({ id: 'k2', title: '二' }),
    ])
    await w.findAll('.tc-chip')[1].trigger('click') // 选中 k2（socket1 在建）
    await nextTick()
    expect(h.sockets).toHaveLength(2)
    confirmBox.confirm.mockResolvedValueOnce(false)
    await w.findAll('.tc-kill')[1].trigger('click')
    await flushMicrotasks()
    expect(svc.KillTerminal).not.toHaveBeenCalled()
    expect(h.sockets).toHaveLength(2)
    expect(socket(1).closed).toBe(false) // 取消：当前通道原样
    // 待决期间连点：asking 先闸后问，confirm 只多调一次
    const d = deferred<boolean>()
    confirmBox.confirm.mockReturnValueOnce(d.promise)
    await w.findAll('.tc-kill')[1].trigger('click')
    await w.findAll('.tc-kill')[1].trigger('click')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(2) // 1 取消 + 1 待决（第二点被闸挡）
    d.resolve(true)
    await flushMicrotasks()
    expect(svc.KillTerminal).toHaveBeenCalledTimes(1)
    expect(svc.KillTerminal).toHaveBeenCalledWith('k2')
    expect(w.findAll('.tc-title').map((n) => n.text())).toEqual(['一'])
    // 选中回落被删项前一项 k1 → 旧通道 closed、自动接入新通道
    expect(socket(1).closed).toBe(true)
    expect(h.sockets).toHaveLength(3)
  })

  it('Kill 危险确认文案说出「杀会话并除账」与不可恢复（tone=danger，破坏性动作分离+确认）', async () => {
    const w = await mountLive([makeWireRow({ id: 'kt' })])
    await w.find('.tc-kill').trigger('click')
    await flushMicrotasks()
    const opts = confirmBox.confirm.mock.calls[0][0] as { title: string; description: string; tone: string; confirmLabel: string }
    expect(opts.tone).toBe('danger')
    expect(opts.confirmLabel).toBe('Kill 终端')
    expect(opts.description).toContain('除账')
    expect(opts.description).toContain('不可恢复')
  })

  it('Kill 后台会话（非选中）不碰当前通道：连接与画面原样，行从名录消失', async () => {
    const w = await mountLive([
      makeWireRow({ id: 'c1', title: '在用' }),
      makeWireRow({ id: 'c2', title: '后台' }),
    ])
    const t = term()
    const socketsBefore = h.sockets.length
    await w.findAll('.tc-kill')[1].trigger('click')
    await flushMicrotasks()
    expect(svc.KillTerminal).toHaveBeenCalledWith('c2')
    expect(h.sockets).toHaveLength(socketsBefore) // 没为后台 Kill 断/重建当前连接
    expect(t.resets).toBe(0)
    expect(w.findAll('.tc-title').map((n) => n.text())).toEqual(['在用'])
  })

  it('无名录挂载给引导空态不自动建会话；首拉失败覆盖层报错，重试成功后回引导态', async () => {
    svc.ListTerminals.mockRejectedValueOnce(new Error('模块未启动'))
    const w = await mountView([])
    expect(w.find('.tm-overlay-error').text()).toContain('模块未启动')
    expect(h.sockets).toHaveLength(0)
    expect(svc.OpenTerminal).not.toHaveBeenCalled()
    svc.ListTerminals.mockResolvedValueOnce([])
    await w.find('.tm-overlay .btn').trigger('click')
    await flushMicrotasks()
    expect(w.find('.tm-overlay-error').exists()).toBe(false)
    expect(w.find('.tm-overlay').text()).toContain('还没有终端会话')
  })

  it('名录里的已退出行：接入不拉通道，覆盖层交代"画面已清/Kill 清账"（不臆造可续进程）', async () => {
    const w = await mountView([makeWireRow({ id: 'dead1', title: '残骸', alive: false })])
    await w.findAll('.tc-chip')[0].trigger('click')
    await nextTick()
    expect(h.sockets).toHaveLength(0) // 死会话不开显示通道
    expect(w.find('.tm-overlay').text()).toContain('进程已退出')
    expect(w.find('.tc-slot-current .chip').text()).toBe('已退出')
  })
})

describe('TerminalView xterm 引擎装配（DOM 渲染器与 CJK 宽度）', () => {
  it('装配四件套：unicode11 激活 11.0、fit/web-links/search 挂上、scrollback 5000；搜索条 Enter=findNext/Shift+Enter=findPrevious/Esc 收起', async () => {
    const w = await mountLive([makeWireRow()])
    const t = term()
    // unicode 读取本身经复刻门禁的 getter——没开 allowProposedApi 时这一行
    // 就抛（真机炸相原样），显式断言是给读用例的人看的账。
    expect(t.options.allowProposedApi).toBe(true)
    expect(t.unicode.activeVersion).toBe('11.0')
    expect(t.addons).toHaveLength(4)
    expect(t.options.scrollback).toBe(5000)
    await w.find('.tm-search-toggle').trigger('click')
    await w.find('.tm-search-input').setValue('关键词')
    await w.find('.tm-search-input').trigger('keydown', { key: 'Enter', shiftKey: false })
    await w.find('.tm-search-input').trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(t.search?.nextQ).toEqual(['关键词'])
    expect(t.search?.prevQ).toEqual(['关键词'])
    await w.find('.tm-search-input').trigger('keydown', { key: 'Escape' })
    expect(w.find('.tm-search-input').exists()).toBe(false)
  })

  it('输入只在 live 通道上行 0x01 帧；断线退避期按键丢弃不假装排队（错序幻觉比丢字更坏）', async () => {
    const w = await mountLive([makeWireRow()])
    const t = term()
    const s = socket(0)
    t.fireData('ls\r')
    const tail = sentFrames(s)[s.sent.length - 1]
    expect(tail?.kind === 'data' && decodeBytes(tail.bytes)).toBe('ls\r')
    s.fireClose() // 断线进 retrying
    await nextTick()
    const sentBefore = s.sent.length
    t.fireData('幽灵输入')
    expect(s.sent).toHaveLength(sentBefore)
    expect(w.find('.tc-slot-current .chip').text()).toBe('重连中')
  })
})
