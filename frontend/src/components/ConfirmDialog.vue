<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

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
let previousFocus: HTMLElement | null = null

function cancel() {
  if (props.busy) return
  emit('cancel')
  emit('update:open', false)
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); cancel(); return }
  if (event.key !== 'Tab' || !dialog.value) return
  const focusable = Array.from(dialog.value.querySelectorAll<HTMLElement>('button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'))
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}

watch(() => props.open, async (open) => {
  if (open) {
    previousFocus = document.activeElement as HTMLElement | null
    await nextTick()
    cancelButton.value?.focus()
    document.addEventListener('keydown', onKeydown)
  } else {
    document.removeEventListener('keydown', onKeydown)
    previousFocus?.focus()
    previousFocus = null
  }
})

onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="workbench-confirm-backdrop" @mousedown.self="cancel">
      <section ref="dialog" class="workbench-confirm" :class="`is-${tone}`" role="alertdialog" aria-modal="true" :aria-labelledby="`${title}-dialog-title`">
        <header>
          <span class="workbench-confirm-mark" aria-hidden="true">!</span>
          <div><h2 :id="`${title}-dialog-title`">{{ title }}</h2><p>{{ description }}</p></div>
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
.workbench-confirm { width:min(460px,100%); padding:20px; border:1px solid var(--color-border); border-radius:var(--radius-panel); background:var(--surface-panel); box-shadow:var(--shadow-panel); color:var(--color-text); }
.workbench-confirm header { display:flex; gap:12px; align-items:flex-start; }
.workbench-confirm h2 { margin:0 0 6px; font-size:17px; }
.workbench-confirm p { margin:0; color:var(--color-text-muted); font-size:13px; line-height:1.65; }
.workbench-confirm-mark { display:grid; place-items:center; width:30px; height:30px; flex:none; border-radius:var(--radius-element); background:var(--state-warning-soft); color:var(--state-warning); font-weight:800; }
.is-danger .workbench-confirm-mark { background:var(--state-danger-soft); color:var(--state-danger); }
.workbench-confirm-details { margin:16px 0 0; padding:12px; border:1px solid var(--color-border); border-radius:var(--radius-element); background:var(--surface-soft); }
.workbench-confirm-details div { display:grid; grid-template-columns:100px minmax(0,1fr); gap:10px; padding:4px 0; }
dt { color:var(--color-text-muted); font-size:12px; } dd { margin:0; overflow-wrap:anywhere; font:12px/1.5 var(--font-mono); }
footer { display:flex; justify-content:flex-end; gap:8px; margin-top:18px; }
.workbench-confirm-btn { min-height:38px; padding:0 16px; border:1px solid var(--color-border); border-radius:var(--radius-control); font-weight:650; cursor:pointer; }
.workbench-confirm-btn.secondary { background:var(--surface-soft); color:var(--color-text); }
.workbench-confirm-btn.primary { border-color:transparent; background:var(--color-primary); color:var(--color-on-primary); }
.is-danger .workbench-confirm-btn.primary { background:var(--state-danger); }
.workbench-confirm-btn:disabled { opacity:.55; cursor:not-allowed; }
.workbench-confirm-btn:focus-visible { outline:3px solid var(--focus-ring); outline-offset:2px; }
@media (max-width:460px) { .workbench-confirm-backdrop{align-items:end;padding:12px}.workbench-confirm{padding:16px}.workbench-confirm footer{flex-direction:column-reverse}.workbench-confirm-btn{min-height:44px;width:100%}.workbench-confirm-details div{grid-template-columns:1fr}.workbench-confirm-details dt{margin-bottom:2px} }
@media (prefers-reduced-motion: reduce) { *,*::before,*::after{transition-duration:.01ms!important;animation-duration:.01ms!important;animation-iteration-count:1!important} }
</style>
