import { onActivated, onBeforeUnmount, onDeactivated, onMounted } from 'vue'

/**
 * window 级监听器按 KeepAlive「四斩」生命周期挂摘：
 * mounted/activate 挂，deactivate/unmount 摘。
 *
 * 为什么必须四斩而非 mount/unmount 两斩：工作台视图常驻 App.vue 的
 * KeepAlive(max=10)，切页只是 deactivate 不卸载——只挂 mount/unmount 的
 * window 监听在失活期间会让异页按键「幽灵操作」隐藏视图（BoardFullscreen
 * 审查 #20 钉过、ClipboardView 2026-10-26 审查补票的同类病灶）。
 * 同引用 addEventListener 幂等，首进 mount+activate 双挂无副作用。
 *
 * 条件开合期挂摘的形态（菜单开才挂、关即摘，如 ClipToolbar 溢出菜单）
 * 不归本件管——那种场景手动 attach/detach 更贴切，勿硬套。
 */
export function useWindowKeyListener<K extends keyof WindowEventMap>(
  type: K,
  handler: (this: Window, ev: WindowEventMap[K]) => void,
  options?: boolean | AddEventListenerOptions,
): void {
  const add = () => window.addEventListener(type, handler, options)
  const remove = () => window.removeEventListener(type, handler, options)
  onMounted(add)
  onActivated(add)
  onDeactivated(remove)
  onBeforeUnmount(remove)
}
