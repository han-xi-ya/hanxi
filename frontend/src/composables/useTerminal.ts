// 内嵌终端（TerminalView）编排单源：会话名录 RPC、字节通道（GoStream "terminal:io"）
// 生命周期、标签帧编解码、xterm 引擎装配与三通道状态位全在这里（视图薄接线、
// TermSessionChips 纯展示，形制照 useAiChat/useWechatBot 的编排壳分离）。
//
// 后端契约事实（主会话按此实现，勿改形状）：
//  - RPC（bindings/hanxi/internal/modules/terminal/terminalservice）：
//    OpenTerminal(profileId, cols, rows, dir) → TermSessionRow{id,title,profileId,dir,pid,createdAt,alive}
//    （dir 空=继承 hanxi 的 cwd；非空后端已校验是存在的目录，claude 档的
//    项目上下文/信任提示跟着这个目录走）。
//    ListTerminals() → TermSessionRow[]；RenameTerminal(id, title)；KillTerminal(id)（杀会话并除账）。
//    PickDirectory() → string：Go 侧原生"选择文件夹"对话框（模态挂主窗），
//    取消=空串不是错误；JS→Go 的对话框参数线格式未验证，不自走前端 Dialogs。
//    Profile 固定三档：cmd / powershell / claude（展示名 命令行 / PowerShell / Claude Code）。
//  - 字节通道：`Stream("terminal:io")`（@wailsio/runtime GoStream），每个终端页一条连接。
//    **帧协议：首字节标签：0x00=JSON 控制帧，0x01=原始终端字节。**
//    连接建立（socket onopen）后客户端先发 {"op":"open","sessionId":id}；
//    服务端回 {"op":"ready"} 后 pty 输出以 0x01 帧推送；子进程退出推 {"op":"exit","code":N}；
//    客户端输入 0x01 帧原样上行，resize 走 0x00 帧 {"op":"resize","cols":C,"rows":R}。
//    binaryType 默认 arraybuffer，onmessage ev.data 即 ArrayBuffer，剥标签后直喂 term.write。
//
// v0 语义（如实标注在 UI 文案里，不装"无缝续连"）：
//  - 连接只承载**显示通道**：页卸载/切页/切换会话都会断连接，但**不销毁后端进程**
//    （本 composable 对任何会话从不隐式 KillTerminal；断连后的 pty 字节不补发）。
//  - 断线自动重连：socket close/error → Stream() 重开并重发 open 帧（进程活着就能续），
//    指数退避 1s/2s/4s/8s，共 MAX_RECONNECT 次；用尽后显式报原因并给「立即重连」。
//    **断线/重连任何阶段都不得清屏**——已渲染画面保留（teardown 只动通道，term.reset
//    仅在主动切换会话时调用：画面归属"这一条连接"是 v0 的诚实口径）。
//  - exit 帧后不再自动重连（进程死了，续它没有意义）；Kill 清账或新建同型会话是出路。
//
// 状态位分通道（JsonFmtView/AiChat 同款纪律，混写会让用户查错的地方）：
//  - listError：名录 RPC 族（List/Rename/Kill 抛）——页级 state-box / slim 横幅；
//  - openError：OpenTerminal 抛——"没开出这一页终端"，slim 横幅，不砸已有画面；
//  - connStatus + connNote：显示通道状态（连接中/握手/重连中 n/N/已断）——状态条播报，
//    高频字节流**不**逐条进 aria-live（读屏会被淹死），只在通道级迁移时报一句。
//
// 生命周期：必须在组件 setup 同步期内调用；onScopeDispose 收口（断通道、清定时器、
// dispose xterm）。KeepAlive 场景 onActivated 重接、onDeactivated 主动断——
// "连接即断、进程活着"正是这条钩子的存在理由。rAF/定时器句柄全部成对清理。
import { computed, nextTick, onActivated, onDeactivated, onScopeDispose, ref } from 'vue'
import { Browser, Stream } from '@wailsio/runtime'
import { Terminal } from '@xterm/xterm'
import type { ITheme } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { WebLinksAddon } from '@xterm/addon-web-links'
// xterm 样式表随引擎模块一并引入（无第三方 UI 框架，视图 scoped 只排布页面，
// 单元格/滚动条等终端内部样式归 xterm.css 全局管——构建实测可解析，勿改成 :deep 仿写）。
import '@xterm/xterm/css/xterm.css'
// 绑定生成物由主会话落盘（假定路径/函数式导出形制照 aichat 的 aichatservice.js）。
// 生成物名若与假定不符，改动面就是这一行 import + spec 的 vi.mock 路径，架构不动。
import * as TerminalAPI from '../../bindings/hanxi/internal/modules/terminal/terminalservice'
import { getErrorMessage } from '../utils/errors'
import { useConfirm } from './useConfirm'
import { usePrompt } from './usePrompt'

// —— 契约常量（spec 逐条钉死，改数值须同步改测试）——
export const STREAM_NAME = 'terminal:io'
export const FRAME_CONTROL = 0x00
export const FRAME_DATA = 0x01
/** fit→后端 resize 控制帧的防抖：窗口拖拽期只发末值（150ms，任务钉死值）。 */
export const RESIZE_DEBOUNCE_MS = 150
/** open 帧发出后等待 ready 的上限：超时视作一次断线（不无限挂"握手中"）。 */
export const READY_TIMEOUT_MS = 8000
/** 自动重连尝试上限，指数退避 1s/2s/4s/8s；用尽转「已断线」等手动重连。 */
export const MAX_RECONNECT = 4

export type TermProfileId = 'cmd' | 'powershell' | 'claude'

/** 三档 profile：id 是后端契约值，label 是工作台展示名。 */
export const TERM_PROFILES: ReadonlyArray<{ id: TermProfileId; label: string; hint: string }> = [
  { id: 'cmd', label: '命令行', hint: 'cmd.exe' },
  { id: 'powershell', label: 'PowerShell', hint: 'powershell.exe' },
  { id: 'claude', label: 'Claude Code', hint: '挂 Claude Code 的会话壳' },
]

/** 名录行 = 后端 TermSessionRow 账本字段 + 前端本地派生位（exitCode 只在本通道见过才非空）。 */
export interface TermRow {
  id: string
  title: string
  profileId: string
  /** 会话工作目录；空串=继承 hanxi 的 cwd（UI 如实标"继承"，不装显式选择）。 */
  dir: string
  pid: number
  createdAt: number
  alive: boolean
  /** 本显示通道收到 exit 帧的退出码（外部退出/断线期间退出的是 null——不臆造）。 */
  exitCode: number | null
  /** 危险确认框待决（先闸后问，aiAsking 教训移植）。 */
  asking: boolean
}

/** 显示通道状态机档位。 */
export type ConnStatus = 'detached' | 'connecting' | 'handshaking' | 'live' | 'retrying' | 'down'

// —— 标签帧编解码（纯函数，spec 钉往返）——
const textEncoder = new TextEncoder()
const textDecoder = new TextDecoder('utf-8')

export type InboundFrame =
  | { kind: 'control'; op: string; code?: number; [key: string]: unknown }
  | { kind: 'data'; bytes: Uint8Array }

/** 0x00 + UTF-8(JSON)。 */
export function encodeControlFrame(payload: Record<string, unknown>): Uint8Array {
  const body = textEncoder.encode(JSON.stringify(payload))
  const frame = new Uint8Array(body.length + 1)
  frame[0] = FRAME_CONTROL
  frame.set(body, 1)
  return frame
}

/** 0x01 + UTF-8（客户端输入原样上行）。 */
export function encodeDataFrame(text: string): Uint8Array {
  const body = textEncoder.encode(text)
  const frame = new Uint8Array(body.length + 1)
  frame[0] = FRAME_DATA
  frame.set(body, 1)
  return frame
}

/**
 * 剥标签出帧。空帧/未知标签/JSON 控制帧解析失败 → null（畸形帧整体丢弃：
 * 宁可不渲染也不把垃圾字节喂进渲染器；数据帧不"尽力而为"，标签就是判据）。
 */
export function decodeFrame(payload: ArrayBuffer | Uint8Array | unknown): InboundFrame | null {
  let bytes: Uint8Array
  if (payload instanceof ArrayBuffer) bytes = new Uint8Array(payload)
  else if (payload instanceof Uint8Array) bytes = payload
  else return null
  if (bytes.length === 0) return null
  const tag = bytes[0]
  if (tag === FRAME_DATA) return { kind: 'data', bytes: bytes.subarray(1) } // 视图，零拷贝（写侧 xterm 自取）
  if (tag === FRAME_CONTROL) {
    try {
      const obj = JSON.parse(textDecoder.decode(bytes.subarray(1))) as Record<string, unknown>
      if (obj && typeof obj.op === 'string') return { ...obj, kind: 'control', op: obj.op } as InboundFrame
    } catch {
      /* 畸形控制帧：静默丢，砸画面才是事故 */
    }
  }
  return null
}

// —— 展示辅助（chips/状态条共用，纯函数）——
export function profileLabel(profileId: string): string {
  return TERM_PROFILES.find((p) => p.id === profileId)?.label ?? profileId
}

/** chip 状态字：状态不单靠颜色——文字先行，tone 只是加成（design-system 纪律）。 */
export function rowStatusText(row: TermRow, current: boolean, conn: ConnStatus): string {
  if (!row.alive) return row.exitCode === null ? '已退出' : `已退出(${row.exitCode})`
  if (!current) return '运行中'
  switch (conn) {
    case 'live': return '已连接'
    case 'connecting': return '连接中'
    case 'handshaking': return '握手中'
    case 'retrying': return '重连中'
    case 'down': return '已断线'
    default: return '未接入'
  }
}

export function rowStatusTone(row: TermRow, current: boolean, conn: ConnStatus): 'positive' | 'information' | 'warning' | 'danger' | 'neutral' {
  if (!row.alive) return 'neutral'
  if (!current) return 'neutral'
  if (conn === 'live') return 'positive'
  if (conn === 'down') return 'danger'
  if (conn === 'retrying') return 'warning'
  if (conn === 'connecting' || conn === 'handshaking') return 'information'
  return 'neutral'
}

/**
 * 从 CSS token 现值拼 xterm 主题底（design-system：零裸色进 JS——这里全部经
 * getComputedStyle 取 var() 解析值；取不到用暗档字面兜底，仅为异常防线）。
 * ANSI 16 色沿用 xterm 默认盘（token 化调色板是 v1 的事，头注已备案）。
 */
function readTermTheme(): ITheme {
  const st = getComputedStyle(document.documentElement)
  const pick = (name: string, fallback: string): string => {
    const v = st.getPropertyValue(name).trim()
    return v || fallback
  }
  const primary = pick('--color-primary', '#42b7b4')
  const theme: ITheme = {
    background: pick('--surface-panel', '#0e1c22'),
    foreground: pick('--color-text', '#e8f2f3'),
    cursor: primary,
  }
  // selectionBackground 要 alpha：token 是 6 位 hex（tokens.css 在册形制），
  // 拼 8 位十六进制 alpha；非 hex（被改写）就放弃自定义选择色，回 xterm 默认。
  if (/^#[0-9a-fA-F]{6}$/.test(primary)) theme.selectionBackground = `${primary}59`
  return theme
}

function monoFontFamily(): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim()
  return v || 'ui-monospace, "Cascadia Mono", Consolas, monospace'
}

interface WireTermRow {
  id?: string
  title?: string
  profileId?: string
  dir?: string
  pid?: number
  createdAt?: number
  alive?: boolean
}

function rowFromWire(w: WireTermRow): TermRow {
  return {
    id: String(w?.id ?? ''),
    title: String(w?.title ?? ''),
    profileId: String(w?.profileId ?? ''),
    dir: String(w?.dir ?? ''),
    pid: Number(w?.pid ?? 0),
    createdAt: Number(w?.createdAt ?? 0),
    alive: !!w?.alive,
    exitCode: null,
    asking: false,
  }
}

export function useTerminal() {
  const { confirm } = useConfirm()
  const { prompt } = usePrompt()

  // —— 名录与选择 ——
  const sessions = ref<TermRow[]>([])
  const currentId = ref('')
  /** 首拉在即真：视图覆盖层据此显示「正在读取终端名录…」（挂载即 refresh）。 */
  const listLoading = ref(true)
  const listError = ref('')
  const openError = ref('')
  /** ListTerminals 代次门禁：晚到的旧一代整发丢弃（PortScan/JsonFmt 同族）。 */
  let listSeq = 0

  const currentRow = computed(() => sessions.value.find((r) => r.id === currentId.value) ?? null)

  // —— 通道状态（仅服务 currentId 那一条连接；其余会话天生 detached）——
  const connStatus = ref<ConnStatus>('detached')
  const connError = ref('')
  const reconnectAttempt = ref(0)
  const reconnectDelayMs = ref(0)

  const creating = ref(false)
  // picking：目录选择对话框在途（模态窗等人手，按钮禁双开；与 creating 分档——
  // "在挑目录"和"在开终端"是两件没做完的事，UI 文案各报各的）。
  const picking = ref(false)

  const connNote = computed(() => {
    if (!currentRow.value) return ''
    switch (connStatus.value) {
      case 'connecting': return '正在建立显示通道…'
      case 'handshaking': return '等待后端 ready 握手…'
      case 'live': return '显示通道在线；断线期间的输出不补'
      case 'retrying': return `显示通道中断${connError.value ? `（${connError.value}）` : ''}，${(reconnectDelayMs.value / 1000).toFixed(0)}s 后自动重连（第 ${reconnectAttempt.value}/${MAX_RECONNECT} 次）——后端进程仍在运行，画面保留`
      case 'down': return `自动重连 ${MAX_RECONNECT} 次未果：${connError.value || '原因未知'}。进程若仍活着，点「立即重连」可续上（断线期间的输出不补）`
      default:
        return currentRow.value.alive
          ? '未接入——本页对每个会话只持一条显示通道，点选即接'
          : '进程已退出；Kill 可清账，或新建同型会话'
    }
  })

  /** 手动重连在场条件：选中存活会话且通道不在自动推进中。 */
  const canReconnect = computed(() =>
    !!currentRow.value && currentRow.value.alive && (connStatus.value === 'down' || connStatus.value === 'detached'),
  )

  // —— xterm 引擎（mountTerminal 起才有实例；happy-dom 测试里整层被 mock）——
  let term: Terminal | null = null
  let fitAddon: FitAddon | null = null
  let searchAddon: SearchAddon | null = null
  let dataDisposable: { dispose(): void } | null = null
  let resizeDisposable: { dispose(): void } | null = null
  let resizeObserver: ResizeObserver | null = null
  let themeObserver: MutationObserver | null = null

  // —— 流与定时器（句柄全部成对清理，无泄露才算收口）——
  // WailsSocket 的 TS 面未发布类型声明，这里按 WebSocket 子集自行约束（契约形状见头注）。
  interface TermSocket {
    readyState: number
    binaryType: string
    onopen: (() => void) | null
    onmessage: ((ev: { data: unknown }) => void) | null
    onclose: (() => void) | null
    onerror: (() => void) | null
    send(data: Uint8Array): void
    close(): void
  }
  let conn: TermSocket | null = null
  /** 连接世代号：teardown 后旧 socket 的异步残余事件按世代作废（幽灵重连防线）。 */
  let connEpoch = 0
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let readyTimer: ReturnType<typeof setTimeout> | null = null
  let resizeTimer: ReturnType<typeof setTimeout> | null = null
  /** 最近一次成功送达后端的终端尺寸（ready 后对账：断连期间改过尺寸就补发一帧）。 */
  let lastSentDims = { cols: 0, rows: 0 }

  function termDims(): { cols: number; rows: number } {
    if (term && term.cols > 0 && term.rows > 0) return { cols: term.cols, rows: term.rows }
    return { cols: 80, rows: 24 } // xterm 缺省行宽：未 open 或布局不可测时的建号兜底
  }

  function clearRetry(): void {
    if (retryTimer !== null) { clearTimeout(retryTimer); retryTimer = null }
  }
  function clearReady(): void {
    if (readyTimer !== null) { clearTimeout(readyTimer); readyTimer = null }
  }

  /** 主动断通道（切换/Kill/exit/停用/卸载）。tearing=true 让即将到达的 close/error 回调闭嘴。 */
  function teardownStream(): void {
    connEpoch++
    clearRetry()
    clearReady()
    const s = conn
    conn = null
    if (s) {
      s.onopen = null; s.onmessage = null; s.onclose = null; s.onerror = null
      try { s.close() } catch { /* 已关的连接：close 无害，静默 */ }
    }
    connStatus.value = 'detached'
    connError.value = ''
    reconnectAttempt.value = 0
  }

  function backoffMs(attempt: number): number {
    return 1000 * 2 ** (attempt - 1) // 1s/2s/4s/8s
  }

  /** 断线处置：自动重连预算未用尽则退避重开，用尽显式报（不静默、不砸画面）。 */
  function handleDrop(reason: string): void {
    if (!conn && connStatus.value === 'detached') return
    const s = conn
    conn = null
    if (s) { s.onopen = null; s.onmessage = null; s.onclose = null; s.onerror = null }
    clearReady()
    if (!currentRow.value?.alive) { connStatus.value = 'detached'; return } // 进程没了，续它没有意义
    if (reconnectAttempt.value >= MAX_RECONNECT) {
      connStatus.value = 'down'
      connError.value = reason
      return
    }
    reconnectAttempt.value += 1
    reconnectDelayMs.value = backoffMs(reconnectAttempt.value)
    connStatus.value = 'retrying'
    connError.value = reason
    clearRetry()
    retryTimer = setTimeout(() => {
      retryTimer = null
      openStream()
    }, reconnectDelayMs.value)
  }

  /** 建立一条新连接：open 后发 open 帧，ready 才算活（契约握手机制）。 */
  function openStream(): void {
    const sid = currentId.value
    const row = sessions.value.find((r) => r.id === sid)
    if (!row || !row.alive) return
    clearRetry()
    clearReady()
    connEpoch += 1
    const epoch = connEpoch
    connStatus.value = 'connecting'
    let socket: TermSocket
    try {
      socket = Stream(STREAM_NAME) as unknown as TermSocket
    } catch (err: unknown) {
      connStatus.value = 'down'
      connError.value = getErrorMessage(err)
      return
    }
    conn = socket
    try { socket.binaryType = 'arraybuffer' } catch { /* 只读实现（真 WebSocket 默认即 arraybuffer）：忽略 */ }
    socket.onopen = () => {
      if (epoch !== connEpoch) return
      connStatus.value = 'handshaking'
      socket.send(encodeControlFrame({ op: 'open', sessionId: sid }))
      clearReady()
      readyTimer = setTimeout(() => {
        readyTimer = null
        if (epoch === connEpoch && connStatus.value === 'handshaking') handleDrop(`握手超时：${READY_TIMEOUT_MS / 1000}s 内未收到 ready`)
      }, READY_TIMEOUT_MS)
    }
    socket.onmessage = (ev) => {
      if (epoch !== connEpoch) return
      const frame = decodeFrame(ev.data)
      if (!frame) return
      if (frame.kind === 'data') {
        term?.write(frame.bytes)
        return
      }
      switch (frame.op) {
        case 'ready': {
          clearReady()
          connStatus.value = 'live'
          connError.value = ''
          reconnectAttempt.value = 0
          // 尺寸对账：断连期间用户拖过窗口，这里补发一次 resize 把 pty 拽齐
          const dims = termDims()
          if (dims.cols !== lastSentDims.cols || dims.rows !== lastSentDims.rows) sendResizeFrame(dims.cols, dims.rows)
          term?.focus()
          break
        }
        case 'exit': {
          const code = typeof frame.code === 'number' ? frame.code : null
          row.alive = false
          row.exitCode = code
          // exit 渲染进终端本体（不错序：先前到达的字节已在屏上，这行按序接尾）
          term?.write(`\r\n\x1b[2m[进程已退出${code === null ? '' : `（code ${code}）`}——Kill 清账或新建同型会话]\x1b[0m\r\n`)
          teardownStream()
          break
        }
        default:
          // 前向兼容的未知控制帧：忽略，不影响字节序，也不弹错（契约未扩，不臆造语义）
          break
      }
    }
    socket.onclose = () => {
      if (epoch !== connEpoch) return
      handleDrop('显示通道被对端关闭')
    }
    socket.onerror = () => {
      if (epoch !== connEpoch) return
      handleDrop('传输层报错（GoStream 连接失败）')
    }
  }

  function sendResizeFrame(cols: number, rows: number): void {
    lastSentDims = { cols, rows }
    if (conn && connStatus.value === 'live') conn.send(encodeControlFrame({ op: 'resize', cols, rows }))
  }

  // —— 名录操作 ——
  async function refresh(): Promise<void> {
    const seq = ++listSeq
    listLoading.value = true
    listError.value = ''
    try {
      const rows = ((await TerminalAPI.ListTerminals()) ?? []) as WireTermRow[]
      if (seq !== listSeq) return // 晚到的旧一代：整发丢弃
      mergeLedger(rows)
    } catch (err: unknown) {
      if (seq === listSeq) listError.value = getErrorMessage(err)
    } finally {
      if (seq === listSeq) listLoading.value = false
    }
  }

  /** 名录以后端为唯一真相（账本字段整表重排）；选中项被外部除名则断通道并按前项回落。 */
  function mergeLedger(rows: WireTermRow[]): void {
    const prev = new Map(sessions.value.map((r) => [r.id, r]))
    const out: TermRow[] = []
    for (const w of rows) {
      const id = String(w?.id ?? '')
      if (!id) continue
      const local = prev.get(id)
      const fresh = rowFromWire(w)
      if (local) {
        fresh.exitCode = local.exitCode // 本通道见过的退出码保留（后端只回 alive 布尔，不回 code）
        prev.delete(id)
      }
      out.push(fresh)
    }
    sessions.value = out
    for (const gone of prev.values()) {
      // 本地在册而后表无 = 已被除名（另一入口 Kill 的）：通道随除名释放
      if (gone.id === currentId.value) {
        teardownStream()
        currentId.value = ''
      }
    }
    if (!currentId.value && out.length > 0) selectSession(out[0].id)
  }

  /**
   * knownDims：仅建号路径传入（OpenTerminal 已按此尺寸起 pty，ready 后无需对账补发）；
   * 切会话时缺省置零 = "未知"，ready 后必发一次 resize 把 pty 拽齐。
   */
  function selectSession(id: string, knownDims?: { cols: number; rows: number }): void {
    if (!id || id === currentId.value) return
    if (!sessions.value.some((r) => r.id === id)) return
    // v0 口径：画面归"这一条连接"——主动切会话即清屏重开新通道；
    // 断线重连**不走**这条路（reset 只在此处，"断线不砸已渲染画面"由 spec 钉死）。
    // 从无选中态接入（首挂载自动接入/空态回落）时屏上本无他会话画面，不清。
    const hadPrevSelection = currentId.value !== ''
    teardownStream()
    if (hadPrevSelection) term?.reset()
    currentId.value = id
    lastSentDims = knownDims ?? { cols: 0, rows: 0 }
    const row = sessions.value.find((r) => r.id === id)
    if (row?.alive) openStream() // 已退出的会话：留名录可清账，不再拉显示通道
  }

  async function createSession(profileId: TermProfileId, dir = ''): Promise<void> {
    if (creating.value) return // 建号在途连点只许一次（先闸后问同族纪律）
    openError.value = ''
    const { cols, rows } = termDims()
    creating.value = true
    try {
      const w = (await TerminalAPI.OpenTerminal(profileId, cols, rows, dir)) as WireTermRow | null
      if (!w?.id) return
      const row = rowFromWire(w)
      // 新会话挂尾（tab 条心智）；名录顺序以后端为准，下次 refresh 自然对齐
      sessions.value = [...sessions.value, row]
      // 建号已带尺寸（pty 按此起），ready 后无需对账补发 resize
      selectSession(row.id, { cols, rows })
    } catch (err: unknown) {
      openError.value = getErrorMessage(err) // "没开出这一页终端"只砸 openError 通道，不碰 list/conn
    } finally {
      creating.value = false
    }
  }

  /**
   * 选目录后建会话：Go 侧原生对话框（PickDirectory），取消=静默无事发生
   * （人点了"取消"不是错误，弹红条是把人的操作当故障）；选择器本身抛了
   * 才走 openError 通道如实报。
   */
  async function createSessionWithDir(profileId: TermProfileId): Promise<void> {
    if (creating.value || picking.value) return // 双闸：挑目录期间也不许第二发
    picking.value = true
    openError.value = ''
    let dir = ''
    try {
      dir = String((await TerminalAPI.PickDirectory()) ?? '')
    } catch (err: unknown) {
      openError.value = '打开目录选择器失败：' + getErrorMessage(err)
      return
    } finally {
      picking.value = false
    }
    if (!dir) return
    await createSession(profileId, dir)
  }

  async function renameSession(id: string): Promise<void> {
    const row = sessions.value.find((r) => r.id === id)
    if (!row) return
    const name = await prompt({ title: '重命名终端', label: '会话名称', initialValue: row.title })
    if (name === null) return
    const title = name.trim()
    if (!title) return // 空名不发白趟（usePrompt"空串按取消由调用方判断"口径）
    try {
      await TerminalAPI.RenameTerminal(id, title)
      row.title = title
    } catch (err: unknown) {
      listError.value = getErrorMessage(err)
    }
  }

  /** Kill = 杀会话进程并除账：破坏性动作先闸后问（operation-panel 纪律）。 */
  async function killSession(id: string): Promise<void> {
    const row = sessions.value.find((r) => r.id === id)
    if (!row || row.asking) return
    row.asking = true
    let accepted = false
    try {
      accepted = await confirm({
        title: `Kill 终端「${row.title}」？`,
        description: '立即结束该会话的子进程并从名录除账，不可恢复。断的是进程不是本页——名录里的行会消失，本页画面随选择回落自然让位。',
        tone: 'danger',
        confirmLabel: 'Kill 终端',
      })
    } finally {
      row.asking = false // 确认框异常也不留残闸
    }
    if (!accepted) return
    try {
      await TerminalAPI.KillTerminal(id)
    } catch (err: unknown) {
      listError.value = getErrorMessage(err) // Kill 失败：进程还在、账也还在，名录通道报，不动画面
      return
    }
    const index = sessions.value.findIndex((r) => r.id === id)
    const rest = sessions.value.filter((r) => r.id !== id)
    const wasCurrent = currentId.value === id
    if (wasCurrent) teardownStream()
    sessions.value = rest
    if (wasCurrent) {
      // 选中回落规则（照 AiChat 定死）：被删项前一项，无前项取新首项，空列表回无选中态
      const fallback = rest[Math.max(index - 1, 0)] ?? rest[0]
      currentId.value = ''
      if (fallback) selectSession(fallback.id)
    }
  }

  function reconnectNow(): void {
    if (!currentRow.value?.alive) return
    reconnectAttempt.value = 0 // 手动触发重新给满预算
    connEpoch++ // 在途旧 socket 作废
    clearRetry()
    clearReady()
    openStream()
  }

  // —— 搜索（SearchAddon 薄壳；Ctrl+F 唤起，Enter 下一个 / Shift+Enter 上一个，Esc 关）——
  const searchOpen = ref(false)
  const searchText = ref('')

  function toggleSearch(): void {
    searchOpen.value = !searchOpen.value
    if (searchOpen.value) {
      // 搜索条是 v-if 位：置真后 DOM 要等一拍才有输入框（nextTick 再聚焦）
      void nextTick(() => document.querySelector<HTMLInputElement>('.tm-search-input')?.focus())
    } else {
      term?.focus()
    }
  }

  function runSearch(next: boolean): void {
    const q = searchText.value
    if (!q) return
    if (next) searchAddon?.findNext(q)
    else searchAddon?.findPrevious(q)
  }

  // —— xterm 装配 ——
  function fitSafely(): void {
    try { fitAddon?.fit() } catch { /* 容器不可测（隐藏/零尺寸）：fit 自己有 isNaN 防线，这里只是兜底 */ }
  }

  function mountTerminal(el: HTMLElement): void {
    if (term) return
    term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: monoFontFamily(),
      scrollback: 5000,
      theme: readTermTheme(),
      // unicode11 addon 走 term.unicode.activeVersion 设定 CJK 宽度；该属性在
      // xterm 6.0.0 仍属 proposed API，不开此开关直接抛
      // "You must set the allowProposedApi option to true to use proposed API"
      // 使整模块渲染失败（真机首跑即炸，测试因整体替桩 xterm 而没接住）。
      // 我们只碰 unicode 这一项，不开任何其它 proposed 行为。
      allowProposedApi: true,
    })
    fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.loadAddon(new Unicode11Addon())
    // CJK 宽度按 Unicode 11（中文对齐的地基，不是装饰）。版本串是 addon
    // 注册的 '11'——写成 '11.0' 会被真 xterm 判 unknown Unicode version
    // （真机第二炸；README 口径也是 '11'）。
    term.unicode.activeVersion = '11'
    searchAddon = new SearchAddon()
    term.loadAddon(searchAddon)
    // URL 可点：走 Wails Browser 出外部浏览器（webview 里 window.open 不承诺可用）
    term.loadAddon(new WebLinksAddon((_event, uri) => { void Browser.OpenURL(uri) }))
    // Ctrl/Cmd+F 唤起页内搜索而非吞给 webview 原生查找；其余按键原样放行给 pty
    term.attachCustomKeyEventHandler((e) => {
      if (e.type === 'keydown' && (e.ctrlKey || e.metaKey) && !e.altKey && e.code === 'KeyF') {
        toggleSearch()
        return false
      }
      return true
    })
    term.open(el)
    fitSafely()
    dataDisposable = term.onData((text) => {
      // 输入只在通道活时上行；断线期按键丢弃是 v0 的诚实（假装排队会制造错序幻觉）
      if (conn && connStatus.value === 'live') conn.send(encodeDataFrame(text))
    })
    resizeDisposable = term.onResize(({ cols, rows }) => {
      // 150ms 防抖只发末值：拖窗口期间不轰炸 resize 帧
      if (resizeTimer !== null) clearTimeout(resizeTimer)
      resizeTimer = setTimeout(() => {
        resizeTimer = null
        sendResizeFrame(cols, rows)
      }, RESIZE_DEBOUNCE_MS)
    })
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => fitSafely())
      resizeObserver.observe(el)
    }
    if (typeof MutationObserver !== 'undefined') {
      // 明暗/色板双轴翻转即时重取 token（useTheme 落点就是 documentElement 的 data-*）
      themeObserver = new MutationObserver(() => {
        if (term) term.options.theme = readTermTheme()
      })
      themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'data-accent'] })
    }
  }

  function disposeTerminal(): void {
    teardownStream()
    if (resizeTimer !== null) { clearTimeout(resizeTimer); resizeTimer = null }
    dataDisposable?.dispose(); dataDisposable = null
    resizeDisposable?.dispose(); resizeDisposable = null
    resizeObserver?.disconnect(); resizeObserver = null
    themeObserver?.disconnect(); themeObserver = null
    term?.dispose()
    term = null
    fitAddon = null
    searchAddon = null
  }

  // KeepAlive：停用即断通道（进程活着，"窗口重开需重新连接"），激活自动续接。
  onActivated(() => {
    if (currentRow.value?.alive && connStatus.value === 'detached') openStream()
  })
  onDeactivated(() => {
    if (connStatus.value !== 'detached') teardownStream()
  })
  onScopeDispose(() => {
    if (resizeTimer !== null) clearTimeout(resizeTimer)
    teardownStream()
  })

  return {
    // 名录与选择
    sessions,
    currentId,
    currentRow,
    listLoading,
    listError,
    openError,
    creating,
    picking,
    refresh,
    selectSession,
    createSession,
    createSessionWithDir,
    renameSession,
    killSession,
    // 通道状态
    connStatus,
    connNote,
    connError,
    canReconnect,
    reconnectNow,
    // 搜索
    searchOpen,
    searchText,
    toggleSearch,
    runSearch,
    closeSearch: () => { searchOpen.value = false; term?.focus() },
    // xterm 引擎
    mountTerminal,
    disposeTerminal,
  }
}
