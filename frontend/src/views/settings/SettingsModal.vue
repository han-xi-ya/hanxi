<script setup lang="ts">
// 设置分区弹窗骨架：AiSection（MCP 接入向导）与 SnapshotSection（历史版本预览）
// 两家的弹窗皮逐字同构，此处收口遮罩、卡片壳与 head/body/actions 三段及 ✕ 钮；
// 标题、正文与动作按钮全部由宿主经插槽自撰——插槽内容归宿主作用域，宿主 scoped
// 样式（含 .modal-head h3 各自的字族）照常命中。
// 除宽度两档（640/680）外其余口径两处一致；modal-actions 的 surface-panel 底色
// 与卡片底同值，统一起见对两宿主皆无观感差。
// 不含 Esc 关闭：两个来源分区原本皆无该行为，此处不添行为（纪律）——spec 以行为断言
// 钉死；useDialogA11y 的 Esc 无关闭开关（escapeYield 让位不 preventDefault、close 仍会
// 触发），故不复用该钩子，手动实现同口径的 Tab 环 + 焦点快照/归还，Esc 全程不消费。
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 卡片定宽 px（max-width 92vw 兜底）；缺省 640（向导档）。 */
    width?: number
    /** 对话框可访问名（宿主传弹窗标题，与 KeyOpsModal 的 title prop 同范式）；缺省回落通用名。 */
    title?: string
  }>(),
  { width: 640, title: '设置弹窗' },
)

const emit = defineEmits<{ close: [] }>()

const dialog = ref<HTMLElement | null>(null)
let previousFocus: HTMLElement | null = null

// Tab 环可聚焦集：逐字镜像 useDialogA11y 的 DEFAULT_FOCUSABLE（三件对话框同口径）
const FOCUSABLE = 'button:not([disabled]), input, select, textarea, [href], [tabindex]:not([tabindex="-1"])'

function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !dialog.value) return
  const focusable = Array.from(dialog.value.querySelectorAll<HTMLElement>(FOCUSABLE))
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}

// 两宿主皆 v-if 挂载本壳：mount=开窗、unmount=关窗。焦点快照/落根/归还的时序
// 对齐 useDialogA11y（挂监听与聚焦在 mount 当轮，宿主 slot 已就位）。
onMounted(() => {
  previousFocus = document.activeElement as HTMLElement | null
  dialog.value?.focus()
  document.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKeydown)
  previousFocus?.focus()
  previousFocus = null
})
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <div ref="dialog" class="modal-card" role="dialog" aria-modal="true" :aria-label="props.title" tabindex="-1" :style="{ width: `${props.width}px` }">
      <div class="modal-head">
        <slot name="head" />
        <button class="btn-close" aria-label="关闭" @click="emit('close')">✕</button>
      </div>
      <div class="modal-body">
        <slot />
      </div>
      <div class="modal-actions">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal-backdrop {
  /* 视图级对话框档（DIALOG_Z.underTop 互锁，见 tokens.css 层序登记册）：
     叠在 App 单例对话框（ConfirmDialog 等 1000）之下 */
  position: fixed; inset: 0; z-index: var(--z-dialog-under);
  background: var(--overlay-mask);
  display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: var(--surface-panel); border-radius: var(--radius-element); max-width: 92vw;
  box-shadow: var(--shadow-panel); overflow: hidden; display: flex; flex-direction: column;
}
.modal-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 20px; border-bottom: 1px solid var(--color-border);
}
/* ✕ 钮命中区补到控件高度档（30px 见方），hover 上底色——原先命中区只有字形大小 */
.btn-close {
  display: inline-flex; align-items: center; justify-content: center;
  min-width: var(--control-h-md); min-height: var(--control-h-md);
  background: transparent; border: none; border-radius: var(--radius-chip);
  font-size: var(--text-lg); cursor: pointer; color: var(--color-text-muted);
  transition: background var(--motion-base) ease, color var(--motion-base) ease;
}
.btn-close:hover { background: var(--surface-hover); color: var(--color-text); }
.modal-body { padding: 14px 20px; display: flex; flex-direction: column; gap: 10px; max-height: 62vh; overflow: auto; }
.modal-actions {
  display: flex; justify-content: flex-end; gap: 10px;
  padding: 12px 20px; border-top: 1px solid var(--color-border); background: var(--surface-panel);
}
</style>
