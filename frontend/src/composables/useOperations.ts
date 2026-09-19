// 统一 Operation 观察面投影单一来源 composable（Wave 4）：
// 只读 AppService.ListOperations()（Active queued/running + Recent(30) 终态与
// resumable 回灌合并去重），operation:changed 到达只重拉、不本地推导业务状态——
// 消费纪律与 useModuleCatalog 一致：缓存的只是投影副本，事务真相在后端 journal/Hub。
//
// 模块级单例（与 useModuleCatalog/useTheme 同一模式）：事件订阅随模块存活于
// 应用全生命周期，视图卸载后缓存仍被事件保鲜，二次进入零冷启动。
import { computed, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { Operation } from '../../bindings/hanxi/internal/extapi/models'
import { getErrorMessage } from '../utils/errors'

/** operation:changed 突发合并窗口（ms）：一次事务步进/收口可能连发多条广播，只重拉一次。 */
const EVENT_REFRESH_DEBOUNCE_MS = 300

/**
 * resumable 回灌记录的展示 ID 前缀（后端 packages/go/operation/hub.go 合成记录
 * `ID: "resumed-" + txnID`）。收口通道一律消费契约字段 `op.txnId`（裸事务 ID），
 * 不再从展示 ID 剥前缀反推——前缀降级路径仅保留给旧后端未回传 txnId 的兼容兜底。
 */
const RESUMABLE_ID_PREFIX = 'resumed-'

// —— 模块级单例状态 ——
const operations = ref<Operation[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
/** 是否已成功完成过首拉：区分"尚无投影"与"有数据时的后台刷新失败（stale）"。 */
const loaded = ref(false)

/** in-flight 单飞：并发 refresh() 合流到同一次拉取，不产生交错覆盖。 */
let inflight: Promise<void> | null = null
let debounceTimer: ReturnType<typeof setTimeout> | null = null
let eventSubscribed = false

async function fetchOnce(): Promise<void> {
  loading.value = true
  try {
    operations.value = (await AppAPI.AppService.ListOperations()) ?? []
    error.value = null
    loaded.value = true
  } catch (err: unknown) {
    // 失败保留旧投影（stale 语义），只记录错误供视图呈现重试入口
    error.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

/** 立即重拉（手动刷新/事件窗口到期共用）；单飞合流。 */
function refresh(): Promise<void> {
  if (!inflight) {
    inflight = fetchOnce().finally(() => {
      inflight = null
    })
  }
  return inflight
}

/** 事件路径的滑动防抖：突发窗口内多次 operation:changed 只重拉一次。 */
function scheduleEventRefresh() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    debounceTimer = null
    void refresh()
  }, EVENT_REFRESH_DEBOUNCE_MS)
}

// —— 只读筛选（展示派生，不改写投影、不新增状态真相）——
// status/kind 等枚举字段经 bindings 的 JSDoc 类型到达，比较一律 String() 归一
// （与 ModuleCard/useModuleCatalog 消费四维投影的同一纪律）。

const isActive = (op: Operation) => ['queued', 'running'].includes(String(op.status))
const isFinished = (op: Operation) =>
  ['succeeded', 'failed', 'cancelled'].includes(String(op.status))
/** resumable 回灌项：后端以 error.code==='resumable' 标记"上次进程未收口的托管事务"。 */
const isResumable = (op: Operation) => op.error?.code === 'resumable'

/** 记录时间基准：终态取收口时间，未收口取登记时间；不可解析记 0（沉底）。 */
function opTime(op: Operation): number {
  const t = Date.parse(op.finishedAt || op.startedAt || '')
  return Number.isNaN(t) ? 0 : t
}

/** 在途操作（queued/running），保持后端登记顺序。 */
const active = computed(() => operations.value.filter(isActive))

/** 待处理的崩溃残留事务（resumable 回灌投影）。 */
const resumable = computed(() => operations.value.filter(isResumable))

/** 投影刷新失败且已有旧数据：视图据此标注"上一次投影"（stale），不清空也不假装新鲜。 */
const stale = computed(() => loaded.value && error.value !== null)

/** 终态记录按时间倒序取最近 n 条（首页"最近任务"合并面消费）。 */
function recentFinished(n: number): Operation[] {
  if (n <= 0) return []
  return operations.value
    .filter(isFinished)
    .sort((a, b) => opTime(b) - opTime(a))
    .slice(0, n)
}

/** 指定模块的在途操作（卡片徽标与动作钮禁用消费；后端保证同模块同时只一笔未收口写事务）。 */
function activeOf(moduleId: string): Operation | null {
  return active.value.find((op) => op.moduleId === moduleId) ?? null
}

/** resumable 载荷 → DismissResumable 的事务 ID（契约字段 txnId 优先）。 */
export function resumableTxnID(op: Operation): string {
  if (op.txnId) return op.txnId;
  // 降级：旧投影未带 txnId 时剥展示前缀（UUID 裸 ID 不含该前缀）。
  return op.id.startsWith(RESUMABLE_ID_PREFIX) ? op.id.slice(RESUMABLE_ID_PREFIX.length) : op.id
}

/**
 * 统一操作观察面入口（单例）。首调用触发冷加载；operation:changed 只登记一次
 * 全局订阅（处理器仅调度重拉、不触碰组件状态，无泄漏面，故不随视图卸载）。
 */
export function useOperations() {
  if (!eventSubscribed) {
    eventSubscribed = true
    Events.On('operation:changed', scheduleEventRefresh)
  }
  if (!loaded.value && !inflight) void refresh()
  return {
    operations,
    active,
    resumable,
    stale,
    loading,
    error,
    loaded,
    recentFinished,
    activeOf,
    refresh,
  }
}
