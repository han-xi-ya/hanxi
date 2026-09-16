import { ref } from 'vue'

/**
 * 失败类 toast 的默认保活时长（ms）。
 *
 * 契约（TROUBLESHOOTING #49 的收口升级）：长错误串需要留时间阅读/复制，
 * 失败提示统一经 showErrorToast 走本默认值；各视图不再逐个传 { duration: 8000 }。
 * 调参只改这一处。
 */
export const ERROR_TOAST_DURATION = 8000

/** showToast / showErrorToast 的可选项。duration：toast 自动消失前的毫秒数，
 *  缺省时普通 toast 为 2500ms、失败 toast 为 ERROR_TOAST_DURATION（8000ms）；显式传入可覆盖。 */
export interface ToastOptions {
  duration?: number
}

const toastMsg = ref('')
let timer: ReturnType<typeof setTimeout> | null = null

/** 全局轻量 toast（模块级单例，全应用共享一条 toastMsg）。 */
export function useToast() {
  /** 展示一条 toast；传 options.duration 可覆盖默认 2500ms 的保活时长。 */
  function showToast(msg: string, options?: ToastOptions) {
    present(msg, options?.duration ?? 2500)
  }

  /** 展示一条失败类 toast（错误详情、操作失败回执）；默认保活 ERROR_TOAST_DURATION，
   *  显式传 options.duration 可覆盖。失败播报请走本入口，不要再手写 { duration: 8000 }。 */
  function showErrorToast(msg: string, options?: ToastOptions) {
    present(msg, options?.duration ?? ERROR_TOAST_DURATION)
  }

  function clearToast() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    toastMsg.value = ''
  }

  function present(msg: string, duration: number) {
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

  return {
    toastMsg,
    showToast,
    showErrorToast,
    clearToast,
  }
}
