import { ref } from 'vue'
import type { Ref } from 'vue'

export type RunResult<T> = { ok: true; data: T } | { ok: false; error: unknown }

export interface UseAsyncActionReturn {
  busy: Ref<boolean>
  run: <T>(fn: () => Promise<T>) => Promise<RunResult<T>>
}

/**
 * "按钮转圈"异步动作包装：单飞 busy 状态 + 结果/错误双通道返回。
 *
 * 不吞错也不产出文案——ok:false 分支原样携带 error，由调用方经
 * getErrorMessage 转提示；重入（busy 期间再次 run）直接返回
 * `操作进行中` 错误且不执行 fn，替代各视图手写的 `if (busy) return`。
 */
export function useAsyncAction(): UseAsyncActionReturn {
  const busy = ref(false)

  async function run<T>(fn: () => Promise<T>): Promise<RunResult<T>> {
    if (busy.value) {
      return { ok: false, error: new Error('操作进行中') }
    }
    busy.value = true
    try {
      return { ok: true, data: await fn() }
    } catch (err) {
      return { ok: false, error: err }
    } finally {
      busy.value = false
    }
  }

  return { busy, run }
}
