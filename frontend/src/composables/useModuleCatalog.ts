// 模块目录投影单一来源 composable（Wave 2，ADR-0001 §1.2）：
// 并发拉取 ListCatalog（静态身份 + description 描述文案）+ ListModuleStates
// （四维事实）两源，按 moduleId 合并为 ModuleEntry[] 后进程内共享。
// 描述文案自 ModuleCatalogItem 契约收编后不再借道 ListModules（第三源已移除，
// 其 enabled/installed 状态字段本就与投影真相重叠，属双写面）。
//
// 纪律：只缓存投影副本，不写任何持久状态、不打补丁推导第二份真相；
// ext:changed 到达只重拉、不本地改 entries（安装/启停的落点在后端事务）。
// 模块级单例（与 useTheme/useToast 同一模式）：事件订阅随模块存活于
// 应用全生命周期，视图卸载后缓存仍被事件保鲜，二次进入零冷启动。
import { ref } from 'vue'
import { Events } from '@wailsio/runtime'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type {
  ModuleCatalogItem,
  ModuleState,
} from '../../bindings/hanxi/internal/extapi/models'
import { getErrorMessage } from '../utils/errors'

export interface ModuleEntry {
  /** 静态身份投影（含 description 描述文案，卡片展示与搜索消费）。 */
  catalog: ModuleCatalogItem
  /** 四维状态投影；后端未下发该模块状态时为 null（UI 显示"状态未知"，禁止本地推断）。 */
  state: ModuleState | null
}

/** ext:changed 突发合并窗口（ms）：一次安装/启停可能牵动多条广播，只重拉一次。 */
const EVENT_REFRESH_DEBOUNCE_MS = 300

// —— 模块级单例状态 ——
const entries = ref<ModuleEntry[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
/** 是否已成功完成过首拉：区分"首屏骨架"与"有数据时的后台刷新失败（stale）"。 */
const loaded = ref(false)

/** in-flight 单飞：并发 refresh() 合流到同一次拉取，不产生交错覆盖。 */
let inflight: Promise<void> | null = null
let debounceTimer: ReturnType<typeof setTimeout> | null = null
let eventSubscribed = false

function mergeEntries(
  catalog: ModuleCatalogItem[] | null,
  states: ModuleState[] | null,
): ModuleEntry[] {
  // 目录驱动：模块中心永远显示全部目录项（未安装也在"可安装"里，ADR-0001 §1.8）
  const stateById = new Map((states ?? []).map((s) => [s.moduleId, s]))
  return (catalog ?? []).map((item) => ({
    catalog: item,
    state: stateById.get(item.id) ?? null,
  }))
}

async function fetchOnce(): Promise<void> {
  loading.value = true
  try {
    const [catalog, states] = await Promise.all([
      AppAPI.AppService.ListCatalog(),
      AppAPI.AppService.ListModuleStates(),
    ])
    entries.value = mergeEntries(catalog, states)
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

/** 事件路径的滑动防抖：突发窗口内多次 ext:changed 只重拉一次。 */
function scheduleEventRefresh() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    debounceTimer = null
    void refresh()
  }, EVENT_REFRESH_DEBOUNCE_MS)
}

/**
 * 模块中心投影入口（单例）。首调用触发冷加载；ext:changed 只登记一次全局
 * 订阅（处理器仅调度重拉、不触碰组件状态，无泄漏面，故不随视图卸载）。
 */
export function useModuleCatalog() {
  if (!eventSubscribed) {
    eventSubscribed = true
    Events.On('ext:changed', scheduleEventRefresh)
  }
  if (!loaded.value && !inflight) void refresh()
  return { entries, loading, error, loaded, refresh }
}
