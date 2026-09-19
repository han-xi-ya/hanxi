// ============================================================================
// 托管控制台运行时 store（Wave 5 · 批 0）
//
// 由 ManagedConsoleShell（或单独挂载的控制条/版本面板）在组件 setup 期内调用
// useManagedConsole(adapter) 创建：集中持有快照/版本区/下载进度 map/busy 闩，
// 并完成四类接线——实例态订阅、进度订阅、2.5s 状态轮询、1s uptime ticker。
// 各视图此前逐字同构的 ~120 行"状态+编排"段自此单源。
//
// 纪律：
//  - 本文件只编排，不解读业务：状态词/按钮声明/文案全部来自 adapter 投影。
//  - 失败 toast 前缀是跨模块逐字同形词（自 ccswitch/markeron 等视图原样收编），
//    集中于此单一来源；成功回执文案经 ManagedActionResult.message 上抛。
//  - 控制动词成功后恒刷一次状态快照（对齐现状 openWindow/quit 的双分支刷新）。
// ============================================================================

import { computed, onMounted, reactive, ref } from 'vue'
import type {
  ManagedActionResult,
  ManagedControlVerb,
  ManagedModuleAdapter,
  ManagedReleaseRecord,
  ManagedSnapshot,
  ManagedVersionRecord,
  NormalizedProgress,
} from './adapter'
import { loadManagedVersions } from '../../composables/loadManagedVersions'
import { usePolling } from '../../composables/usePolling'
import { useToast } from '../../composables/useToast'
import { getErrorMessage } from '../../utils/errors'
import { toolStateMeta } from '../../constants/status'

/** 动作失败 toast 前缀（逐字沿用视图现词；primary 失败为裸错误串，无前缀）。 */
const ACTION_ERROR_PREFIX = {
  quit: '退出失败: ',
  download: '下载失败: ',
  setActive: '设置失败: ',
  remove: '卸载失败: ',
  import: '导入失败: ',
  openDir: '打开目录失败: ',
} as const

/** 状态轮询间隔（全托管视图现状统一 2500ms；uptime 每秒从 startedAt 重算）。 */
const STATUS_POLL_MS = 2500
const UPTIME_TICK_MS = 1000
/** 下载完成票据的表内驻留时长（现状 800ms 后清除并重拉版本列表）。 */
const DONE_TICK_RETIRE_MS = 800

/** 控制条/版本面板消费的控制台共享状态与动作面（reactive 解包后的现值视图）。 */
export interface ManagedConsoleStore {
  snap: ManagedSnapshot | null
  busy: boolean
  uptimeSec: number
  releases: ManagedReleaseRecord[]
  installed: ManagedVersionRecord[]
  activeVersion: string
  loading: boolean
  listError: string
  downloading: Record<string, NormalizedProgress>

  state: string
  stateText: string
  runningVersion: string
  isRunningOrStarting: boolean
  isExternal: boolean
  banner: { tone: 'ok' | 'info' | 'warn' | 'error'; text: string } | null
  hint: string | null

  load: () => Promise<void>
  refresh: () => Promise<void>
  progressKeyOf: (release: ManagedReleaseRecord) => string
  runControl: (which: 'primary' | 'quit') => Promise<void>
  runDownload: (release: ManagedReleaseRecord) => Promise<void>
  runSetActive: (info: ManagedVersionRecord) => Promise<void>
  runRemove: (info: ManagedVersionRecord) => Promise<void>
  runImport: () => Promise<void>
  runOpenDir: (info: ManagedVersionRecord) => Promise<void>
}

/**
 * 在组件 setup 同步期内创建控制台 store（内部含 usePolling/useWailsEvent
 * 接线与 onMounted 首拉；宿主卸载自动停轮询、注销订阅）。
 */
export function useManagedConsole(adapter: ManagedModuleAdapter): ManagedConsoleStore {
  const { showToast } = useToast()

  const snap = ref<ManagedSnapshot | null>(null)
  const busy = ref(false)
  const uptimeSec = ref(0)

  const releases = ref<ManagedReleaseRecord[]>([])
  const installed = ref<ManagedVersionRecord[]>([])
  const activeVersion = ref('')
  const loading = ref(false)
  const listError = ref('')
  const downloading = ref<Record<string, NormalizedProgress>>({})

  // ---------- 派生投影（只读 adapter/快照，零本地状态推断） ----------
  const state = computed(() => snap.value?.state ?? '')
  const stateText = computed(() =>
    snap.value && adapter.stateText ? adapter.stateText(snap.value) : toolStateMeta(state.value).text,
  )
  const runningVersion = computed(() => snap.value?.version ?? '')
  const isRunningOrStarting = computed(() => state.value === 'running' || state.value === 'starting')
  const isExternal = computed(() => state.value === 'external')
  const banner = computed(() => (snap.value && adapter.banner ? adapter.banner(snap.value) : null))
  const hint = computed(() => (snap.value && adapter.hint ? adapter.hint(snap.value) : null))

  // ---------- 数据加载 ----------
  async function load(): Promise<void> {
    await loadManagedVersions({
      remote: () => adapter.versions.listReleases(),
      local: () => adapter.versions.listInstalled(),
      active: adapter.versions.getActive ?? (async () => ''),
      setRemote: (value) => {
        releases.value = value
      },
      setLocal: (value) => {
        installed.value = value
      },
      setActive: (value) => {
        activeVersion.value = value
      },
      setLoading: (value) => {
        loading.value = value
      },
      setError: (value) => {
        listError.value = value
      },
    })
  }

  async function refresh(): Promise<void> {
    try {
      snap.value = (await adapter.getStatus()) ?? snap.value
    } catch (e) {
      // 轮询/动作后刷新静默失败：保留上次快照（视图现状口径）
      console.warn('[managed] GetStatus failed:', getErrorMessage(e))
    }
  }

  // ---------- 动作编排 ----------
  function settle(res: ManagedActionResult | void | undefined): void {
    if (res?.message !== undefined) showToast(res.message)
    if (res?.activeVersion !== undefined) activeVersion.value = res.activeVersion
  }

  async function runControl(which: 'primary' | 'quit'): Promise<void> {
    const verb: ManagedControlVerb | undefined =
      which === 'primary' ? adapter.control?.primary : adapter.control?.quit
    if (!verb || busy.value) return
    busy.value = true
    try {
      settle(await verb.run())
      await refresh()
    } catch (e) {
      showToast((which === 'quit' ? ACTION_ERROR_PREFIX.quit : '') + getErrorMessage(e))
      await refresh()
    } finally {
      busy.value = false
    }
  }

  async function runDownload(rel: ManagedReleaseRecord): Promise<void> {
    try {
      const res = await adapter.versions.download(rel)
      settle(res)
      if (res?.reloadVersions) await load()
    } catch (e) {
      showToast(`${ACTION_ERROR_PREFIX.download}${getErrorMessage(e)}`)
    }
  }

  async function runSetActive(v: ManagedVersionRecord): Promise<void> {
    if (!adapter.versions.setActive) return
    try {
      const res = await adapter.versions.setActive(v.version)
      settle(res)
      if (res?.reloadVersions) await load()
    } catch (e) {
      showToast(`${ACTION_ERROR_PREFIX.setActive}${getErrorMessage(e)}`)
    }
  }

  async function runRemove(v: ManagedVersionRecord): Promise<void> {
    try {
      const res = await adapter.versions.remove(v)
      settle(res)
      if (res?.reloadVersions) await load()
    } catch (e) {
      showToast(`${ACTION_ERROR_PREFIX.remove}${getErrorMessage(e)}`)
    }
  }

  async function runImport(): Promise<void> {
    const importLocal = adapter.versions.importLocal
    if (!importLocal || busy.value) return
    busy.value = true
    try {
      const res = await importLocal()
      settle(res)
      if (res?.reloadVersions) await load()
    } catch (e) {
      showToast(`${ACTION_ERROR_PREFIX.import}${getErrorMessage(e)}`)
    } finally {
      busy.value = false
    }
  }

  async function runOpenDir(v: ManagedVersionRecord): Promise<void> {
    try {
      settle(await adapter.versions.openDir(v))
    } catch (e) {
      showToast(`${ACTION_ERROR_PREFIX.openDir}${getErrorMessage(e)}`)
    }
  }

  function progressKeyOf(rel: ManagedReleaseRecord): string {
    return adapter.versions.progressKey ? adapter.versions.progressKey(rel) : rel.version
  }

  // ---------- 事件与轮询接线（setup 期一次成型） ----------
  adapter.subscribeInstanceState((s) => {
    snap.value = s
    if (s.state !== 'running') uptimeSec.value = 0
  })

  adapter.subscribeProgress((p) => {
    adapter.onProgress?.(p)
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

  onMounted(() => {
    void load()
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
    state,
    stateText,
    runningVersion,
    isRunningOrStarting,
    isExternal,
    banner,
    hint,
    load,
    refresh,
    progressKeyOf,
    runControl,
    runDownload,
    runSetActive,
    runRemove,
    runImport,
    runOpenDir,
  }) as ManagedConsoleStore
}
