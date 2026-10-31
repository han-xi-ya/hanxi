// 特征测试：AiChatView + useAiChat（AI 聊天前端）。
// 后端契约事实（internal/modules/aichat/）钉在本测试里的部分：
//  - SendMessage 快返回 {turnId}，回复走 aichat:timeline / aichat:session-state /
//    aichat:turn-stats 三事件；后端不回推 user 回声——乐观 user 气泡按返回的
//    turnId 回填并与 stats 对账；
//  - session-state 实档只有 idle/dead/closed（working 不发，在途为前端推导）；
//  - 同会话一轮在途禁发（后端 busy 当面拒，错误如实上屏不伪装）；切会话不禁，
//    后台事件照常入账目并计未读；
//  - "停止"=CloseSession（封存，下一条自动复进）；DeleteSession=除名删仓。
// 打桩形制：绑定 vi.mock（aichatservice 函数式导出假定路径，missingBindingSeam
// 为未落盘生成物解析空壳，JsonFmtView.spec 头注在册同一条路）；Events 桩收编
// runtime.handlers 手播（MsgBoardView.spec 同法）；useConfirm/usePrompt hoisted
// mock（JsonFmtView.spec 同法）；rAF 断言用 nextFrame（FrpcProjectsView.spec 同法）。
// fake timers 下排干 await 一律 flushMicrotasks（JsonFmtView.spec 头注纪律）——
// 本套件的帧调度是 rAF，不挂定时器，帧断言走真实 rAF。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AiChatView from '../AiChatView.vue'
import { EXTRACT_JSON_PROMPT, handOff, peekForChat, takeForChat, takeForFmt } from '../../composables/useJsonChatBridge'

const svc = vi.hoisted(() => ({
  CreateSession: vi.fn(),
  ListSessions: vi.fn(),
  RenameSession: vi.fn(),
  CloseSession: vi.fn(),
  DeleteSession: vi.fn(),
  SendMessage: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/aichat/aichatservice', () => svc)

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return vi.fn()
    },
  },
}))

const confirmBox = vi.hoisted(() => ({ confirm: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => confirmBox }))

const promptBox = vi.hoisted(() => ({ prompt: vi.fn() }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => promptBox }))

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()))
}

/** 一帧 + 排干：事件 → pending → rAF flush → computed → patch 全链路走完。 */
async function settleFrame() {
  await nextFrame()
  await nextTick()
  await flushMicrotasks()
}

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

// —— 后端形状工厂（json tag 与 models.go 逐字一致：id/title/createdAt/archived）——
let idSeq = 0
function makeRow(over: Record<string, unknown> = {}) {
  return {
    id: `s${++idSeq}`,
    title: '会话',
    createdAt: 1730000000000 + idSeq,
    archived: false,
    ...over,
  }
}

function fireTimeline(sessionId: string, turnId: string, text: string, kind = 'assistant', ts = 1730000009000) {
  runtime.handlers['aichat:timeline']({ data: { sessionId, turnId, kind, text, ts } })
}
function fireState(sessionId: string, state: string, note = '') {
  runtime.handlers['aichat:session-state']({ data: { sessionId, state, note } })
}
function fireStats(sessionId: string, turnId: string, over: Record<string, unknown> = {}) {
  runtime.handlers['aichat:turn-stats']({
    data: { sessionId, turnId, elapsedMs: 2500, ttftMs: 800, inBytes: 0, outBytes: 2048, totalCostUsd: 0.0123, ...over },
  })
}

async function mountView(rows: ReturnType<typeof makeRow>[] = []) {
  svc.ListSessions.mockResolvedValue(rows)
  const wrapper = mount(AiChatView, { attachTo: document.body })
  await flushMicrotasks()
  return wrapper
}

/** 选中列表第 i 个会话行并收口装载。 */
async function selectRow(wrapper: Awaited<ReturnType<typeof mountView>>, i = 0) {
  await wrapper.findAll('.ac-sess-main')[i].trigger('click')
  await nextTick()
  await flushMicrotasks()
}

/** 给 textarea 打字并按 Enter（走 IME 守卫后的真发送路径）。 */
async function typeAndEnter(wrapper: Awaited<ReturnType<typeof mountView>>, text: string) {
  await wrapper.find('#ac-draft').setValue(text)
  await wrapper.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: false })
}

/** happy-dom 没有布局引擎：滚动几何必须显式造（阈值判定读的是元素字段）。 */
function setScrollGeom(el: Element, opts: { top: number; client: number; full: number }) {
  Object.defineProperty(el, 'scrollTop', { value: opts.top, writable: true, configurable: true })
  Object.defineProperty(el, 'clientHeight', { value: opts.client, configurable: true })
  Object.defineProperty(el, 'scrollHeight', { value: opts.full, configurable: true })
}

function bubbleTexts(wrapper: Awaited<ReturnType<typeof mountView>>, sel: string): string[] {
  return wrapper.findAll(sel).map((n) => n.find('.ac-msg-text')?.text() ?? n.text())
}

beforeEach(() => {
  idSeq = 0
  takeForChat() // 排空 JSON 桥（模块级单例跨用例存活，防上一用例的货弹进本用例）
  takeForFmt()
  confirmBox.confirm.mockResolvedValue(true)
  promptBox.prompt.mockResolvedValue(null)
  svc.CreateSession.mockImplementation((title: string) => Promise.resolve(makeRow({ title })))
  svc.RenameSession.mockResolvedValue(undefined)
  svc.CloseSession.mockResolvedValue(undefined)
  svc.DeleteSession.mockResolvedValue(undefined)
  let sendN = 0
  svc.SendMessage.mockImplementation(() => Promise.resolve({ turnId: `turn${++sendN}` }))
})

afterEach(() => {
  vi.clearAllMocks()
  vi.restoreAllMocks()
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('AiChatView 会话列表与门禁', () => {
  it('挂载只调 ListSessions 装载；空列表给出"新建会话"引导空态，不自动建会话', async () => {
    const w = await mountView([])
    expect(svc.ListSessions).toHaveBeenCalledTimes(1)
    expect(svc.CreateSession).not.toHaveBeenCalled()
    expect(w.find('.ac-sessions-empty').text()).toContain('还没有会话')
    expect(w.find('.ac-no-session').exists()).toBe(true)
    // 空态文案必须说清"直接输入即可，自动建号"——旧版"先新建会话"闸把带文本
    // 跳进来的用户卡在门口（机主实测报障），文案随行为一起翻案
    expect(w.find('.ac-no-session').text()).toContain('直接输入并发送')
    expect(w.find('.ac-send').attributes('title')).toBe('请输入内容')
    // 无会话时 Enter 与点击都不造任何外发
    await w.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: false })
    await w.find('.ac-send').trigger('click')
    expect(svc.SendMessage).not.toHaveBeenCalled()
    expect(confirmBox.confirm).not.toHaveBeenCalled()
    w.unmount()
  })

  it('无会话直接发送：静默建号（CreateSession 传空名、零弹命名框）→确认后才 SendMessage，乐观气泡进新会话流', async () => {
    const w = await mountView([])
    confirmBox.confirm.mockResolvedValueOnce(true)
    svc.CreateSession.mockResolvedValueOnce(makeRow({ id: 'auto1', title: '新会话' }))
    svc.SendMessage.mockResolvedValueOnce({ turnId: 't1' })
    await w.find('#ac-draft').setValue('这段日志帮我提取 JSON')
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    expect(svc.CreateSession).toHaveBeenCalledWith('') // 静默建号：不走命名 prompt
    expect(promptBox.prompt).not.toHaveBeenCalled()
    expect(svc.SendMessage).toHaveBeenCalledWith('auto1', '这段日志帮我提取 JSON')
    // 确认框在建号之后仍要弹（每会话首条外发确认不因自动建号而旁路）
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('无会话发送但确认被拒：静默号已建（零网络的账本位）但 SendMessage 零调用、文本回得到', async () => {
    const w = await mountView([])
    confirmBox.confirm.mockResolvedValueOnce(false)
    svc.CreateSession.mockResolvedValueOnce(makeRow({ id: 'auto2', title: '新会话' }))
    await w.find('#ac-draft').setValue('先试试')
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    expect(svc.CreateSession).toHaveBeenCalledTimes(1)
    expect(svc.SendMessage).not.toHaveBeenCalled()
    // 取消不吞用户文本：草稿原样在输入框（外发未遂的代价只能是零，不能是丢）
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe('先试试')
    w.unmount()
  })

  it('进门即聊：装载后未选过项时自动采纳最新会话（带文本跳来的用户不该先找芯片点一下）', async () => {
    const w = await mountView([makeRow({ id: 'n1', title: '最新' }), makeRow({ id: 'n2', title: '次新' })])
    await flushMicrotasks()
    expect(w.find('.ac-pane-title').text()).toBe('最新')
    w.unmount()
  })

  it('「新建会话」走 usePrompt：取消(null)与空串都不发 CreateSession', async () => {
    const w = await mountView([])
    promptBox.prompt.mockResolvedValueOnce(null)
    await w.find('.ac-create').trigger('click')
    await flushMicrotasks()
    promptBox.prompt.mockResolvedValueOnce('   ')
    await w.find('.ac-create').trigger('click')
    await flushMicrotasks()
    expect(svc.CreateSession).not.toHaveBeenCalled()
    expect(w.findAll('.ac-sess')).toHaveLength(0)
    w.unmount()
  })

  it('CreateSession 成功即选中新会话：状态空闲、发送闸开（trim 前）、历史空态文案在屏（契约无历史拉取，不臆造旧消息）', async () => {
    const w = await mountView([makeRow({ title: '旧会话', createdAt: 1000 })])
    promptBox.prompt.mockResolvedValueOnce('聊聊部署')
    svc.CreateSession.mockResolvedValueOnce(makeRow({ id: 'new1', title: '聊聊部署', createdAt: 9999999999999 }))
    await w.find('.ac-create').trigger('click')
    await flushMicrotasks()
    expect(svc.CreateSession).toHaveBeenCalledWith('聊聊部署')
    expect(svc.ListSessions).toHaveBeenCalledTimes(1) // 不整页重拉
    // 后端 store.list 按 createdAt 降序：新会话排最前（列表重排的本地镜像）
    expect(w.findAll('.ac-sess-title').map((n) => n.text())).toEqual(['聊聊部署', '旧会话'])
    expect(w.find('.ac-pane-title').text()).toBe('聊聊部署')
    expect(w.find('.ac-toolbar .chip').text()).toBe('空闲')
    expect(w.find('.ac-empty').text()).toContain('本次运行内收发的内容会保留在这里')
    await w.find('#ac-draft').setValue('第一句')
    expect(w.find('.ac-send').attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('ListSessions 连发两发：晚到的旧一代不得整表回灌（reqSeq 门禁，PortScan 同族）', async () => {
    // 名录非空才挂得出工作台与刷新钮（首装空列表由 loading 闸口挡着刷新入口，
    // 双发在途只在"已有名录"的场景下可达）
    const w = await mountView([makeRow({ id: 'sX', title: '初始一代' })])
    const d1 = deferred<ReturnType<typeof makeRow>[]>()
    const d2 = deferred<ReturnType<typeof makeRow>[]>()
    svc.ListSessions.mockReturnValueOnce(d1.promise).mockReturnValueOnce(d2.promise)
    await w.find('.ac-refresh').trigger('click')
    await w.find('.ac-refresh').trigger('click')
    await flushMicrotasks()
    expect(svc.ListSessions).toHaveBeenCalledTimes(3)
    // 新一代先回、旧一代迟到：旧一代整发（含幽灵行）作废
    d2.resolve([makeRow({ id: 'sX', title: '新标题一代', createdAt: 100 })])
    await flushMicrotasks()
    d1.resolve([makeRow({ id: 'sX', title: '旧标题一代', createdAt: 100 }), makeRow({ id: 'ghost', title: '幽灵会话' })])
    await flushMicrotasks()
    expect(w.findAll('.ac-sess-title').map((n) => n.text())).toEqual(['新标题一代'])
    w.unmount()
  })

  it('刷新合并保留运行态：新表只带账本字段，本地 sendError/state/unread 不被旧字段洗掉', async () => {
    const row = makeRow({ id: 'sM', title: '原标题' })
    const w = await mountView([row])
    await selectRow(w)
    // 造运行态：一次失败的发送 + 一个 dead 状态
    svc.SendMessage.mockRejectedValueOnce(new Error('会话不存在'))
    await typeAndEnter(w, '发不出的话')
    await flushMicrotasks()
    expect(w.find('.ac-send-banner').text()).toContain('会话不存在')
    fireState('sM', 'dead', '进程崩了')
    await settleFrame()
    expect(w.find('.ac-toolbar .chip').text()).toBe('已终止')
    // 刷新：新表仅账本字段变化，运行态位必须活着
    svc.ListSessions.mockResolvedValueOnce([makeRow({ id: 'sM', title: '新标题', createdAt: row.createdAt, archived: true })])
    await w.find('.ac-refresh').trigger('click')
    await flushMicrotasks()
    expect(w.find('.ac-sess-title').text()).toBe('新标题')
    expect(w.find('.ac-send-banner').exists()).toBe(true)
    expect(w.find('.ac-toolbar .chip').text()).toBe('已终止')
    w.unmount()
  })

  it('DeleteSession 先过危险确认：取消不删；确认后除行删仓、选中回落到前一项，迟到事件不落任何 UI', async () => {
    const w = await mountView([makeRow({ id: 'a1', title: '第一' }), makeRow({ id: 'a2', title: '第二' }), makeRow({ id: 'a3', title: '第三' })])
    await selectRow(w, 1) // 选中 a2
    confirmBox.confirm.mockResolvedValueOnce(false)
    await w.findAll('.ac-sess')[1].findAll('.ac-sess-actions .btn')[2].trigger('click') // 删除钮
    await flushMicrotasks()
    expect(svc.DeleteSession).not.toHaveBeenCalled()
    expect(w.findAll('.ac-sess')).toHaveLength(3)
    confirmBox.confirm.mockResolvedValueOnce(true)
    await w.findAll('.ac-sess')[1].findAll('.ac-sess-actions .btn')[2].trigger('click')
    await flushMicrotasks()
    expect(svc.DeleteSession).toHaveBeenCalledWith('a2')
    expect(w.findAll('.ac-sess-title').map((n) => n.text())).toEqual(['第一', '第三'])
    // 选中回落到被删项的前一项（a1）
    expect(w.find('.ac-pane-title').text()).toBe('第一')
    // 迟到事件整体作废：不入流、不复活幽灵行、不弹错
    const before = w.html()
    fireTimeline('a2', 'tx', '迟到的回复')
    fireState('a2', 'idle')
    fireStats('a2', 'tx')
    await settleFrame()
    expect(w.html()).toBe(before)
    expect(w.findAll('.ac-sess')).toHaveLength(2)
    w.unmount()
  })

  it('Rename 走 usePrompt；成功后列表就地改题不整页重拉，且镜像后端解除封存位', async () => {
    const w = await mountView([makeRow({ id: 'sR', title: '旧名', archived: true })])
    await selectRow(w)
    expect(w.find('.ac-toolbar .chip').text()).toBe('已封存')
    promptBox.prompt.mockResolvedValueOnce('  新名  ')
    await w.find('.ac-sess-actions .btn').trigger('click') // 第一个行内钮=改名
    await flushMicrotasks()
    expect(svc.RenameSession).toHaveBeenCalledWith('sR', '新名')
    expect(svc.ListSessions).toHaveBeenCalledTimes(1) // 无重拉
    expect(w.find('.ac-sess-title').text()).toBe('新名')
    // service.go RenameSession：改名=还要用它，archived 本地同步解除
    expect(w.find('.ac-toolbar .chip').text()).toBe('空闲')
    // 取消（null）不发 RPC
    promptBox.prompt.mockResolvedValueOnce(null)
    await w.find('.ac-sess-actions .btn').trigger('click')
    await flushMicrotasks()
    expect(svc.RenameSession).toHaveBeenCalledTimes(1)
    w.unmount()
  })
})

describe('AiChatView 发送只在按下的那一刻', () => {
  it('会话第一条消息先要过外发确认；确认后 SendMessage 逐字一次 (sessionId, trim 后文本) 且输入清空、乐观气泡即刻上屏', async () => {
    const w = await mountView([makeRow({ id: 'k1', title: '会话一' })])
    await selectRow(w)
    await typeAndEnter(w, '  帮我看看这段配置  ')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    expect(svc.SendMessage).toHaveBeenCalledTimes(1)
    expect(svc.SendMessage).toHaveBeenCalledWith('k1', '帮我看看这段配置')
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe('')
    expect(bubbleTexts(w, '.ac-msg--user')).toEqual(['帮我看看这段配置'])
    w.unmount()
  })

  it('确认文案必须说出「离开这台机器」「2.5~8 秒」与停止=结束本轮并回收进程（含糊的"使用 AI 处理"不得回头）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    await w.find('#ac-draft').setValue('喂')
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    const opts = confirmBox.confirm.mock.calls[0][0] as { title: string; description: string; tone: string }
    expect(opts.description).toContain('离开这台机器')
    expect(opts.description).toContain('2.5~8 秒')
    expect(opts.description).toContain('结束本轮并回收进程')
    expect(opts.tone).toBe('warning')
    w.unmount()
  })

  it('取消确认一次都不外发，输入保留、无任何在途位残留', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    confirmBox.confirm.mockResolvedValueOnce(false)
    await typeAndEnter(w, '想想再说')
    await flushMicrotasks()
    expect(svc.SendMessage).not.toHaveBeenCalled()
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe('想想再说')
    expect(w.find('.ac-send').text()).toBe('发送 (Enter)')
    expect(w.findAll('.ac-msg--user')).toHaveLength(0)
    // 取消不烧确认位：下一发仍会弹确认
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('确认框待决期间连按 Enter/点钮：confirm 只调一次、SendMessage 零调用（先闸后问，aiAsking 教训移植）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    const d = deferred<boolean>()
    confirmBox.confirm.mockReturnValueOnce(d.promise)
    await w.find('#ac-draft').setValue('连点两下也只许外发一次')
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    // 待决期：按钮进「等待确认…」档且禁用，Enter 被 asking 闸挡（textarea 不禁用是刻意的）
    expect(w.find('.ac-send').text()).toBe('等待确认…')
    expect(w.find('.ac-send').attributes('disabled')).toBeDefined()
    await w.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: false })
    await w.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: false })
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    expect(svc.SendMessage).not.toHaveBeenCalled()
    d.resolve(true)
    await flushMicrotasks()
    expect(svc.SendMessage).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('同会话第二轮不再弹确认；新会话重新弹（确认与会话同生命周期）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    await typeAndEnter(w, '第一句')
    await flushMicrotasks()
    fireStats('k1', 'turn1') // 收口本轮，闸重开
    await settleFrame()
    await typeAndEnter(w, '第二句')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1)
    expect(svc.SendMessage).toHaveBeenCalledTimes(2)
    // 新会话：确认位随会话生灭，首条重新弹
    promptBox.prompt.mockResolvedValueOnce('另一个话题')
    svc.CreateSession.mockResolvedValueOnce(makeRow({ id: 'k2', title: '另一个话题', createdAt: 9999999999998 }))
    await w.find('.ac-create').trigger('click')
    await flushMicrotasks()
    await typeAndEnter(w, '新会话首条')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(2)
    expect(svc.SendMessage).toHaveBeenCalledWith('k2', '新会话首条')
    w.unmount()
  })

  it('同会话一轮在途时输入禁发：钮禁用、Enter 无效；turn-stats 收口后闸重开（后端也会当面拒，前端不留空窗）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    await typeAndEnter(w, '第一轮')
    await flushMicrotasks() // SendMessage 已回 turn1，回合在途
    expect(w.find('.ac-send').text()).toBe('AI 回复中…')
    expect(w.find('.ac-send').attributes('disabled')).toBeDefined()
    await w.find('#ac-draft').setValue('急着追第二句')
    await w.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: false })
    await w.find('.ac-send').trigger('click')
    expect(svc.SendMessage).toHaveBeenCalledTimes(1)
    expect(w.find('.ac-send').attributes('title')).toBe('AI 回复中')
    // 收口：stats(turn1) → 单飞位摘除，再发=新回合（不再弹确认）
    fireStats('k1', 'turn1')
    await settleFrame()
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    expect(svc.SendMessage).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('被拒的在途错误如实上屏不伪装：sendError 横幅 + 流内「未送达」项，乐观气泡与已到达内容一条不砸（失败不清屏）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    await typeAndEnter(w, '第一句')
    await flushMicrotasks()
    fireTimeline('k1', 'turn1', '第一轮的回答')
    await settleFrame()
    fireStats('k1', 'turn1')
    await settleFrame()
    // 第二发被后端单飞闸当面拒（错误文案原样透传）
    svc.SendMessage.mockRejectedValueOnce(new Error('上一轮回答还在途中（常驻会话单轮实测 2.5~8 秒），等它收口再发'))
    await typeAndEnter(w, '第二句')
    await flushMicrotasks()
    expect(w.find('.ac-send-banner').text()).toContain('这一句没出去')
    expect(w.find('.ac-send-banner').text()).toContain('上一轮回答还在途中')
    const errors = w.findAll('.ac-msg-error')
    expect(errors).toHaveLength(1)
    expect(errors[0].text()).toContain('这句话未送达')
    // 已到达的回答与用户内容一条不砸
    expect(bubbleTexts(w, '.ac-msg--assistant')).toEqual(['第一轮的回答'])
    expect(bubbleTexts(w, '.ac-msg--user')).toEqual(['第一句', '第二句'])
    expect(w.find('.ac-toolbar .chip').text()).toBe('空闲')
    w.unmount()
  })

  it('空白/纯空白文本不发（trim 判定，按钮 title 给原因）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    await w.find('#ac-draft').setValue('    ')
    expect(w.find('.ac-send').attributes('disabled')).toBeDefined()
    expect(w.find('.ac-send').attributes('title')).toBe('请输入内容')
    await w.find('.ac-send').trigger('click')
    await w.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: false })
    expect(svc.SendMessage).not.toHaveBeenCalled()
    expect(confirmBox.confirm).not.toHaveBeenCalled()
    w.unmount()
  })

  it('Enter 发送/Shift+Enter 换行；isComposing 的组合期回车不发送（IME 误发钉死）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    await w.find('#ac-draft').setValue('半句话')
    // Shift+Enter：不发送（放行默认换行）
    await w.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(svc.SendMessage).not.toHaveBeenCalled()
    // 组合期回车=确认候选词，不是发送
    const ta = w.find('#ac-draft').element
    const composing = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    Object.defineProperty(composing, 'isComposing', { value: true })
    ta.dispatchEvent(composing)
    await flushMicrotasks()
    expect(svc.SendMessage).not.toHaveBeenCalled()
    expect(confirmBox.confirm).not.toHaveBeenCalled()
    // 正常 Enter 才发
    await w.find('#ac-draft').trigger('keydown', { key: 'Enter', shiftKey: false })
    await flushMicrotasks()
    expect(svc.SendMessage).toHaveBeenCalledWith('k1', '半句话')
    w.unmount()
  })

  it('rows 展开钮在 3/8 间切换，不碰文本内容（纯 UI 位不上收 composable）', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    const ta = w.find('#ac-draft')
    await ta.setValue('多行草稿')
    expect(ta.attributes('rows')).toBe('3')
    await w.find('.ac-expand').trigger('click')
    expect(w.find('#ac-draft').attributes('rows')).toBe('8')
    expect(w.find('.ac-expand').text()).toBe('收起')
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe('多行草稿')
    await w.find('.ac-expand').trigger('click')
    expect(w.find('#ac-draft').attributes('rows')).toBe('3')
    w.unmount()
  })

  it('封存会话的按钮态与复进口径：行内封存钮禁用、chip 已封存、占位语说明发送即自动复进', async () => {
    const w = await mountView([makeRow({ id: 'sC', title: '封存的会话', archived: true })])
    await selectRow(w)
    expect(w.find('.ac-toolbar .chip').text()).toBe('已封存')
    const closeBtn = w.findAll('.ac-sess-actions .btn')[1]
    expect(closeBtn.attributes('disabled')).toBeDefined()
    expect(closeBtn.attributes('title')).toContain('复进')
    expect(w.find('#ac-draft').attributes('placeholder')).toContain('复进')
    // 复进=直接再发一条（首条仍过确认；关闭后本地仓保留，迟到的 closed note 还能在场）
    await typeAndEnter(w, '复进第一句')
    await flushMicrotasks()
    expect(svc.SendMessage).toHaveBeenCalledWith('sC', '复进第一句')
    w.unmount()
  })

  it('「停止」钮=CloseSession：在途在场、title 写死"结束本轮并回收进程"，确认后摘在途位、不弹错误伪装', async () => {
    const w = await mountView([makeRow({ id: 'sS' })])
    await selectRow(w)
    expect(w.find('.ac-stop').exists()).toBe(false)
    await typeAndEnter(w, '长任务')
    await flushMicrotasks()
    const stop = w.find('.ac-stop')
    expect(stop.exists()).toBe(true)
    expect(stop.attributes('title')).toContain('结束本轮并回收进程')
    expect(stop.attributes('title')).toContain('复进')
    await stop.trigger('click') // 破坏性动作先确认（operation-panel 纪律）
    await flushMicrotasks()
    expect(svc.CloseSession).toHaveBeenCalledWith('sS')
    expect(w.find('.ac-stop').exists()).toBe(false)
    expect(w.find('.ac-toolbar .chip').text()).toBe('已封存')
    // 收口后闸重开：同会话已确认过，复进发不弹确认
    // （confirm 到此共两次：首条外发确认 + 封存确认；复进不再加弹）
    await typeAndEnter(w, '复进')
    await flushMicrotasks()
    expect(svc.SendMessage).toHaveBeenCalledTimes(2)
    expect(confirmBox.confirm).toHaveBeenCalledTimes(2)
    w.unmount()
  })
})

describe('AiChatView 事件流进表与状态机', () => {
  it('session-state dead：chip「已终止」且 note 渲染进流（诊断尾巴不吞）；closed/idle 各档翻转输入可用性', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    fireState('s1', 'dead', '进程暴死｜Error parsing streaming input line')
    await settleFrame()
    expect(w.find('.ac-toolbar .chip').text()).toBe('已终止')
    expect(w.find('.ac-note-pill').text()).toContain('Error parsing streaming input line')
    expect(w.find('.ac-announce').text()).toContain('已终止')
    // dead 不锁输入：再发=重试（后端会复进）
    await w.find('#ac-draft').setValue('重试一句')
    expect(w.find('.ac-send').attributes('disabled')).toBeUndefined()
    fireState('s1', 'closed', '会话已封存（下一条消息会自动复进）')
    await settleFrame()
    expect(w.find('.ac-toolbar .chip').text()).toBe('已封存')
    expect(w.findAll('.ac-note-pill').map((n) => n.text())).toHaveLength(2)
    fireState('s1', 'idle')
    await settleFrame()
    expect(w.find('.ac-toolbar .chip').text()).toBe('空闲')
    w.unmount()
  })

  it('timeline 同 (turnId, kind=assistant) 块快照续接成一个气泡；kind 或轮次切换另起新项，互不污染', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    fireTimeline('s1', 'a1', '前半块')
    fireTimeline('s1', 'a1', '后半块')
    fireTimeline('s1', 'a2', '另一轮')
    await settleFrame()
    expect(bubbleTexts(w, '.ac-msg--assistant')).toEqual(['前半块后半块', '另一轮'])
    // 乐观 user 气泡与同轮 assistant 不续接（kind 隔离）
    await typeAndEnter(w, '我发的')
    await flushMicrotasks()
    fireTimeline('s1', 'turn1', '答你的')
    await settleFrame()
    expect(bubbleTexts(w, '.ac-msg--user')).toEqual(['我发的'])
    expect(bubbleTexts(w, '.ac-msg--assistant')).toEqual(['前半块后半块', '另一轮', '答你的'])
    w.unmount()
  })

  it('乐观气泡按 RPC 返回 turnId 回填，turn-stats 按 turnId 锚在该轮尾部：单位换算原样、零二次统计、闸随对账摘除', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    await typeAndEnter(w, '对账轮')
    await flushMicrotasks() // SendMessage → turn1
    fireTimeline('s1', 'turn1', '回答块')
    await settleFrame()
    expect(w.find('.ac-toolbar .chip').text()).toBe('回复中')
    expect(w.findAll('.ac-stats')).toHaveLength(0) // stats 未到：账目行不在场
    fireStats('s1', 'turn1', { elapsedMs: 2500, ttftMs: 800, inBytes: 0, outBytes: 2048, totalCostUsd: 0.0123 })
    await settleFrame()
    expect(w.findAll('.ac-stats')).toHaveLength(1)
    expect(w.find('.ac-stats').text()).toBe('耗时 2.5 s · 首字 800 ms · 入 0 B · 出 2.0 KB · 费用 $0.0123')
    // stats 对账即收口：在途帽摘（钮上不再有「AI 回复中」的禁发理由），
    // 外部事实=第二发真能出去（空草稿的「请输入内容」是正交闸，不算在途残留）
    expect(w.find('.ac-toolbar .chip').text()).toBe('空闲')
    expect(w.find('.ac-send').attributes('title')).toBe('请输入内容')
    await w.find('#ac-draft').setValue('第二轮')
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    expect(svc.SendMessage).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('陈年 turn-stats（已封存/换轮后迟到）不得替新一轮摘帽，也不炸渲染', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    await typeAndEnter(w, '第一轮')
    await flushMicrotasks()
    fireStats('s1', 'turn1')
    await settleFrame()
    await typeAndEnter(w, '第二轮')
    await flushMicrotasks()
    expect(w.find('.ac-toolbar .chip').text()).toBe('回复中')
    fireStats('s1', 'turn1', { elapsedMs: 999 }) // 旧轮迟到的重复 stats
    await settleFrame()
    expect(w.find('.ac-toolbar .chip').text()).toBe('回复中') // 第二轮在途位未被陈年 stats 摘掉
    w.unmount()
  })

  it('rAF 合帧：一帧内 3 个事件帧前不可见、帧后一次到齐（断言渲染批次，不断言回调次数）', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    fireTimeline('s1', 'tA', '甲')
    fireTimeline('s1', 'tB', '乙')
    fireTimeline('s1', 'tC', '丙')
    await nextTick() // 渲染微任务已排干但帧未到：pending 缓冲不得触发表
    expect(w.findAll('.ac-msg--assistant')).toHaveLength(0)
    await settleFrame()
    expect(bubbleTexts(w, '.ac-msg--assistant')).toEqual(['甲', '乙', '丙'])
    w.unmount()
  })

  it('事件按 sessionId 路由：后台会话进表不污染当前流并计未读（列表行数字同步），切回可见且未读清零', async () => {
    const w = await mountView([makeRow({ id: 'bg', title: '后台会话', createdAt: 2000 }), makeRow({ id: 'fg', title: '前台会话', createdAt: 1000 })])
    await selectRow(w, 1) // 前台
    fireTimeline('bg', 'x1', '后台一条')
    fireTimeline('bg', 'x2', '后台两条')
    await settleFrame()
    expect(w.findAll('.ac-msg')).toHaveLength(0) // 当前流零污染
    const bgRow = w.findAll('.ac-sess')[0]
    expect(bgRow.find('.ac-unread').text()).toBe('2') // 列表未读点=数字报数
    expect(w.findAll('.ac-sess')[1].find('.ac-unread').exists()).toBe(false)
    await selectRow(w, 0) // 切回后台会话
    expect(bubbleTexts(w, '.ac-msg--assistant')).toEqual(['后台一条', '后台两条'])
    expect(w.find('.ac-unread').exists()).toBe(false)
    w.unmount()
  })

  it('未在册（从未装载/已删除）会话的三类事件整体作废：不复活幽灵会话、不弹错、不落 UI', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    const before = w.html()
    fireTimeline('ghost', 'g1', '幽灵回复')
    fireState('ghost', 'dead', '幽灵死亡')
    fireStats('ghost', 'g1')
    await settleFrame()
    expect(w.html()).toBe(before)
    expect(w.findAll('.ac-sess')).toHaveLength(1)
    expect(w.find('.ac-list-banner').exists()).toBe(false)
    w.unmount()
  })

  it('回合在途占位在场（2.5~8 秒口径、不诺打字机），块流到达后让位、idle 后消失', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    expect(w.find('.ac-waiting').exists()).toBe(false)
    await typeAndEnter(w, '问一句')
    await flushMicrotasks()
    const waiting = w.find('.ac-waiting')
    expect(waiting.exists()).toBe(true)
    expect(waiting.text()).toContain('2.5~8 秒')
    expect(waiting.text()).not.toContain('打字')
    // 块级快照到达：队尾已是本轮块流，占位让位（"正在生成"由块出现自证）
    fireTimeline('s1', 'turn1', '第一块回答')
    await settleFrame()
    expect(w.find('.ac-waiting').exists()).toBe(false)
    // 新一轮在途（第二轮）时队尾换轮次，占位回到场
    fireStats('s1', 'turn1')
    await settleFrame()
    await typeAndEnter(w, '追一句')
    await flushMicrotasks()
    expect(w.find('.ac-waiting').exists()).toBe(true)
    fireState('s1', 'idle')
    await settleFrame()
    expect(w.find('.ac-waiting').exists()).toBe(false)
    w.unmount()
  })

  it('超 600 条裁头保尾：首项上移、尾项原样，key 用稳定 id 不整表重补（frpc :index 教训反例钉死）', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    for (let i = 1; i <= 602; i++) fireTimeline('s1', `k${i}`, `第${i}条`)
    await settleFrame()
    const bubbles = w.findAll('.ac-msg--assistant')
    expect(bubbles).toHaveLength(600)
    expect(bubbles[0].find('.ac-msg-text').text()).toBe('第3条')
    expect(bubbles[599].find('.ac-msg-text').text()).toBe('第602条')
    w.unmount()
  })
})

describe('AiChatView 滚动跟随', () => {
  it('贴底时新帧自动落底；上滚脱锁（>40px）后到事件不再拉动滚动，悬浮「回到最新消息（N）」在场，点击落底计数清零闩复位', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    const flow = w.find('.ac-flow')
    // 造几何：视口 300、全文 1000、当前位置贴底
    setScrollGeom(flow.element, { top: 700, client: 300, full: 1000 })
    fireTimeline('s1', 'a1', '贴底帧')
    await settleFrame()
    expect((flow.element as Element & { scrollTop: number }).scrollTop).toBe(1000) // 闩开：自动咬合
    expect(w.find('.ac-back').exists()).toBe(false)
    // 用户上滚脱锁：距底 500px（>40 阈值）
    await flow.trigger('scroll')
    setScrollGeom(flow.element, { top: 200, client: 300, full: 1000 })
    await flow.trigger('scroll')
    await nextTick()
    fireTimeline('s1', 'a2', '脱锁帧一')
    fireTimeline('s1', 'a2', '脱锁帧二')
    await settleFrame()
    expect((flow.element as Element & { scrollTop: number }).scrollTop).toBe(200) // 滚动不再被拉动
    const back = w.find('.ac-back')
    expect(back.exists()).toBe(true)
    expect(back.text()).toContain('2 条新内容')
    await back.trigger('click')
    await settleFrame()
    expect((flow.element as Element & { scrollTop: number }).scrollTop).toBe(1000)
    expect(w.find('.ac-back').exists()).toBe(false)
    w.unmount()
  })

  it('发送成功强制落底一次（自己的消息必须被看见），随后用户再上滚照常脱锁', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    const flow = w.find('.ac-flow')
    setScrollGeom(flow.element, { top: 100, client: 300, full: 1000 }) // 先在翻旧账
    await flow.trigger('scroll')
    await nextTick()
    await typeAndEnter(w, '我发的消息我得看见')
    await flushMicrotasks()
    await settleFrame()
    expect((flow.element as Element & { scrollTop: number }).scrollTop).toBe(1000) // 显式越过闩
    expect(w.find('.ac-back').exists()).toBe(false)
    // 随后上滚自然脱锁
    setScrollGeom(flow.element, { top: 50, client: 300, full: 1000 })
    await flow.trigger('scroll')
    await nextTick()
    fireTimeline('s1', 'turn1', '回来的回答')
    await settleFrame()
    expect((flow.element as Element & { scrollTop: number }).scrollTop).toBe(50)
    expect(w.find('.ac-back').exists()).toBe(true)
    w.unmount()
  })

  it('切换会话默认贴底且未读清零', async () => {
    const w = await mountView([makeRow({ id: 'b', title: 'B', createdAt: 2000 }), makeRow({ id: 'a', title: 'A', createdAt: 1000 })])
    await selectRow(w, 1) // A
    const flow = w.find('.ac-flow')
    setScrollGeom(flow.element, { top: 10, client: 300, full: 1000 })
    await flow.trigger('scroll')
    await nextTick()
    fireTimeline('b', 'x1', 'B 的后台消息')
    await settleFrame()
    expect(w.findAll('.ac-sess')[0].find('.ac-unread').exists()).toBe(true)
    await selectRow(w, 0) // 切去 B：进会话看最新，不问旧事
    expect(w.find('.ac-unread').exists()).toBe(false)
    expect(w.findAll('.ac-sess')[0].find('.ac-unread').exists()).toBe(false)
    await settleFrame()
    expect((flow.element as Element & { scrollTop: number }).scrollTop).toBe(1000)
    w.unmount()
  })
})

describe('AiChatView 名录通道（listError 与流通道分家）', () => {
  it('首屏名录失败走页面级 state-error + 重试钮；重试成功后正常进工作台', async () => {
    svc.ListSessions.mockRejectedValueOnce(new Error('模块未启动'))
    const w = mount(AiChatView, { attachTo: document.body })
    await flushMicrotasks()
    expect(w.find('.state-error').text()).toContain('模块未启动')
    svc.ListSessions.mockResolvedValueOnce([makeRow({ id: 's1' })])
    await w.find('.state-error .btn').trigger('click')
    await flushMicrotasks()
    expect(w.findAll('.ac-sess')).toHaveLength(1)
    w.unmount()
  })

  it('已有名录时名录失败只挂 slim 横幅：当前会话的流与输入一条不砸（三通道分家纪律）', async () => {
    const w = await mountView([makeRow({ id: 's1', title: '在用的会话' })])
    await selectRow(w)
    await typeAndEnter(w, '已发出的话')
    await flushMicrotasks()
    fireTimeline('s1', 'turn1', '已到达的回答')
    await settleFrame()
    svc.ListSessions.mockRejectedValueOnce(new Error('名录刷新失败原因'))
    await w.find('.ac-refresh').trigger('click')
    await flushMicrotasks()
    expect(w.find('.ac-list-banner').text()).toContain('名录刷新失败原因')
    expect(bubbleTexts(w, '.ac-msg--assistant')).toEqual(['已到达的回答'])
    expect(bubbleTexts(w, '.ac-msg--user')).toEqual(['已发出的话'])
    expect(w.find('.ac-pane-title').text()).toBe('在用的会话')
    w.unmount()
  })

  it('loading 闸口在场只到首装完成（装载期间不双挂工作台）', async () => {
    const d = deferred<ReturnType<typeof makeRow>[]>()
    svc.ListSessions.mockReturnValueOnce(d.promise)
    const w = mount(AiChatView, { attachTo: document.body })
    await flushMicrotasks()
    expect(w.find('.state-box').text()).toContain('正在读取会话名录')
    expect(w.find('.ac-desk').exists()).toBe(false)
    d.resolve([])
    await flushMicrotasks()
    expect(w.find('.ac-desk').exists()).toBe(true)
    w.unmount()
  })
})

// —— JSON 美化器双向桥（2026-10-31 jsonfmt 收纳裁决的对口）——
// 入向：toChat 文本按编排模板预填，**发送仍由人按下**（外发确认链原样复用）；
// 出向：收口回合的合法 ```json 块挂卡，按下写桥 (toFmt) + emit navigate。
// 本地 JSON.parse 只当判卷：非法块无卡——卡一钓就砸。
describe('AiChatView ↔ JSON 美化器桥', () => {
  it('「提取 JSON」快捷钮：无待发文本禁用且 title 说明；有草稿按下逐字预填编排模板、零自动发送', async () => {
    const w = await mountView([makeRow({ id: 'k1' })])
    await selectRow(w)
    // 桥空 + 输入空 → 禁用，禁用理由要说得出
    expect(w.find('.ac-extract').attributes('disabled')).toBeDefined()
    expect(w.find('.ac-extract').attributes('title')).toContain('没有待提取文本')
    await w.find('#ac-draft').setValue('2026-10-31 INFO {"a":1')
    expect(w.find('.ac-extract').attributes('disabled')).toBeUndefined()
    await w.find('.ac-extract').trigger('click')
    await flushMicrotasks()
    const expected = '请从下面文本里提取所有 JSON 值（对象或数组），数字逐字符照抄不许改精度、不许科学计数法，每个结果单独放一个 ```json 代码块，除代码块外不要写任何说明：\n\n2026-10-31 INFO {"a":1'
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe(expected)
    expect(svc.SendMessage).not.toHaveBeenCalled()
    expect(confirmBox.confirm).not.toHaveBeenCalled()
    // 再按一次不叠第二层模板（包装幂等）
    await w.find('.ac-extract').trigger('click')
    await flushMicrotasks()
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe(expected)
    w.unmount()
  })

  it('带桥文本进会话：模板逐字预填 + 来源条说明出处；不自动发送，按下发送才走外发确认链', async () => {
    handOff('{"broken":')
    const w = await mountView([makeRow({ id: 'k1' })])
    await flushMicrotasks()
    const expected = EXTRACT_JSON_PROMPT + '\n\n' + '{"broken":'
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe(expected)
    expect(w.find('.ac-bridge-banner').text()).toContain('来自 JSON 美化器')
    expect(svc.SendMessage).not.toHaveBeenCalled()
    expect(confirmBox.confirm).not.toHaveBeenCalled()
    expect(peekForChat()).toBeNull() // 读后即清：同一份不二次弹入
    await selectRow(w)
    await w.find('.ac-send').trigger('click')
    await flushMicrotasks()
    expect(confirmBox.confirm).toHaveBeenCalledTimes(1) // 确认链在"按下"那一刻照常触发
    expect(svc.SendMessage).toHaveBeenCalledWith('k1', expected)
    w.unmount()
  })

  it('桥文本优先于草稿：快捷钮按下时交的是美化器送来的原文（已知取舍，见桥文件头注）', async () => {
    handOff('第一段')
    const w = await mountView([makeRow({ id: 'k1' })])
    await flushMicrotasks() // 挂载即消费带入——为验证按钮的桥优先，这里再造一份
    handOff('桥里的新段 {"x":1')
    await w.find('#ac-draft').setValue('我自己打的草稿')
    await w.find('.ac-extract').trigger('click')
    await flushMicrotasks()
    expect((w.find('#ac-draft').element as HTMLTextAreaElement).value).toBe(
      EXTRACT_JSON_PROMPT + '\n\n桥里的新段 {"x":1',
    )
    expect(peekForChat()).toBeNull()
    w.unmount()
  })

  it('回程卡判定：合法 ```json 块出卡、非法块无卡、回合未收口不出卡', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    await typeAndEnter(w, '帮我提 JSON')
    await flushMicrotasks() // turn1 在途
    fireTimeline('s1', 'turn1', '好的：\n```json\n{"a":1}\n```\n另一个：\n```json\n{坏掉的,}\n```')
    await settleFrame()
    expect(w.findAll('.ac-json-card')).toHaveLength(0) // 未收口：卡不钓人
    fireStats('s1', 'turn1')
    await settleFrame()
    const cards = w.findAll('.ac-json-card')
    expect(cards).toHaveLength(1) // 非法块静默不挂卡
    expect(cards[0].text()).toContain('在 JSON 美化器中打开')
    w.unmount()
  })

  it('卡按下：handToFmt(块原文) + emit navigate /ext/jsonfmt；多块逐块列、各开各的', async () => {
    const w = await mountView([makeRow({ id: 's1' })])
    await selectRow(w)
    await typeAndEnter(w, '两段都要')
    await flushMicrotasks()
    fireTimeline('s1', 'turn1', '```json\n{"a":1}\n```\n```json\n[1,2]\n```')
    fireStats('s1', 'turn1')
    await settleFrame()
    const cards = w.findAll('.ac-json-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('第 1 块')
    expect(cards[1].text()).toContain('第 2 块')
    await cards[1].trigger('click')
    await flushMicrotasks()
    expect(takeForFmt()).toBe('[1,2]') // 消费即取——顺带钉死读后即清
    expect(w.emitted('navigate')).toEqual([['/ext/jsonfmt']])
    w.unmount()
  })
})

// 注：出向的 handToFmt 由卡片点击路径覆盖（上一条用例），不在这里直调预置——
// 桥的 toFmt 消费面已由 JsonFmtView.spec 的「回程桥消费」族钉死。
