// 轮盘侧 devicePixelRatio 响应式读数：图标像素预算（wheelIconBudget）的 DPR 输入源。
// 轮盘弹窗是常驻复用窗口，跨缩放比拖动显示器 / 改系统缩放时 DPR 会变而窗口不重载，
// 故用 matchMedia(`(resolution: Xdppx)`) 精确监听（设备像素比变化才触发，resize
// 兜底）；非浏览器/测试环境全量防御，读数退回 1。
import { onBeforeUnmount, onMounted, readonly, ref } from 'vue'

function currentDpr(): number {
  if (typeof window === 'undefined') return 1
  return Number.isFinite(window.devicePixelRatio) && window.devicePixelRatio > 0 ? window.devicePixelRatio : 1
}

/** 只读响应式 DPR；组件用它给 app: 位图算物理边长（见 wheelIconBudget.snapIconCssPx）。 */
export function useWheelDpr() {
  const raw = ref(currentDpr())
  let mq: MediaQueryList | null = null

  function onDprChange() {
    raw.value = currentDpr()
    bind()
  }

  function bind() {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
    mq?.removeEventListener?.('change', onDprChange)
    try {
      mq = window.matchMedia(`(resolution: ${raw.value}dppx)`)
      mq.addEventListener?.('change', onDprChange)
    } catch {
      mq = null // matchMedia 表达式不被支持：仅剩手动重挂兜底，DPR 读数保持首值
    }
  }

  onMounted(bind)
  onBeforeUnmount(() => mq?.removeEventListener?.('change', onDprChange))
  return readonly(raw)
}
