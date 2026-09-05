// 全局输入对话框单例：Promise 风格收编 14 处 window.prompt（输路径/备注名等）。
// UiPromptDialog 实例由 App.vue 顶层挂载唯一一份；空串按取消处理由调用方自行判断
// （部分场景允许清空值），本层只区分"提交/取消"。
import { reactive } from 'vue'

export interface PromptOptions {
  title: string
  description?: string
  label?: string
  placeholder?: string
  initialValue?: string
  confirmLabel?: string
  cancelLabel?: string
}

interface PromptState {
  open: boolean
  options: PromptOptions
}

const state = reactive<PromptState>({
  open: false,
  options: { title: '' },
})

let resolver: ((value: string | null) => void) | null = null

function settle(value: string | null) {
  if (!state.open) return
  state.open = false
  resolver?.(value)
  resolver = null
}

export function usePrompt() {
  /** 打开输入框；提交→字符串（可为空串），取消/Esc→null。 */
  function prompt(options: PromptOptions): Promise<string | null> {
    settle(null) // 并发防御：落定旧请求，不让悬挂
    state.options = {
      confirmLabel: '确认',
      cancelLabel: '取消',
      ...options,
    }
    state.open = true
    return new Promise<string | null>((resolve) => {
      resolver = resolve
    })
  }

  return {
    /** 仅供 App.vue 宿主对话框绑定；视图侧只应使用 prompt() */
    promptState: state,
    prompt,
    settlePrompt: settle,
  }
}
