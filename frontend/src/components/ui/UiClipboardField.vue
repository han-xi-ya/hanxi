<script setup lang="ts">
// UiClipboardField：带"从剪贴板粘贴 / 复制全文"按钮的标准文本区（PLAN_CLIPBOARD §3.2B）。
// 设计意图是把剪贴板交互规范"默认就会用上"而非靠自觉——含多行输入/结果输出的
// 新视图优先挂本件；存量视图按克制原则逐个评估替换。
// 输入态：textarea + 粘贴钮（空即填入；已有内容按 pasteMode 追加/替换，不无声覆盖）；
// 只读态（输出）：默认只挂"复制全文"。回执话术全部走 useClipboard/copyWithToast。
import { computed } from 'vue'
import { useClipboard } from '../../composables/useClipboard'
import { useToast } from '../../composables/useToast'
import UiButton from './UiButton.vue'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  rows?: number
  /** 等宽字族（配置/命令/代码类内容） */
  mono?: boolean
  /** 输出态：只读 + 复制全文 */
  readonly?: boolean
  disabled?: boolean
  label?: string
  // 显隐走"否定/追加"布尔（Boolean 缺省即 false，语义自明；不用三态 undefined）
  /** 输入态隐藏粘贴钮（如内容仅由程序写入） */
  hidePaste?: boolean
  /** 只读态隐藏复制全文钮 */
  hideCopy?: boolean
  /** 输入态额外挂"复制全文"（默认只在只读态出现） */
  allowCopy?: boolean
  /** 已有内容时的粘贴策略：追加到末尾（默认，保守）或整体替换 */
  pasteMode?: 'append' | 'replace'
  pasteOkTip?: string
  copyOkTip?: string
}>(), {
  rows: 6,
  pasteMode: 'append',
})

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const { paste, copyWithToast } = useClipboard()
const { showToast, showErrorToast } = useToast()

// 无 SSR 场景下的模块级自增 id（label/aria 关联用）
let seq = 0
const fieldId = `ui-clip-field-${++seq}-${Math.floor(Math.random() * 1e6)}`

const wantPaste = computed(() => !props.readonly && !props.hidePaste)
const wantCopy = computed(() => (props.readonly ? !props.hideCopy : props.allowCopy))

async function pasteIn() {
  const text = await paste()
  if (text === null) {
    showErrorToast('无法读取剪贴板，请在输入框内按 Ctrl+V')
    return
  }
  if (text.trim() === '') {
    showErrorToast('剪贴板中没有文本')
    return
  }
  const cur = props.modelValue
  let next = text
  if (props.pasteMode === 'append' && cur !== '') {
    next = cur.endsWith('\n') ? cur + text : `${cur}\n${text}`
  }
  emit('update:modelValue', next)
  showToast(props.pasteOkTip ?? '已粘贴剪贴板内容')
}

async function copyOut() {
  await copyWithToast(props.modelValue, props.copyOkTip ?? '已复制全文')
}

function onInput(e: Event) {
  emit('update:modelValue', (e.target as HTMLTextAreaElement).value)
}
</script>

<template>
  <div class="ui-clip-field">
    <div v-if="label || wantPaste || wantCopy" class="ui-clip-head">
      <label v-if="label" class="ui-clip-label" :for="fieldId">{{ label }}</label>
      <span v-else aria-hidden="true" />
      <span class="ui-clip-actions">
        <UiButton v-if="wantPaste" small :disabled="disabled" title="读取系统剪贴板文本填入" @click="pasteIn">
          📋 从剪贴板粘贴
        </UiButton>
        <UiButton v-if="wantCopy" small :disabled="disabled || !modelValue" @click="copyOut">
          复制全文
        </UiButton>
      </span>
    </div>
    <textarea
      :id="fieldId"
      class="ui-clip-textarea"
      :class="{ 'ui-clip-mono': mono }"
      :value="modelValue"
      :rows="rows"
      :placeholder="placeholder"
      :readonly="readonly"
      :disabled="disabled"
      spellcheck="false"
      @input="onInput"
    />
  </div>
</template>

<style scoped>
.ui-clip-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.ui-clip-head { display: flex; align-items: center; gap: 8px; min-width: 0; }
.ui-clip-label { font-size: var(--text-sm); color: var(--color-text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ui-clip-actions { display: flex; align-items: center; gap: 8px; margin-left: auto; flex-wrap: wrap; }
.ui-clip-textarea {
  width: 100%; min-width: 0; padding: 8px 10px; font-size: var(--text-base); line-height: 1.5;
  color: var(--color-text); background: var(--surface-panel);
  border: 1px solid var(--color-border); border-radius: var(--radius-control);
  resize: vertical; transition: border-color var(--motion-base) ease;
}
.ui-clip-textarea:hover { border-color: var(--color-border-strong); }
.ui-clip-textarea:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 1px; }
.ui-clip-textarea::placeholder { color: var(--color-text-subtle); }
.ui-clip-textarea[readonly] { background: var(--surface-soft); color: var(--color-text-muted); }
.ui-clip-mono { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
</style>
