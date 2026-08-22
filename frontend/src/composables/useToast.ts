import { ref } from 'vue'

export interface ToastOptions {
  duration?: number
}

const toastMsg = ref('')
let timer: ReturnType<typeof setTimeout> | null = null

export function useToast() {
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
