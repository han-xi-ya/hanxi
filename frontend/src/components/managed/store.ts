// ============================================================================
// 托管控制台运行时 store（Wave 5 · 批 0；共享契约增强批扩装）
//
// 由 ManagedConsoleShell（或单独挂载的控制条/版本面板）在组件 setup 期内调用
// useManagedConsole(adapter) 创建：集中持有快照/版本区/下载进度 map/busy 闩，
// 并完成四类接线——实例态订阅、进度订阅、2.5s 状态轮询、1s uptime ticker。
// 各视图此前逐字同构的 ~120 行"状态+编排"段自此单源。
//
// 增强批新增编排（全部向后兼容）：
//  - runControl/runToggle/runDownload/runChannel/runVariant 统一消费
//    ManagedActionResult 的 message/reloadVersions/activeVersion——控制动词
//    成功回执带 reloadVersions 时并列复刷版本区（mangodisk 启动双复刷、
//    markeron toggle 版本徽标即时、papertodo 变体切换列表刷新自此回归 store，
//    视图侧补丁退役）；
//  - busy 闩 = 动作在途 OR adapter.dangerBusy（⑦ 强制结束联动）；
//  - banner/hint 新增第二参版本区投影（②：{installed, releases, active}，
//    store 单源直填，少收第二参的旧 adapter 零改动）；
//  - primaryLabel/quitLabel 解析 label 联合词形（① 动态主钮）；
//  - statusTone 色档覆写（⑧）；
//  - 轮询停止（KeepAlive 停用/卸载）→ uptime 归零（⑨，recordly 现案收编）。
//
// 纪律：
//  - 本文件只编排，不解读业务：状态词/按钮声明/文案全部来自 adapter 投影。
//  - 失败 toast 前缀是跨模块逐字同形词（自 ccswitch/markeron 等视图原样收编），
//    集中于此单一来源，且支持 copy.errorPrefix 逐动词覆写（⑥，「安装失败: 」
//    词表回契约）；成功回执文案经 ManagedActionResult.message 上抛。
//  - 控制动词成功后恒刷一次状态快照（对齐现状 openWindow/quit 的双分支刷新）。
// ============================================================================

import { computed, onDeactivated, onMounted, onUnmounted, reactive, ref, shallowRef } from 'vue'
import type {
  ManagedActionResult,
  ManagedControlVerb,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedSnapshot,
  ManagedVersionDialect,
  ManagedVersionRecord,
  NormalizedProgress,
} from './adapter'
import { sameSnapshot, sameVersionOf } from './adapter'
import { loadManagedVersions } from '../../composables/loadManagedVersions'
import { usePolling } from '../../composables/usePolling'
import { useToast } from '../../composables/useToast'
import { getErrorMessage } from '../../utils/errors'
import { SUMMARY_META, toolStateMeta } from '../../constants/status'

/** 动作失败 toast 前缀（逐字沿用视图现词；primary/toggle 失败为裸错误串，无前缀）。 */
const ACTION_ERROR_PREFIX = {
  quit: '退出失败: ',
  download: '下载失败: ',
  setActive: '设置失败: ',
  remove: '卸载失败: ',
  import: '导入失败: ',
  openDir: '打开目录失败: ',
  channel: '切换通道失败: ',
  variant: '设置失败: ',
} as const

/** 状态轮询间隔（全托管视图现状统一 2500ms；uptime 每秒从 startedAt 重算）。 */
const STATUS_POLL_MS = 2500
const UPTIME_TICK_MS = 1000
/** 下载完成票据的表内驻留时长（现状 800ms 后清除并重拉版本列表）。 */
const DONE_TICK_RETIRE_MS = 800

/**
 * 控制条/版本面板消费的控制台共享状态与动作面（reactive 解包后的现值视图）。
 * V 为已装版本记录方言字段包（增强批③，缺省纯基型）；宽型消费方
 * （ControlBar/Shell 等）按默认实例化，typed store 传入宽 prop 处协变兼容。
 */
export interface ManagedConsoleStore<V = ManagedVersionDialect> {
  readonly snap: ManagedSnapshot | null
  /** busy 闩：声明式动作在途 OR adapter.dangerBusy 联动（⑦）。 */
  readonly busy: boolean
  readonly uptimeSec: number
  readonly releases: ManagedReleaseRecord[]
  readonly installed: ManagedVersionRecord<V>[]
  readonly activeVersion: string
  readonly loading: boolean
  listError: string
  readonly downloading: Record<string, NormalizedProgress>
  /**
   * 状态真相三态（P0 批 3·4.1/4.2）：
   *  - statusError：状态 RPC 最近一次失败——快照仍是旧事实，视图须降灰
   *    呈现「暂不可确认」，不得冒充实时；
   *  - lastStatusAt：最后一次成功取得状态/收到实例事件的时刻（ms，0=从未）；
   *  - localResolved：本地版本区至少完成过一次回写——未解析前禁止把
   *    `installed.length===0` 判成首用空态。
   */
  readonly statusError: boolean
  readonly lastStatusAt: number
  readonly localResolved: boolean

  readonly state: string
  /** N43 状态真相：本地事实已解析且无任何托管版本、又无在跑实例（含外部）时，
   *  呈现口径从"未运行"（暗示"可以起"）回落为中性"未安装"。 */
  readonly notInstalled: boolean
  /** 未安装态「启动」直落版本页的接线钩子（Shell 赋值；runControl 消费）。 */
  goVersions: (() => void) | null
  readonly stateText: string
  /** 状态灯色档词（⑧，缺省 = state）。 */
  readonly statusTone: string
  readonly runningVersion: string
  readonly isRunningOrStarting: boolean
  readonly isExternal: boolean
  readonly banner: { tone: 'ok' | 'info' | 'warn' | 'error'; text: string } | null
  readonly hint: string | null
  /** 启停钮面词（①：label 字符串或 (state, snap) 纯函数的统一解析结果）。 */
  readonly primaryLabel: string
  readonly quitLabel: string

  /**
   * 注：动作面一律方法语法声明——strictFunctionTypes 下属性函数按逆变检查，
   * 方法语法保留双变，使「带方言 V 的 store」可直接喂进宽型 ManagedConsoleStore prop。
   */
  load(): Promise<void>
  refresh(): Promise<void>
  progressKeyOf(release: ManagedReleaseRecord): string
  runControl(which: 'primary' | 'quit'): Promise<void>
  /** 扩展槽动作统一占用共享 busy 闩，避免自绘动作直接改写只读派生 busy。 */
  runExclusive<T>(action: () => PromiseLike<T>): Promise<T | undefined>
  /** toggle 槽动词（markeron 六态钮经 store 单源编排）。 */
  runToggle(): Promise<void>
  /** channel 槽切换；true=成功（调用方翻转选中态）。 */
  runChannel(next: string): Promise<boolean>
  /** variant 槽切换；true=成功。 */
  runVariant(next: string): Promise<boolean>
  runDownload(release: ManagedReleaseRecord): Promise<void>
  runSetActive(info: ManagedVersionRecord<V>): Promise<void>
  runRemove(info: ManagedVersionRecord<V>): Promise<void>
  runImport(): Promise<void>
  runOpenDir(info: ManagedVersionRecord<V>): Promise<void>
}

/** 启停钮面词解析（label 联合词形；ControlBar 与自定义视图同源取词）。 */
function resolveVerbLabel(
  verb: ManagedControlVerb | undefined,
  state: string,
  snap: ManagedSnapshot | null,
): string {
  if (!verb) return ''
  return typeof verb.label === 'function' ? verb.label(state, snap) : verb.label
}

/**
 * 在组件 setup 同步期内创建控制台 store（内部含 usePolling/useWailsEvent
 * 接线与 onMounted 首拉；宿主卸载自动停轮询、注销订阅）。
 * V 为已装版本记录方言字段包（增强批③）：自 adapter 泛型推断，store.installed
 * 直接携带方言字段类型（paseo verifiedHash / mangodisk integrity 族视图去 cast）。
 */
export function useManagedConsole<S extends ManagedSnapshot = ManagedSnapshot, V = ManagedVersionDialect>(
  adapter: ManagedModuleAdapter<S, V>,
): ManagedConsoleStore<V> {
  const { showToast } = useToast()

  const snap = ref<ManagedSnapshot | null>(null)
  const actionBusy = ref(false)
  const uptimeSec = ref(0)

  const releases = ref<ManagedReleaseRecord[]>([])
  // installed 用 shallowRef：泛型交叉型经 ref 深解包会折叠掉方言 V 分量；
  // 版本区整组替换（load/settle 从不原地改写成员），shallow 语义正合。
  const installed = shallowRef<ManagedVersionRecord<V>[]>([])
  const activeVersion = ref('')
  const loading = ref(false)
  const listError = ref('')
  const downloading = ref<Record<string, NormalizedProgress>>({})
  /** load 请求代次：后发请求拥有写权，防通道切换后的旧响应反向覆盖。 */
  let loadGeneration = 0
  // 状态真相三态（见 ManagedConsoleStore 注记）：statusError/lastStatusAt 描述
  // "快照是否可作实时口径"；localResolved 门控首用空态判定。
  const statusError = ref(false)
  const lastStatusAt = ref(0)
  const localResolved = ref(false)

  // Shell 接线钩子（见 ManagedConsoleStore.goVersions）：局部可变 + 访问器外露。
  let goVersionsFn: (() => void) | null = null

  // ---------- 派生投影（只读 adapter/快照，零本地状态推断） ----------
  const state = computed(() => snap.value?.state ?? '')
  // 共享件按宽型持有快照；投影位 adapter 声明其收窄 S——方法位双变，
  // 运行期同一对象，类型收敛仅此一处 cast，视图零感知。
  const narrowSnap = computed(() => snap.value as S | null)
  // N43：安装维度先于运行维度——未装任何版本且无在跑实例（running/starting/
  // external/quitting/failed 各有自己的事实要讲）时，"stopped/空"不得渲染成
  // "未运行"。判定收在共享契约层单点，21+ 视图零方言受益。
  const notInstalled = computed(
    () =>
      localResolved.value &&
      installed.value.length === 0 &&
      !['running', 'starting', 'external', 'quitting', 'failed'].includes(state.value),
  )
  const stateText = computed(() => {
    if (notInstalled.value) return SUMMARY_META['not-installed'] // 与四维摘要冻结词表同源
    return narrowSnap.value && adapter.stateText ? adapter.stateText(narrowSnap.value) : toolStateMeta(state.value).text
  })
  /** 状态灯色档（⑧）：缺省随 state 五态；adapter.statusTone 覆写（bili23 running+hidden→warn）。 */
  const statusTone = computed(() =>
    narrowSnap.value && adapter.statusTone ? adapter.statusTone(narrowSnap.value) : state.value,
  )
  const runningVersion = computed(() => snap.value?.version ?? '')
  const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
  const isExternal = computed(() => state.value === 'external')
  /** busy 闩（⑦）：声明式动作在途 OR 危险动作 dangerBusy 联动。 */
  const busy = computed(() => actionBusy.value || adapter.dangerBusy?.value === true)

  // ②：banner/hint 第二参版本区投影（store 单源直填，少收第二参的旧 adapter 零感知）
  const versionCtx = computed(() => ({
    installed: installed.value,
    releases: releases.value,
    active: activeVersion.value,
  }))
  const banner = computed(() =>
    narrowSnap.value && adapter.banner ? adapter.banner(narrowSnap.value, versionCtx.value) : null,
  )
  const hint = computed(() => {
    if (notInstalled.value) {
      return '尚未安装任何托管版本：「启动」将直接带你到版本管理页，先下载或导入。'
    }
    return narrowSnap.value && adapter.hint ? adapter.hint(narrowSnap.value, versionCtx.value) : null
  })
  /** 启停钮面词（①）：label 联合词形统一解析，声明式控制条与自定义视图同源。 */
  const primaryLabel = computed(() => resolveVerbLabel(adapter.control?.primary, state.value, snap.value))
  const quitLabel = computed(() => resolveVerbLabel(adapter.control?.quit, state.value, snap.value))

  // ---------- 数据加载 ----------
  async function load(): Promise<void> {
    const generation = ++loadGeneration
    await loadManagedVersions({
      remote: () => adapter.versions.listReleases(),
      local: () => adapter.versions.listInstalled(),
      active: adapter.versions.getActive ?? (async () => ''),
      setRemote: (value) => {
        if (generation === loadGeneration) releases.value = value
      },
      setLocal: (value) => {
        if (generation === loadGeneration) installed.value = value
      },
      setActive: (value) => {
        if (generation === loadGeneration) activeVersion.value = value
      },
      setLoading: (value) => {
        if (generation === loadGeneration) loading.value = value
      },
      setError: (value) => {
        if (generation === loadGeneration) listError.value = value
      },
    })
    // loadManagedVersions 在本地（listInstalled+getActive）落地后 resolve：
    // 世代未过期即宣布"本地已解析"，此后空列表才是可信的"确无安装"。
    if (generation === loadGeneration) localResolved.value = true
  }

  async function refresh(): Promise<void> {
    try {
      const next = (await adapter.getStatus()) ?? snap.value
      // 空转归零（perf）：内容级无差异的轮询回包不替换快照引用——21+ 托管视图
      // 由此在"一切如旧"时整轮零渲染批；有差异（含 RPC 交回 null 后的清空）照常替换。
      if (!sameSnapshot(snap.value, next)) snap.value = next
      statusError.value = false
      lastStatusAt.value = Date.now()
    } catch (e) {
      // 4.1：保留上次快照但**如实标注不可确认**——旧快照不得冒充实时状态。
      statusError.value = true
      console.warn('[managed] GetStatus failed:', getErrorMessage(e))
    }
  }

  // ---------- 动作编排 ----------
  function errorPrefix(which: keyof typeof ACTION_ERROR_PREFIX): string {
    return adapter.copy?.errorPrefix?.[which] ?? ACTION_ERROR_PREFIX[which]
  }

  /** 回执结算：message 弹 toast、activeVersion 即时迁移高亮；返回是否需重拉版本区。 */
  function settle(res: ManagedActionResult | void | undefined): boolean {
    if (res?.message !== undefined) showToast(res.message)
    if (res?.activeVersion !== undefined) activeVersion.value = res.activeVersion
    return res?.reloadVersions === true
  }

  async function runControl(which: 'primary' | 'quit'): Promise<void> {
    const verb: ManagedControlVerb | undefined =
      which === 'primary' ? adapter.control?.primary : adapter.control?.quit
    if (!verb || actionBusy.value) return
    // N43②：未安装态的「启动」不发起注定失败的 RPC，直落版本管理页；
    // 独立挂载（无 Shell 接线）时降级为 toast 指路。
    if (which === 'primary' && notInstalled.value) {
      if (goVersionsFn) goVersionsFn()
      else showToast('尚未安装任何版本，请到「版本管理」下载或导入')
      return
    }
    actionBusy.value = true
    try {
      const res = await verb.run()
      if (settle(res)) await load()
      await refresh()
    } catch (e) {
      showToast((which === 'quit' ? errorPrefix('quit') : '') + getErrorMessage(e))
      // 个别冷启动检查会在拒绝启动前刷新版本事实（如 MangoDisk 完整性）；
      // primary 失败后重拉版本区，避免 banner/版本卡继续展示旧快照。
      if (which === 'primary') await load()
      await refresh()
    } finally {
      actionBusy.value = false
    }
  }

  /**
   * 自绘扩展动作（reset/快捷方式/专属控制钮）共享单飞闩。
   * busy 本身是 actionBusy ∨ dangerBusy 的只读投影，消费方不得直接赋值。
   */
  async function runExclusive<T>(action: () => PromiseLike<T>): Promise<T | undefined> {
    if (busy.value) return undefined
    actionBusy.value = true
    try {
      return await action()
    } finally {
      actionBusy.value = false
    }
  }

  /** toggle 槽动词（markeron 六态钮）：成功回执/重拉/刷态统一；失败裸串 toast（原口径）。 */
  async function runToggle(): Promise<void> {
    const toggle = adapter.toggle
    if (!toggle || actionBusy.value) return
    actionBusy.value = true
    try {
      const res = await toggle.run()
      if (settle(res)) await load()
      await refresh()
    } catch (e) {
      showToast(getErrorMessage(e))
      await refresh()
    } finally {
      actionBusy.value = false
    }
  }

  /** channel 槽切换（⑧ 共享块/视图单选卡共用）：返回 true=切换成功（调用方翻转选中态）。 */
  async function runChannel(next: string): Promise<boolean> {
    const channel = adapter.channel
    if (!channel || busy.value) return false
    actionBusy.value = true
    try {
      const res = await channel.set(next)
      if (settle(res)) await load()
      return true
    } catch (e) {
      showToast(`${errorPrefix('channel')}${getErrorMessage(e)}`)
      return false
    } finally {
      actionBusy.value = false
    }
  }

  /** variant 槽切换（papertodo 变体卡）：同上语义，失败前缀缺省「设置失败: 」（原逐字现词）。 */
  async function runVariant(next: string): Promise<boolean> {
    const variant = adapter.variant
    if (!variant || busy.value) return false
    actionBusy.value = true
    try {
      const res = await variant.set(next)
      if (settle(res)) await load()
      return true
    } catch (e) {
      showToast(`${errorPrefix('variant')}${getErrorMessage(e)}`)
      return false
    } finally {
      actionBusy.value = false
    }
  }

  async function runDownload(rel: ManagedReleaseRecord): Promise<void> {
    const key = progressKeyOf(rel)
    const existing = downloading.value[key]
    if (busy.value || (existing && existing.stage !== 'error')) return
    actionBusy.value = true
    // RPC 返还前先挂 pending 票据，封住双击窗口；首个真实进度事件会覆盖它。
    downloading.value = {
      ...downloading.value,
      [key]: { key, stage: 'resolve', done: 0, total: 0, message: '正在创建下载任务…' },
    }
    try {
      const res = await adapter.versions.download(rel)
      const reload = settle(res)
      if (reload) {
        await load()
        // already-installed 等同步终态无进度事件，以重拉后的已装事实收票。
        // 4.3：比较走版本互认唯一口径（sameVersionOf）——预发布 tag 与核心
        // 版本判等的模块（recordly/paseo 方言）不再"远程行已装、票据永挂"。
        if (installed.value.some((v) => sameVersionOf(adapter.versions, v.version, rel.version))) {
          const next = { ...downloading.value }
          delete next[key]
          downloading.value = next
        }
      }
    } catch (e) {
      const message = `${errorPrefix('download')}${getErrorMessage(e)}`
      showToast(message)
      downloading.value = {
        ...downloading.value,
        [key]: { key, stage: 'error', done: 0, total: 0, message },
      }
    } finally {
      actionBusy.value = false
    }
  }

  async function runSetActive(v: ManagedVersionRecord<V>): Promise<void> {
    if (!adapter.versions.setActive || busy.value) return
    actionBusy.value = true
    try {
      const res = await adapter.versions.setActive(v.version)
      if (settle(res)) await load()
    } catch (e) {
      showToast(`${errorPrefix('setActive')}${getErrorMessage(e)}`)
      // ⑥：设版失败恒重拉版本区核对后端真实值（原 paseo 差异③「自捕获重拉」
      // 的契约化表达——词源单源 + 安全网通用化，不再由 adapter 逐模块抄写）。
      await load()
    } finally {
      actionBusy.value = false
    }
  }

  async function runRemove(v: ManagedVersionRecord<V>): Promise<void> {
    if (busy.value) return
    actionBusy.value = true
    try {
      const res = await adapter.versions.remove(v)
      if (settle(res)) await load()
    } catch (e) {
      showToast(`${errorPrefix('remove')}${getErrorMessage(e)}`)
    } finally {
      actionBusy.value = false
    }
  }

  async function runImport(): Promise<void> {
    const importLocal = adapter.versions.importLocal
    if (!importLocal || actionBusy.value) return
    actionBusy.value = true
    try {
      const res = await importLocal()
      if (settle(res)) await load()
    } catch (e) {
      showToast(`${errorPrefix('import')}${getErrorMessage(e)}`)
    } finally {
      actionBusy.value = false
    }
  }

  async function runOpenDir(v: ManagedVersionRecord<V>): Promise<void> {
    if (busy.value) return
    actionBusy.value = true
    try {
      settle(await adapter.versions.openDir(v))
    } catch (e) {
      showToast(`${errorPrefix('openDir')}${getErrorMessage(e)}`)
    } finally {
      actionBusy.value = false
    }
  }

  function progressKeyOf(rel: ManagedReleaseRecord): string {
    return adapter.versions.progressKey ? adapter.versions.progressKey(rel) : rel.version
  }

  // ---------- 事件与轮询接线（setup 期一次成型） ----------
  adapter.subscribeInstanceState((s) => {
    snap.value = s
    if (s.state !== 'running') uptimeSec.value = 0
    // 实例事件是推送来的新鲜事实：清 stale。
    statusError.value = false
    lastStatusAt.value = Date.now()
  })

  adapter.subscribeProgress((p) => {
    adapter.onProgress?.(p)
    // custom 版本体自行持有票据、刷新与清理时序（VS Code 双表、Snipaste
    // 事实核验票据）；共享 store 仅保证事件单订阅并转交 onProgress。
    if (adapter.versions.orchestration === 'custom') return
    downloading.value = { ...downloading.value, [p.key]: p }
    if (p.stage === 'done') {
      setTimeout(() => {
        const next = { ...downloading.value }
        delete next[p.key]
        downloading.value = next
      }, DONE_TICK_RETIRE_MS)
      void load()
    }
  })

  usePolling(refresh, STATUS_POLL_MS)
  usePolling(() => {
    // 运行时长秒表：每秒从快照 startedAt 重算（KeepAlive 停用期由 usePolling 暂停）
    if (snap.value?.state === 'running' && snap.value.startedAt) {
      const started = new Date(snap.value.startedAt).getTime()
      if (!Number.isNaN(started)) {
        uptimeSec.value = Math.max(0, Math.floor((Date.now() - started) / 1000))
      }
    }
  }, UPTIME_TICK_MS)
  // ⑨：轮询停止（KeepAlive 停用/卸载）即 uptime 归零——对齐 recordly 迁移前
  // stopTimers 语义。用停用/卸载钩子而非 watch(isPolling)：失活组件的 watch
  // 回调被暂停，归零要同步发生在停用当下。
  onDeactivated(() => {
    uptimeSec.value = 0
  })
  onUnmounted(() => {
    uptimeSec.value = 0
  })

  onMounted(() => {
    if (adapter.versions.orchestration !== 'custom') void load()
  })

  return reactive({
    snap,
    busy,
    uptimeSec,
    releases,
    installed,
    activeVersion,
    loading,
    listError,
    downloading,
    statusError,
    lastStatusAt,
    localResolved,
    notInstalled,
    state,
    stateText,
    get goVersions() {
      return goVersionsFn
    },
    set goVersions(fn: (() => void) | null) {
      goVersionsFn = fn
    },
    statusTone,
    runningVersion,
    isRunningOrStarting,
    isExternal,
    banner,
    hint,
    primaryLabel,
    quitLabel,
    load,
    refresh,
    progressKeyOf,
    runControl,
    runExclusive,
    runToggle,
    runChannel,
    runVariant,
    runDownload,
    runSetActive,
    runRemove,
    runImport,
    runOpenDir,
  }) as unknown as ManagedConsoleStore<V>
}
