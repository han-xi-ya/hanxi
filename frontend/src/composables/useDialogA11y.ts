// 对话框焦点入窗 / Tab 环困窗 / Esc 与遮罩关闭 / 回焦契约单源（冗余治理波 2C）：
// ConfirmDialog / UiPromptDialog / UiHistoryDialog 三件的键盘与焦点机制曾近五成互抄、
// 逐字三份并存，本钩子收敛机制为一份；三件间的有意差异全部以参数显式化——
// z 档 / 焦点落点 / Esc 让位与 preventDefault 口径 / watch 即时性 / KeepAlive 重挂。
// 红线：三件既有 spec（ConfirmDialog / UiPromptDialog / UiHistoryDialog）逐条钉死下列
// 语义——挂监听须待开窗翻转的 nextTick 之后、Confirm 的 watch 非 immediate、
// UiHistory 的 Esc 在确认框在场时让位且不 preventDefault。改本文件须三 spec 全跑。
import { nextTick, onActivated, onBeforeUnmount, onDeactivated, watch, type Ref } from 'vue'

/**
 * 对话框 z 档单源（CSS 实际值仍各件 scoped 逐档原样保留，本处是互锁语义的登记处）：
 *  - top=1000：App 单例对话框层（ConfirmDialog / UiPromptDialog，恒位于 App.vue 顶层）；
 *  - underTop=950：视图级对话框（UiHistoryDialog）。宿主视图经 KeepAlive 懒挂载，
 *    Teleport 锚点 DOM 序天然晚于 App 单例，同层拼位置必输——950 是按层阶判出的
 *    档位（UiHistoryDialog 头注 2 的历史结论），不是随手写的数。
 * 互锁由类型保证：underTop 档必须携带 escapeYield（一次 Esc 只关最上层——
 * 一键穿两层是 document 级监听同场竞走的历史事故），漏传在 vue-tsc 即报错。
 */
export const DIALOG_Z = { underTop: 950, top: 1000 } as const
export type DialogZTier = (typeof DIALOG_Z)[keyof typeof DIALOG_Z]

interface DialogA11yBase {
  /** 开合状态取值器（对应各件的 props.open） */
  open: () => boolean
  /** 对话框根元素：Tab 环的查找域，亦是 focusOnOpen 缺省时的焦点落点（宿主需 tabindex="-1"） */
  dialog: Ref<HTMLElement | null>
  /** Esc 且未让位时的出口（cancel/close 语义含 busy 门禁等归宿主） */
  close: () => void
  /** 返回 true 时本次 Esc 让位给上层：既不关闭也不 preventDefault，事件原样继续传播 */
  escapeYield?: () => boolean
  /** Esc 消费时是否 preventDefault（Confirm/UiPrompt 为 true，UiHistory 为 false——逐字保住） */
  escapePreventsDefault?: boolean
  /** Tab 环可聚焦选择器；缺省见 DEFAULT_FOCUSABLE 注释 */
  focusableSelector?: string
  /** 开窗焦点落点；缺省落 dialog 根 */
  focusOnOpen?: () => void
  /** previousFocus 快照之后、nextTick 之前的同步动作（UiPrompt 回填 draft，保证随后 select 拿到新值） */
  beforeOpen?: () => void
  /** watch(open) 是否 immediate（Confirm false[spec 钉死]，UiPrompt/UiHistory true） */
  immediate?: boolean
  /** deactivate（KeepAlive 切页）摘监听、activate 且仍开着补挂——仅视图级对话框需要 */
  keepAliveRebind?: boolean
}

/** z 档判别联合：underTop（950）分支强制 escapeYield，互锁契约进类型面 */
export type DialogA11yOptions =
  | (DialogA11yBase & { z: typeof DIALOG_Z.top })
  | (DialogA11yBase & { z: typeof DIALOG_Z.underTop; escapeYield: () => boolean })

/**
 * Tab 环可聚焦集默认档：镜像三件中原最宽者（UiHistory 在 Confirm 基准形上补
 * input/select/textarea——其面板含搜索框）。Confirm/UiPrompt 模板不含
 * input/select/textarea（Confirm 更无插槽），取最宽档后困窗序列逐字不变。
 */
const DEFAULT_FOCUSABLE = 'button:not([disabled]), input, select, textarea, [href], [tabindex]:not([tabindex="-1"])'

export function useDialogA11y(options: DialogA11yOptions): void {
  const { open, dialog, close } = options
  const escapePreventsDefault = options.escapePreventsDefault ?? true
  const focusableSelector = options.focusableSelector ?? DEFAULT_FOCUSABLE
  let previousFocus: HTMLElement | null = null

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      if (options.escapeYield?.()) return
      if (escapePreventsDefault) event.preventDefault()
      close()
      return
    }
    if (event.key !== 'Tab' || !dialog.value) return
    const focusable = Array.from(dialog.value.querySelectorAll<HTMLElement>(focusableSelector))
    if (!focusable.length) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
  }

  const hook = () => document.addEventListener('keydown', onKeydown)
  const unhook = () => document.removeEventListener('keydown', onKeydown)

  watch(open, async (v) => {
    if (v) {
      previousFocus = document.activeElement as HTMLElement | null
      options.beforeOpen?.()
      await nextTick()
      if (options.focusOnOpen) options.focusOnOpen()
      else dialog.value?.focus()
      hook()
    } else {
      unhook()
      previousFocus?.focus()
      previousFocus = null
    }
  }, { immediate: options.immediate ?? false })

  // 卸载兜底：宿主页 KeepAlive 逐出/切页时弹窗可能仍开着，摘不掉就是每次逐出叠一份监听
  onBeforeUnmount(unhook)
  if (options.keepAliveRebind) {
    onDeactivated(unhook)
    onActivated(() => { if (open()) hook() })
  }
}
