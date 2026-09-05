<script setup lang="ts">
// 输入型对话框：ConfirmDialog 的 prompt 姊妹件（结构/焦点/键盘契约一致）。
// 打开后焦点落输入框；Enter 提交、Esc 取消；提交值经 submit 事件上抛。
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  open: boolean
  title: string
  description?: string
  label?: string
  placeholder?: string
  initialValue?: string
  confirmLabel?: string
  cancelLabel?: string
}>(), {
  description: '', label: '', placeholder: '', initialValue: '',
  confirmLabel: '确认', cancelLabel: '取消',
})

const emit = defineEmits<{ submit: [value: string]; cancel: []; 'update:open': [value: boolean] }>()

const dialog = ref<HTMLElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const draft = ref('')
let previousFocus: HTMLElement | null = null

function cancel() {
  emit('cancel')
  emit('update:open', false)
}

function submit() {
  emit('submit', draft.value)
  emit('update:open', false)
}

function onInputKeydown(event: KeyboardEvent) {
  // Enter 就地绑定于输入元素（不经 document 冒泡，链路最短）
  if (event.key === 'Enter') { event.preventDefault(); submit() }
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); cancel(); return }
  if (event.key !== 'Tab' || !dialog.value) return
  const focusable = Array.from(dialog.value.querySelectorAll<HTMLElement>('button:not([disabled]), input, [href], [tabindex]:not([tabindex="-1"])'))
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}

// 有意做成 immediate（与 ConfirmDialog 的 watch 语义差异见 TROUBLESHOOTING §25.4）：
// 允许宿主/单例以 open:true 直接挂载即完成焦点与初值初始化；false 分支在挂载期是安全空操作。
watch(() => props.open, async (open) => {
  if (open) {
    previousFocus = document.activeElement as HTMLElement | null
    draft.value = props.initialValue ?? ''
    await nextTick()
    input.value?.focus()
    input.value?.select()
    document.addEventListener('keydown', onKeydown)
  } else {
    document.removeEventListener('keydown', onKeydown)
    previousFocus?.focus()
    previousFocus = null
  }
}, { immediate: true })

onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="hx-prompt-backdrop" @mousedown.self="cancel">
      <section ref="dialog" class="hx-prompt" role="dialog" aria-modal="true" :aria-labelledby="title + '-prompt-title'">
        <header>
          <h2 :id="title + '-prompt-title'">{{ title }}</h2>
          <p v-if="description">{{ description }}</p>
        </header>
        <label class="hx-prompt-field">
          <span v-if="label" class="hx-prompt-label">{{ label }}</span>
          <input ref="input" v-model="draft" type="text" :placeholder="placeholder" @keydown="onInputKeydown" />
        </label>
        <footer>
          <button type="button" class="hx-prompt-btn secondary" @click="cancel">{{ cancelLabel }}</button>
          <button type="button" class="hx-prompt-btn primary" @click="submit">{{ confirmLabel }}</button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.hx-prompt-backdrop { position: fixed; inset: 0; z-index: 1000; display: grid; place-items: center; padding: 24px; background: var(--overlay-mask); }
.hx-prompt { width: min(440px, 100%); padding: 20px; border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--surface-panel); box-shadow: var(--shadow-panel); color: var(--color-text); }
.hx-prompt h2 { margin: 0 0 6px; font-size: 17px; }
.hx-prompt header p { margin: 0 0 4px; color: var(--color-text-muted); font-size: 13px; line-height: 1.65; }
.hx-prompt-field { display: flex; flex-direction: column; gap: 6px; margin-top: 14px; }
.hx-prompt-label { font-size: 12px; font-weight: 600; color: var(--color-text-muted); }
.hx-prompt-field input { min-height: 38px; padding: 0 10px; border: 1px solid var(--color-border-strong); border-radius: var(--radius-control); background: var(--surface-soft); color: var(--color-text); font-size: 13px; }
.hx-prompt-field input:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 1px; }
footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
.hx-prompt-btn { min-height: 38px; padding: 0 16px; border: 1px solid var(--color-border); border-radius: var(--radius-control); font-weight: 650; cursor: pointer; transition: background var(--motion-base) ease; }
.hx-prompt-btn.secondary { background: var(--surface-soft); color: var(--color-text); }
.hx-prompt-btn.secondary:hover { background: var(--surface-hover); }
.hx-prompt-btn.primary { border-color: transparent; background: var(--color-primary); color: var(--color-on-primary); }
.hx-prompt-btn.primary:hover { background: var(--color-primary-hover); }
@media (max-width: 460px) { .hx-prompt-backdrop { align-items: end; padding: 12px } .hx-prompt { padding: 16px } .hx-prompt footer { flex-direction: column-reverse } .hx-prompt-btn { min-height: 44px; width: 100% } }
</style>
