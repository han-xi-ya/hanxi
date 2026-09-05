// 全局确认对话框单例：把 ConfirmDialog 包成 Promise 风格，收编 window.confirm。
// 对话框实例由 App.vue 顶层挂载唯一一份（复用现有可访问性标杆 ConfirmDialog），
// 视图侧只需 `if (!(await confirm({...}))) return`。
// 注意：ConfirmDialog 的 open 监听是 watch 非 immediate（见 TROUBLESHOOTING §25.4），
// 本单例恒以 open:false 起步、经状态翻转打开，天然规避该坑。
import { reactive } from 'vue'

export interface ConfirmOptions {
  title: string
  description: string
  confirmLabel?: string
  cancelLabel?: string
  tone?: 'default' | 'warning' | 'danger'
  details?: Array<{ label: string; value: string }>
}

interface ConfirmState {
  open: boolean
  options: ConfirmOptions
}

const state = reactive<ConfirmState>({
  open: false,
  options: { title: '', description: '' },
})

let resolver: ((accepted: boolean) => void) | null = null

function settle(accepted: boolean) {
  if (!state.open) return
  state.open = false
  resolver?.(accepted)
  resolver = null
}

export function useConfirm() {
  /** 打开确认框并等待用户裁决；确认→true，取消/Esc/遮罩→false。 */
  function confirm(options: ConfirmOptions): Promise<boolean> {
    // 已有待决确认时按"取消"落定旧请求（防御并发触发，不让旧 Promise 悬挂）
    settle(false)
    state.options = {
      confirmLabel: '确认',
      cancelLabel: '取消',
      tone: 'default',
      details: [],
      ...options,
    }
    state.open = true
    return new Promise<boolean>((resolve) => {
      resolver = resolve
    })
  }

  return {
    /** 仅供 App.vue 宿主对话框绑定；视图侧只应使用 confirm()，不直接改状态 */
    confirmState: state,
    confirm,
    /** 宿主事件接线用：ConfirmDialog 各出口统一回流到 settle */
    settleConfirm: settle,
  }
}
