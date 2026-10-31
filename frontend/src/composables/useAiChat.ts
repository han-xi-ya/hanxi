// AI 聊天（AiChatView）业务状态与操作单一来源。编排壳分离形制照 useWechatBot
// （bindings 调用序列、事件名、门禁矩阵、进表合并规则均属契约面，由特征测试锁定；
// 纯 UI 折叠态——输入框收展——留在视图本地，不上了收本模块）。
//
// 后端契约事实（internal/modules/aichat/，service.go/models.go 逐字对过）：
//  - RPC 快返回 + 事件流收口：SendMessage 立即回 {turnId}，正文/终态全走事件；
//    后端**不回推 user 回声**（协议里 user 是 tool_result 回填位，v1 无工具），
//    用户气泡由前端按返回的 turnId 乐观渲染并回填，不存在"事件对账第二个裁判"。
//  - 文本粒度是 content 块快照（每个块完成时一条帧），**不是逐 token**——
//    UI 文案不许诺打字机；事件回调仍走 rAF 合帧（frpc 教训：每事件一批渲染是病）。
//  - session-state 实际只会出现 idle / dead / closed 三档（working 后端不发，
//    "在途中"由前端 sending ∪ turnInFlight 本地推导）；note 是死亡原因/诊断尾巴，
//    进流渲染为居中胶囊。
//  - "停止" = CloseSession（结束本轮并回收进程，会话封存；下一条自动 --resume 复进，
//    跨进程记忆实测成立）。秒级 interrupt 未验证，不存在"即刻打断"的承诺口径。
//  - 同会话一轮在途时后端会当面拒第二发（"上一轮回答还在途中……等它收口再发"），
//    前端矩阵同步禁发；真被拒时错误如实显示（sendError 通道），不伪装不吞。
//  - 单轮实测 2.5~8 秒（aiagent/stream.go 头注在册实测值，常驻形态免冷启动）。
//
// 确认策略：以**会话**为外发确认单位——每个新会话的第一次发送前弹一次，
// 该会话内后续轮次不再弹。理由逐字留档：会话 = 一次全新的模型上下文暴露，
// 知情同意的粒度应与暴露的粒度一致；全局"只问一次"会静默吞掉新会话的告知义务。
//
// 通道分家（JsonFmtView 教训的直接移植）：listError（列表 RPC 族）≠ sendError
//（SendMessage 抛，"这一句没出去"）≠ 流内 error/note 项（"这一轮/这个进程的事"）。
// 一轮 dead 不得清掉已渲染 items，一次 listError 不得清掉已选会话的流——混写会
// 让用户去查错的地方。
//
// 过期结果门禁（§2.3 逐条裁定的落地）：
//  - ListSessions 走 reqSeq 代次门禁（PortScan/JsonFmt 同族）：晚到的旧一代整发丢弃；
//    合并不整表回灌——title/archived/createdAt 以新表为准，运行态（state/sending/
//    asking/unread/confirmedSend/sendError）本地权威，本地有而新表没有的会话=除名。
//  - 已删除会话（仓随除名释放）之后到达的任何事件整体作废、静默——对已经不存在的
//    会话播报失败没有意义。
//
// 生命周期：必须在组件 setup 同步期内调用——内部 useWailsEvent ×3 订阅
//（aichat:timeline / aichat:session-state / aichat:turn-stats，3 订阅/3 注销配对，
// 勿增删改同名订阅）；rAF 合帧位 flushScheduled 无句柄需取消（残帧至多一次空
// flush，卸载后 Wails 已断流，同 frpc 形态，无害）。KeepAlive 停用不注销监听，
// handler 只写 Map 与响应式位、不触达 DOM；**禁止**在 onDeactivated 里"暂停事件
// 消费"——回合状态机会因此错位。
//
// 渲染驱动：timeline 存于按 sessionId 分仓的 Map（值整表置换 + timelineVersion
// 自增触发渲染，浅层不逐条 deep reactive——frpc perf 改造的同一教训）；
// turn-stats 频率每轮一条，不进 rAF 通道，handler 直写 + statsVersion 触发。
import { computed, nextTick, onScopeDispose, ref } from 'vue'
// 绑定生成物由主会话落盘（假定路径/导出形制照 jsonfmtservice.js 的函数式导出）。
// 生成物名若与假定不符，改动面就是这一行 import + spec 的 vi.mock 路径，架构不动。
import * as AiChatAPI from '../../bindings/hanxi/internal/modules/aichat/aichatservice'
import { getErrorMessage } from '../utils/errors'
import { useConfirm } from './useConfirm'
import { usePrompt } from './usePrompt'
import { useWailsEvent } from './useWailsEvent'
import { trimToCap } from './useLogTails'

/** 单会话消息流条目上限：超限裁头保尾（护栏，见 AiChatFlow 头注的虚拟滚动否决）。 */
export const FLOW_CAP = 600

/** 会话进程状态：后端只会推这三档（working 由前端本地在途位推导）。 */
export type SessionState = 'idle' | 'dead' | 'closed'

/**
 * 流内条目种类。后端 timeline 契约字段允许 assistant|error|note（现 pump 只发
 * assistant）；'user' 与部分 'error'/'note' 由前端本地写入（乐观气泡 / RPC 抛错 /
 * 状态事件 note 尾巴）。无 thinking/tool 档——内核在 aiagent 层就丢弃这类噪音帧。
 */
export type ChatKind = 'user' | 'assistant' | 'error' | 'note'

export interface TimelineItem {
  /** 单调 seq（全模块一个计数器），v-for 稳定 key——frpc logRowSeq 口径。 */
  id: number
  kind: ChatKind
  text: string
  /** 后端原样（unix ms）；本地项取 Date.now()。 */
  ts: number
  turnId: string
}

export interface TurnStats {
  turnId: string
  elapsedMs: number
  ttftMs: number
  inBytes: number
  outBytes: number
  totalCostUsd: number
}

/** 列表行 = 后端 SessionRow 账本字段 + 前端每会话运行态（运行态永不回写后端）。 */
export interface SessionRow {
  id: string
  title: string
  createdAt: number
  /** 封存位（CloseSession 置位，Rename 解除，SendMessage 自动复进）。 */
  archived: boolean
  /** 未见事件前恒 idle。 */
  state: SessionState
  /** SendMessage RPC 在途。 */
  sending: boolean
  /** 外发/危险确认框待决（先闸后问，aiAsking 教训移植）。 */
  asking: boolean
  /** 本轮在途：RPC 已回、回合未收口（turn-stats/dead/closed 收口）。 */
  turnInFlight: boolean
  /** 当前在途回合 uuid（turn-stats 按它对账；状态事件清位）。 */
  activeTurn: string
  /** 本会话 SendMessage 失败通道（与回合内事件分家）。 */
  sendError: string
  /** 本会话已完成首次外发确认（随会话生灭）。 */
  confirmedSend: boolean
  /** 脱锁/后台期间新增流条目数。 */
  unread: number
  /** sr-only role=status 的回合级播报位（token 级内容不进读屏，已知取舍）。 */
  announce: string
}

/**
 * AiChatFlow defineExpose 的最小句柄契约（滚动经此触达，同 wechatbot 形制）。
 * 闩用方法而不是 Ref 暴露：defineExpose 的公开代理会把 Ref 自动解包，
 * 句柄上拿到的是裸 boolean——方法形态不随解包语义漂移。
 */
export interface AiChatFlowHandle {
  /** 贴底跟随闩（flush 据此决定滚动还是计未读）。 */
  isFollowing(): boolean
  /** 闩控落底；force=true 先复位闩再落底（发送/切会话的显式越过）。 */
  scrollToBottom(force?: boolean): void
}

// —— 事件载荷形状（json tag 与 models.go 逐字对齐；全部防御式消费）——
interface TimelineEvent {
  sessionId?: string
  turnId?: string
  kind?: string
  text?: string
  ts?: number
}
interface SessionStateEvent {
  sessionId?: string
  state?: string
  note?: string
}
interface TurnStatsEvent {
  sessionId?: string
  turnId?: string
  elapsedMs?: number
  ttftMs?: number
  inBytes?: number
  outBytes?: number
  totalCostUsd?: number
}

/** 后端账本行（camelCase json tag 形状）；前端补运行态成 SessionRow。 */
interface WireSessionRow {
  id?: string
  title?: string
  createdAt?: number
  archived?: boolean
}

function toLocalRow(w: WireSessionRow): SessionRow {
  return {
    id: String(w?.id ?? ''),
    title: String(w?.title ?? '新会话'),
    createdAt: Number(w?.createdAt ?? 0),
    archived: !!w?.archived,
    state: 'idle',
    sending: false,
    asking: false,
    turnInFlight: false,
    activeTurn: '',
    sendError: '',
    confirmedSend: false,
    unread: 0,
    announce: '',
  }
}

const STATE_VALUES: readonly SessionState[] = ['idle', 'dead', 'closed']

export function useAiChat() {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // —— 账本与选择 ——
  const sessions = ref<SessionRow[]>([])
  const currentId = ref('')
  const listLoading = ref(false)
  const listError = ref('')
  /** ListSessions 请求代次（晚到的旧一代整发丢弃，PortScan 同族）。 */
  let listSeq = 0

  // —— 分仓账本（仓的在册性 = 会话的"本机存在性"：未在册事件整体作废）——
  const timelines = new Map<string, TimelineItem[]>()
  const turnStats = new Map<string, Map<string, TurnStats>>()
  const timelineVersion = ref(0)
  const statsVersion = ref(0)
  /** 全模块一个单调计数器：TimelineItem.id 的稳定 key 源。 */
  let itemSeq = 0

  const inputText = ref('')
  const flowRef = ref<AiChatFlowHandle | null>()

  function findRow(id: string): SessionRow | undefined {
    return sessions.value.find((s) => s.id === id)
  }

  const currentRow = computed(() => findRow(currentId.value) ?? null)
  const currentItems = computed<TimelineItem[]>(() => {
    timelineVersion.value
    const arr = timelines.get(currentId.value)
    return arr ? arr.slice() : []
  })
  const currentStats = computed(() => {
    statsVersion.value
    return turnStats.get(currentId.value) ?? new Map<string, TurnStats>()
  })

  // —— 滚动触达（经 AiChatFlow 句柄，nextTick 时序即契约，wechatbot 同款）——
  function scrollCurrent(force = false) {
    const handle = flowRef.value
    if (handle) handle.scrollToBottom(force)
  }
  // 乐观上屏与 flush 都可能立即换表；滚动必须等 DOM patch 之后（useLogTails 同因）
  function scrollCurrentNextTick(force = false) {
    void nextTick(() => scrollCurrent(force))
  }

  // —— rAF 合帧（逐字照 frpc pending + scheduleLogFlush 形态）——
  interface PendingEntry { sid: string; item: TimelineItem }
  let pending: PendingEntry[] = []
  let flushScheduled = false

  function newLocalItem(kind: ChatKind, text: string, turnId = ''): TimelineItem {
    return { id: ++itemSeq, kind, text, ts: Date.now(), turnId }
  }

  /** 一批草稿合进分仓（按到达序；同 (turnId,assistant) 续接，其余另起新项）。 */
  function applyBatch(batch: PendingEntry[]): void {
    const bySid = new Map<string, PendingEntry[]>()
    for (const entry of batch) {
      if (!timelines.has(entry.sid)) continue // §已删仓/未在册：作废
      const group = bySid.get(entry.sid)
      if (group) group.push(entry)
      else bySid.set(entry.sid, [entry])
    }
    for (const [sid, entries] of bySid) {
      const store = (timelines.get(sid) ?? []).slice()
      for (const { item } of entries) {
        const tail = store[store.length - 1]
        // 进表规则：仅 assistant 同轮续接（块快照到达序即块序，恒为一个气泡）；
        // kind 或 turnId 变化另起新项，前端不猜测回合边界、不推断合并。
        if (item.kind === 'assistant' && tail && tail.kind === 'assistant' && tail.turnId === item.turnId) {
          store[store.length - 1] = { ...tail, text: tail.text + item.text }
        } else {
          store.push(item)
        }
      }
      timelines.set(sid, trimToCap(store, FLOW_CAP))
      const row = findRow(sid)
      if (!row) continue
      if (sid === currentId.value) {
        if (flowRef.value?.isFollowing()) scrollCurrent(false)
        else row.unread += entries.length // 脱锁期间当前会话也计未读（悬浮钮报数）
      } else {
        row.unread += entries.length // 后台会话照常入账目并计未读（多会话的意义）
      }
    }
    timelineVersion.value++
  }

  function scheduleFlush(): void {
    if (flushScheduled) return
    flushScheduled = true
    requestAnimationFrame(() => {
      flushScheduled = false
      if (pending.length === 0) return
      const batch = pending
      pending = []
      applyBatch(batch)
    })
  }

  /** 同步排干（发送前调用：杜绝"在途 pending 晚于乐观气泡落仓"的错序窗口）。 */
  function flushPendingNow(): void {
    if (pending.length === 0) return
    const batch = pending
    pending = []
    applyBatch(batch)
  }
  onScopeDispose(() => {
    pending = []
  })

  // —— 事件订阅（载荷字段与 models.go json tag 逐字一致，防御式消费）——
  useWailsEvent<TimelineEvent>('aichat:timeline', (ev) => {
    if (!ev?.sessionId || !timelines.has(ev.sessionId)) return // 未在册会话：作废
    const kind = ev.kind === 'assistant' || ev.kind === 'error' || ev.kind === 'note' ? ev.kind : null
    if (!kind || typeof ev.text !== 'string') return // 契约外的 kind（如 user）不进表
    pending.push({ sid: ev.sessionId, item: { id: ++itemSeq, kind, text: ev.text, ts: Number(ev.ts ?? Date.now()), turnId: String(ev.turnId ?? '') } })
    scheduleFlush()
  })

  useWailsEvent<SessionStateEvent>('aichat:session-state', (ev) => {
    const sid = ev?.sessionId
    if (!sid || !timelines.has(sid)) return // 迟到/未在册事件：丢弃不复活，静默
    const state = STATE_VALUES.includes(ev.state as SessionState) ? (ev.state as SessionState) : null
    if (!state) return
    const row = findRow(sid)
    if (!row) return
    row.state = state
    // 三档全是终态事件：本轮到此为止（stats 若没来，这里兜底清在途位）
    row.turnInFlight = false
    row.activeTurn = ''
    const note = typeof ev.note === 'string' ? ev.note.trim() : ''
    if (note) pending.push({ sid, item: { id: ++itemSeq, kind: 'note', text: note, ts: Date.now(), turnId: '' } })
    row.announce = state === 'dead' ? `会话进程已终止：${note || '未给出原因'}`
      : state === 'closed' ? '会话已封存，下一条消息会自动复进'
      : '会话空闲'
    scheduleFlush() // note 与同会话未刷 timeline 事件走同一队列，保到达序
  })

  useWailsEvent<TurnStatsEvent>('aichat:turn-stats', (ev) => {
    const sid = ev?.sessionId
    if (!sid || !timelines.has(sid)) return
    const turnId = String(ev.turnId ?? '')
    if (!turnId) return
    // 换表不换引用=下游 computed 按 Object.is 判等不重算（Vue 缓存语义）——
    // 必须整 Map 置换，statsVersion 只是 computed 的订阅扳机
    const next = new Map(turnStats.get(sid) ?? [])
    next.set(turnId, {
      turnId,
      elapsedMs: Number(ev.elapsedMs ?? 0),
      ttftMs: Number(ev.ttftMs ?? 0),
      inBytes: Number(ev.inBytes ?? 0),
      outBytes: Number(ev.outBytes ?? 0),
      totalCostUsd: Number(ev.totalCostUsd ?? 0),
    })
    turnStats.set(sid, next)
    const row = findRow(sid)
    // 按 turnId 对账：只收口"自己这一轮"（陈年 stats 不得替新一轮摘帽）
    if (row && turnId === row.activeTurn) {
      row.turnInFlight = false
      row.activeTurn = ''
      row.announce = '回复完成'
    }
    statsVersion.value++
  })

  // —— 列表装载（reqSeq 门禁 + 合并语义定死，§2.3-2）——
  async function refresh(): Promise<void> {
    const seq = ++listSeq
    listLoading.value = true
    listError.value = ''
    try {
      const rows = (await AiChatAPI.ListSessions()) ?? []
      if (seq !== listSeq) return // 晚到的旧一代：整发丢弃
      mergeLedger(rows as WireSessionRow[])
    } catch (err: unknown) {
      if (seq === listSeq) listError.value = getErrorMessage(err)
    } finally {
      if (seq === listSeq) listLoading.value = false
    }
  }

  /** 新表覆盖 title+集合增删，运行态字段本地保留（绝不整表回灌）。 */
  function mergeLedger(rows: WireSessionRow[]): void {
    const prev = new Map(sessions.value.map((r) => [r.id, r]))
    const out: SessionRow[] = []
    for (const w of rows) {
      const id = String(w?.id ?? '')
      if (!id) continue
      const local = prev.get(id)
      if (local) {
        local.title = String(w.title ?? local.title)
        local.archived = !!w.archived
        local.createdAt = Number(w.createdAt ?? local.createdAt)
        prev.delete(id)
        out.push(local)
      } else {
        const fresh = toLocalRow(w)
        openStore(fresh.id)
        out.push(fresh)
      }
    }
    // 本地有而新表没有 = 已被除名（后端/另一入口删的）：仓随除名释放
    for (const stale of prev.values()) releaseStore(stale.id)
    sessions.value = out
    if (currentId.value && !out.some((r) => r.id === currentId.value)) {
      currentId.value = out[0]?.id ?? ''
      if (currentId.value) selectSession(currentId.value)
    } else if (!currentId.value && out.length > 0) {
      // 进门即聊：首装/刷新后没有选中项时自动采纳最新会话（降序表的 out[0]）。
      // 此前首屏 currentId 恒空，带着文本从美化器跳来的用户会被"先新建会话"
      // 闸卡在门口（机主实测报障"无法发送"的根因之一）。
      selectSession(out[0].id)
    }
  }

  function openStore(id: string): void {
    if (!timelines.has(id)) timelines.set(id, [])
    if (!turnStats.has(id)) turnStats.set(id, new Map())
  }

  function releaseStore(id: string): void {
    timelines.delete(id)
    turnStats.delete(id)
    timelineVersion.value++
  }

  function selectSession(id: string): void {
    if (!id || !sessions.value.some((s) => s.id === id)) return
    currentId.value = id
    const row = findRow(id)
    if (row) row.unread = 0
    // 进一个会话默认看最新，不问旧事：闩复位 + 落底
    scrollCurrentNextTick(true)
  }

  /** 悬浮「回到最新消息」点击/重新贴底时由视图接线调用。 */
  function clearUnread(): void {
    const row = currentRow.value
    if (row) row.unread = 0
  }

  // —— 会话账本操作 ——
  /**
   * 静默建会话：不弹命名框。后端 CreateSession 只落账本**零网络行为**，
   * 缺省名"新会话"会被首条消息本地升格——所以"发送前按需建号"没有代价，
   * 而"先建会话才能说话"这道闸只会把带文本跳进来的用户卡在门口（机主实测报障）。
   */
  async function createSessionQuietly(title = ''): Promise<boolean> {
    try {
      const row = await AiChatAPI.CreateSession(title)
      if (!row) return false
      const local = toLocalRow(row as WireSessionRow)
      openStore(local.id)
      // 后端列表按 createdAt 降序（store.list 实测口径）：新会话插到最前
      sessions.value = [local, ...sessions.value.filter((s) => s.id !== local.id)]
      selectSession(local.id)
      return true
    } catch (err: unknown) {
      listError.value = getErrorMessage(err)
      return false
    }
  }

  /** 手动新建（走 usePrompt 命名）：取消与空串仍不建号——误触弹窗不白造会话。 */
  async function createSession(): Promise<void> {
    const name = await prompt({ title: '新建会话', label: '会话名称', placeholder: '一句话说给 AI 的第一件事（留空即取消）', confirmLabel: '新建' })
    if (name === null) return
    const title = name.trim()
    if (!title) return
    await createSessionQuietly(title)
  }

  async function renameSession(id: string): Promise<void> {
    const row = findRow(id)
    if (!row) return
    const name = await prompt({ title: '重命名会话', label: '会话名称', initialValue: row.title })
    if (name === null) return
    const title = name.trim()
    if (!title) return // 后端 RenameSession 同样拒绝空名；前端不发白趟
    try {
      await AiChatAPI.RenameSession(id, title)
      row.title = title
      // 镜像后端语义（service.go RenameSession：改名是"还要用它"的信号，封存位顺手解除）
      row.archived = false
    } catch (err: unknown) {
      listError.value = getErrorMessage(err)
    }
  }

  /**
   * 封存（CloseSession）：结束本轮并回收进程——"停止"钮与列表行"封存"钮同走此路。
   * 破坏性动作先确认（仓内 operation-panel 纪律）；transcript 归 claude 自己的数据根，
   * 文案不假称删除对话内容。确认后本地即摘在途位（closed 事件与迟到 stats 都作废不了 UI）。
   */
  async function closeSession(id: string): Promise<void> {
    const row = findRow(id)
    if (!row) return
    row.asking = true
    let accepted = false
    try {
      accepted = await confirm({
        title: `停止并封存会话「${row.title}」？`,
        description: '会立即结束正在进行的本轮回答并回收该会话的常驻进程；会话转入封存，下一条消息会自动复进（模型侧对话档案保留）。',
        tone: 'danger',
        confirmLabel: '封存会话',
      })
    } finally {
      row.asking = false
    }
    if (!accepted) return
    try {
      await AiChatAPI.CloseSession(id)
      row.archived = true
      row.turnInFlight = false
      row.activeTurn = ''
      row.state = 'closed'
      row.announce = '会话已封存，下一条消息会自动复进'
    } catch (err: unknown) {
      listError.value = getErrorMessage(err)
    }
  }

  /** 删除 = 账本除名（本地本次运行的流随仓释放）；CLI 的 transcript 不归 hanxi 删，文案如实。 */
  async function deleteSession(id: string): Promise<void> {
    const row = findRow(id)
    if (!row) return
    const accepted = await confirm({
      title: `删除会话「${row.title}」？`,
      description: '从会话名录中除名，本次运行收到的消息流立即清空且不可恢复；Claude CLI 自己的会话档案不由 hanxi 删除（要彻底灭迹请在 claude 侧处置）。',
      tone: 'danger',
      confirmLabel: '删除会话',
    })
    if (!accepted) return
    try {
      await AiChatAPI.DeleteSession(id)
    } catch (err: unknown) {
      listError.value = getErrorMessage(err)
      return
    }
    const index = sessions.value.findIndex((s) => s.id === id)
    const rest = sessions.value.filter((s) => s.id !== id)
    releaseStore(id)
    sessions.value = rest
    if (currentId.value === id) {
      // 选中回落规则定死：被删项的前一项，无前项取新首项，空列表回无选中态
      const fallback = rest[Math.max(index - 1, 0)] ?? rest[0]
      currentId.value = fallback?.id ?? ''
      if (fallback) selectSession(fallback.id)
    }
  }

  // —— 发送（§2.2 矩阵 + §4 先闸后问，逐字照确认闸序）——
  const busy = computed(() => {
    const row = currentRow.value
    return !!row && (row.sending || row.turnInFlight)
  })

  /**
   * 禁发原因的单一 computed（矩阵行序即优先级）；空串 = 可发送。
   * "无会话"不再是禁发原因：send() 会在发送前静默建号（建号零网络，
   * 见 createSessionQuietly 注释）——闸只拦真问题（在途/空文本）。
   */
  const sendDisabledReason = computed(() => {
    const row = currentRow.value
    if (row?.asking) return '等待确认中'
    if (row?.sending) return '发送中'
    if (row?.turnInFlight) return 'AI 回复中'
    if (!inputText.value.trim()) return '请输入内容'
    return ''
  })
  const canSend = computed(() => sendDisabledReason.value === '')

  /** 发送钮三态文案档位（AiChatInput 纯展示消费）。 */
  const sendPhase = computed<'idle' | 'asking' | 'sending' | 'working'>(() => {
    const row = currentRow.value
    if (row?.asking) return 'asking'
    if (row?.sending) return 'sending'
    if (row?.turnInFlight) return 'working'
    return 'idle'
  })

  /**
   * 在途占位在场判据：回合在途且队尾不是本轮已到的 assistant 块——块流已在屏时
   * 占位让位（文本是块级快照，"正在生成"由块的出现自证，不装打字机）。
   */
  const waitingPlaceholder = computed(() => {
    const row = currentRow.value
    if (!row || !busy.value) return false
    // 订阅整表版本扳机（timelines 是普通 Map，不读版本号拿到的就是陈值——
    // 块流刚落的帧若不重算，占位会和回答打架）
    void timelineVersion.value
    const items = timelines.get(currentId.value)
    const last = items?.[items.length - 1]
    return !(last && last.kind === 'assistant' && row.activeTurn && last.turnId === row.activeTurn)
  })

  async function send(): Promise<void> {
    let row = currentRow.value
    const text = inputText.value.trim()
    if (!canSend.value || !text) return
    if (!row) {
      // 按需建号：只在真要发送的那一刻发生（"按下才外发"同款纪律的本地半段）
      if (!(await createSessionQuietly())) return
      row = currentRow.value
      if (!row) return
    }
    const sid = row.id

    if (!row.confirmedSend) {
      row.asking = true // 先置闸再弹窗：待决期间连点/连按 Enter 都只改不了这个位
      let accepted = false
      try {
        accepted = await confirm({
          title: '把这句话交给本机 AI？',
          description:
            '消息连同本会话此前约定的上下文会进入模型会话——内容会离开这台机器，不只是本机处理。' +
            '常驻会话单轮实测 2.5~8 秒，期间可切去看别的会话，回复会以事件流回到本会话；' +
            '「停止」等于结束本轮并回收进程（下一条自动复进）。发送后本条无法撤回。',
          confirmLabel: '发送给 AI',
          cancelLabel: '再想想',
          tone: 'warning',
        })
      } finally {
        row.asking = false
      }
      if (!accepted) return
      row.confirmedSend = true
    }

    row.sending = true
    row.sendError = ''
    flushPendingNow() // 杜绝旧回合未刷条目晚于乐观气泡落仓（到达序即账目序）
    const item = newLocalItem('user', text)
    const store = (timelines.get(sid) ?? []).slice()
    timelines.set(sid, trimToCap(store.concat(item), FLOW_CAP))
    timelineVersion.value++
    inputText.value = ''
    row.announce = '已发送，等待 AI 回复'
    scrollCurrentNextTick(true) // 用户自己的动作必须被看到：显式越过闩一次

    try {
      const res = await AiChatAPI.SendMessage(sid, text)
      const turnId = String((res as { turnId?: string } | null)?.turnId ?? '')
      const live = findRow(sid)
      if (live && timelines.has(sid)) {
        if (turnId) {
          // 乐观气泡按 turnId 回填（后端不推 user 回声，turnId 是对账唯一桥）
          const arr = timelines.get(sid)!.map((it) => (it.id === item.id ? { ...it, turnId } : it))
          timelines.set(sid, arr)
          timelineVersion.value++
          live.turnInFlight = true
          live.activeTurn = turnId
          live.announce = 'AI 开始回复'
        }
      }
    } catch (err: unknown) {
      const message = getErrorMessage(err)
      const live = findRow(sid)
      if (live && timelines.has(sid)) {
        live.sendError = message
        const errItem = newLocalItem('error', `这句话未送达：${message}`)
        const arr = (timelines.get(sid) ?? []).concat(errItem)
        timelines.set(sid, trimToCap(arr, FLOW_CAP))
        timelineVersion.value++
        live.announce = `发送失败：${message}`
        scrollCurrentNextTick(false)
      }
      // 失败不清屏：乐观 user 项与已到达 items 一条不砸（JsonFmtView 同款纪律）
    } finally {
      const after = findRow(sid)
      if (after && timelines.has(sid)) after.sending = false
    }
  }

  return {
    // 账本与选择
    sessions,
    currentId,
    currentRow,
    refresh,
    listLoading,
    listError,
    // 流与统计（渲染驱动走版本号整表置换）
    currentItems,
    currentStats,
    // 输入与门禁
    inputText,
    canSend,
    sendDisabledReason,
    sendPhase,
    busy,
    waitingPlaceholder,
    // 滚动句柄（视图 v-ref="flowRef" 绑 AiChatFlow）
    flowRef,
    clearUnread,
    // 动作
    selectSession,
    createSession,
    renameSession,
    closeSession,
    deleteSession,
    send,
  }
}
