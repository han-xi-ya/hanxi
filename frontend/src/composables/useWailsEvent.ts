import { Events } from '@wailsio/runtime'
import { getCurrentScope, onScopeDispose } from 'vue'

// 全局只警告一次：作用域外调用属用法错误，重复刷屏无诊断价值。
let warnedOutOfScope = false

/**
 * 订阅 Wails 事件并在组件卸载时自动注销（须在组件 setup 同步期内调用）。
 *
 * 注册发生在 setup 期而非 onMounted，保证挂载完成前推送的早期事件不丢失；
 * Wails runtime 回调载荷恒为 `{ data }` 包装，此处统一解包后交给 handler。
 * 返回值仍可用于提前手动注销；组件内使用通常可忽略。
 */
export function useWailsEvent<T>(name: string, handler: (data: T) => void): () => void {
  const unlisten = Events.On(name, (event: { data?: unknown }) => {
    handler(event?.data as T)
  })
  if (getCurrentScope()) {
    onScopeDispose(unlisten)
  } else if (!warnedOutOfScope) {
    warnedOutOfScope = true
    console.warn(`[useWailsEvent] 在组件作用域外订阅「${name}」：不会自动清理，请在不再需要时手动调用返回的 unlisten`)
  }
  return unlisten
}
