import { onActivated, onDeactivated, onMounted, onUnmounted, ref } from 'vue'
import type { Ref } from 'vue'

export interface UsePollingOptions {
  /** start 时先立即执行一次再进入周期（默认 true，对齐托管视图"激活即刷一帧"现状）。 */
  immediateFirstRun?: boolean
}

export interface UsePollingReturn {
  start: () => void
  stop: () => void
  isPolling: Ref<boolean>
}

/**
 * KeepAlive 感知的周期轮询（须在组件 setup 同步期内调用）。
 *
 * 契约（docs/FRONTEND.md §6 铁律 3）：onMounted/onActivated 启动、
 * onDeactivated/onUnmounted 停止——页面切后台绝不空转，杜绝定时器泄漏。
 * start 幂等：KeepAlive 下 mounted 与首次 activated 先后触发，仅生效一次
 * （复刻 MarkerOnView `if (pollTimer) return` 的防重语义）。
 *
 * 选型：自管 setInterval 而非 VueUse useIntervalFn——包装层反正必须自己守
 * onActivated/onDeactivated 契约（useIntervalFn 无 KeepAlive 感知），自管实现
 * 的幂等 start、首跑时机与 31 个存量视图现状完全一致，测试与回归心智最省；
 * 如日后需要"页面不可见自动暂停"可无痛替换（本应用为常驻 WebView2，暂无此收益）。
 */
export function usePolling(
  fn: () => void | Promise<void>,
  intervalMs: number,
  opts: UsePollingOptions = {},
): UsePollingReturn {
  const { immediateFirstRun = true } = opts
  const isPolling = ref(false)
  let timer: ReturnType<typeof setInterval> | null = null

  function tick() {
    // 轮询回调异常绝不冒泡杀定时器（fn 若自带 catch 则此处仅兜底透传）。
    try {
      const r = fn()
      if (r && typeof r.catch === 'function') {
        r.catch((err: unknown) => console.warn('[usePolling] 轮询回调异常:', err))
      }
    } catch (err) {
      console.warn('[usePolling] 轮询回调异常:', err)
    }
  }

  function start() {
    if (timer) return
    if (immediateFirstRun) tick()
    timer = setInterval(tick, intervalMs)
    isPolling.value = true
  }

  function stop() {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
    isPolling.value = false
  }

  onMounted(start)
  onActivated(start)
  onDeactivated(stop)
  onUnmounted(stop)

  return { start, stop, isPolling }
}
