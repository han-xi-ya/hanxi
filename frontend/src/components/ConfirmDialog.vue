<script setup lang="ts">
import { ref } from 'vue'
import { DIALOG_Z, useDialogA11y } from '../composables/useDialogA11y'

const props = withDefaults(defineProps<{
  open: boolean
  title: string
  description: string
  confirmLabel?: string
  cancelLabel?: string
  tone?: 'default' | 'warning' | 'danger'
  busy?: boolean
  details?: Array<{ label: string; value: string }>
}>(), {
  confirmLabel: '确认', cancelLabel: '取消', tone: 'default', busy: false, details: () => [],
})

const emit = defineEmits<{ confirm: []; cancel: []; 'update:open': [value: boolean] }>()
const dialog = ref<HTMLElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)

function cancel() {
  if (props.busy) return
  emit('cancel')
  emit('update:open', false)
}

// 焦点/Tab 环/Esc 契约走 useDialogA11y 单源，本件为全仓标杆语义（波 2C 接线，逐字等价）：
// z 顶档（App 单例，无需让位）、watch 非 immediate（spec 钉死：翻真后才挂监听）、
// 开窗焦点落取消按钮、Esc 消费且 preventDefault。
useDialogA11y({
  open: () => props.open,
  dialog,
  z: DIALOG_Z.top,
  close: cancel,
  focusOnOpen: () => cancelButton.value?.focus(),
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="workbench-confirm-backdrop" @mousedown.self="cancel">
      <section ref="dialog" class="workbench-confirm" :class="`is-${tone}`" role="alertdialog" aria-modal="true" aria-labelledby="hx-confirm-title">
        <header>
          <span class="workbench-confirm-mark" aria-hidden="true">!</span>
          <div><h2 id="hx-confirm-title">{{ title }}</h2><p>{{ description }}</p></div>
        </header>
        <dl v-if="details.length" class="workbench-confirm-details">
          <div v-for="item in details" :key="item.label"><dt>{{ item.label }}</dt><dd>{{ item.value }}</dd></div>
        </dl>
        <footer>
          <button ref="cancelButton" class="workbench-confirm-btn secondary" :disabled="busy" @click="cancel">{{ cancelLabel }}</button>
          <button class="workbench-confirm-btn primary" :disabled="busy" @click="emit('confirm')">{{ busy ? '处理中…' : confirmLabel }}</button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.workbench-confirm-backdrop { position:fixed; inset:0; z-index:1000; display:grid; place-items:center; padding:24px; background:var(--overlay-mask); }
.workbench-confirm { width:min(460px,100%); max-height:min(72vh,640px); overflow-y:auto; padding:20px; border:1px solid var(--color-border); border-radius:var(--radius-panel); background:var(--surface-panel); box-shadow:var(--shadow-panel); color:var(--color-text); }
.workbench-confirm header { display:flex; gap:12px; align-items:flex-start; }
.workbench-confirm h2 { margin:0 0 6px; font-size:var(--text-lg); }
.workbench-confirm p { margin:0; color:var(--color-text-muted); font-size:var(--text-base); line-height:1.65; white-space:pre-line; }
.workbench-confirm-mark { display:grid; place-items:center; width:30px; height:30px; flex:none; border-radius:var(--radius-element); background:var(--state-warning-soft); color:var(--state-warning); font-weight:800; }
.is-danger .workbench-confirm-mark { background:var(--state-danger-soft); color:var(--state-danger); }
.workbench-confirm-details { margin:16px 0 0; padding:12px; border:1px solid var(--color-border); border-radius:var(--radius-element); background:var(--surface-soft); }
.workbench-confirm-details div { display:grid; grid-template-columns:100px minmax(0,1fr); gap:10px; padding:4px 0; }
dt { color:var(--color-text-muted); font-size:var(--text-sm); } dd { margin:0; overflow-wrap:anywhere; font:var(--text-sm)/1.5 var(--font-mono); }
footer { display:flex; justify-content:flex-end; gap:8px; margin-top:18px; }
.workbench-confirm-btn { min-height:var(--control-h-lg); padding:0 16px; border:1px solid var(--color-border); border-radius:var(--radius-control); font-weight:650; cursor:pointer; }
.workbench-confirm-btn.secondary { background:var(--surface-soft); color:var(--color-text); }
.workbench-confirm-btn.primary { border-color:transparent; background:var(--color-primary); color:var(--color-on-primary); }
.is-danger .workbench-confirm-btn.primary { background:var(--state-danger); color:var(--color-on-accent); }
.workbench-confirm-btn:disabled { opacity:.55; cursor:not-allowed; }
.workbench-confirm-btn:focus-visible { outline:3px solid var(--focus-ring); outline-offset:2px; }
@media (max-width:460px) { .workbench-confirm-backdrop{align-items:end;padding:12px}.workbench-confirm{padding:16px}.workbench-confirm footer{flex-direction:column-reverse}.workbench-confirm-btn{min-height:44px;width:100%}.workbench-confirm-details div{grid-template-columns:1fr}.workbench-confirm-details dt{margin-bottom:2px} }
@media (prefers-reduced-motion: reduce) { *,*::before,*::after{transition-duration:.01ms!important;animation-duration:.01ms!important;animation-iteration-count:1!important} }
</style>
