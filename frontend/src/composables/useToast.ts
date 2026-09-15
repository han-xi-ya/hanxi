import { ref } from 'vue'

/** showToast 的可选项。duration：toast 自动消失前的毫秒数，缺省 2500ms。
 *  长错误串需要留时间阅读/复制时，调用方显式传较长 duration（如 { duration: 8000 }），不要改全局默认值。 */
export interface ToastOptions {
  duration?: number
}

const toastMsg = ref('')
let timer: ReturnType<typeof setTimeout> | null = null

/** 全局轻量 toast（模块级单例，全应用共享一条 toastMsg）。 */
export function useToast() {
  /** 展示一条 toast；传 options.duration 可覆盖默认 2500ms 的保活时长。 */
  function showToast(msg: string, options?: ToastOptions) {
    const duration = options?.duration ?? 2500
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    toastMsg.value = msg
    timer = setTimeout(() => {
      toastMsg.value = ''
      timer = null
    }, duration)
  }

  function clearToast() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    toastMsg.value = ''
  }

  return {
    toastMsg,
    showToast,
    clearToast,
  }
}
