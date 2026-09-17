<script setup lang="ts">
import { computed } from 'vue'
import type { OutgoingAttachmentDraft } from '../../composables/useWechatBot'

const props = defineProps<{
  modelValue: string
  isSending: boolean
  attachmentDraft: OutgoingAttachmentDraft | null
  attachmentError: string
  isPreparingAttachment: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'send-text': []
  'choose-attachment': []
  'paste-attachment': [file: File]
  'send-attachment': []
  'clear-attachment': []
  'clear': []
}>()

const textModel = computed({
  get: () => props.modelValue,
  set: (v: string) => emit('update:modelValue', v),
})

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    emit('send-text')
  }
}

function handlePaste(e: ClipboardEvent) {
  const file = Array.from(e.clipboardData?.items || [])
    .find(item => item.kind === 'file')
    ?.getAsFile()
  if (!file) return
  e.preventDefault()
  emit('paste-attachment', file)
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}
</script>

<template>
  <footer class="chat-input-area" :class="{ 'has-attachment': attachmentDraft || isPreparingAttachment || attachmentError }">
    <div class="input-toolbar-row">
      <button
        class="toolbar-btn attachment-btn"
        :disabled="isSending || isPreparingAttachment"
        title="选择图片或文件；也可在输入框内按 Ctrl+V 粘贴剪贴板提供的附件"
        @click="emit('choose-attachment')"
      >
        <span class="tb-icon" aria-hidden="true">＋</span>
        {{ isPreparingAttachment ? '正在读取…' : '添加附件' }}
      </button>
      <span class="paste-tip">附件可 Ctrl+V，预览或信息确认后再发送</span>
      <div class="tb-spacer"></div>
      <button class="toolbar-btn text-danger" :disabled="isSending" @click="emit('clear')" title="清空当前消息窗口">
        清屏
      </button>
    </div>

    <div v-if="attachmentDraft || isPreparingAttachment || attachmentError" class="attachment-stage" aria-live="polite">
      <div v-if="isPreparingAttachment" class="attachment-loading">正在校验附件内容与大小…</div>
      <div v-else-if="attachmentDraft" class="attachment-preview-card">
        <img v-if="attachmentDraft.isImage && attachmentDraft.previewUrl" :src="attachmentDraft.previewUrl" class="attachment-thumb" alt="待发送图片预览" />
        <div v-else class="attachment-file-icon" aria-hidden="true">FILE</div>
        <div class="attachment-meta">
          <strong :title="attachmentDraft.fileName">{{ attachmentDraft.fileName }}</strong>
          <span>{{ attachmentDraft.isImage ? '图片' : '文件' }} · {{ formatFileSize(attachmentDraft.fileSize) }}</span>
        </div>
        <button class="attachment-remove" :disabled="isSending" aria-label="移除待发送附件" @click="emit('clear-attachment')">移除</button>
        <button class="attachment-send" :disabled="isSending" @click="emit('send-attachment')">
          {{ isSending ? '发送中…' : '确认发送' }}
        </button>
      </div>
      <div v-else class="attachment-error">
        <span>{{ attachmentError }}</span>
        <button @click="emit('choose-attachment')">重新选择</button>
      </div>
    </div>

    <div class="input-textarea-wrapper">
      <textarea
        v-model="textModel"
        class="wechat-textarea"
        placeholder="输入消息；Enter 发送，Shift + Enter 换行，Ctrl+V 可粘贴附件…"
        rows="3"
        @keydown="handleKeydown"
        @paste="handlePaste"
      ></textarea>
    </div>

    <div class="input-footer-row">
      <span class="shortcut-tip">普通文本粘贴不受影响 · 附件不会自动发送</span>
      <button class="btn-send-message" :disabled="!modelValue.trim() || isSending" @click="emit('send-text')">
        {{ isSending ? '发送中…' : '发送 (S)' }}
      </button>
    </div>
  </footer>
</template>

<style scoped>
.chat-input-area { min-height: 155px; background: var(--surface-panel); border-top: 1px solid var(--color-border); display: flex; flex-direction: column; flex-shrink: 0; }
.chat-input-area.has-attachment { min-height: 235px; }
.input-toolbar-row { padding: 6px 14px 2px; display: flex; align-items: center; gap: 8px; }
.toolbar-btn { background: transparent; border: none; font-size: var(--text-sm); color: var(--color-text); padding: 5px 8px; border-radius: 6px; cursor: pointer; display: flex; align-items: center; gap: 4px; transition: background .15s ease; }
.toolbar-btn:hover:not(:disabled) { background: var(--surface-hover); }
.toolbar-btn:disabled { color: var(--color-text-subtle); cursor: not-allowed; }
.attachment-btn { border: 1px solid var(--color-border); background: var(--surface-soft); }
.toolbar-btn.text-danger:hover:not(:disabled) { color: var(--state-danger); background: var(--state-danger-soft); }
.tb-icon { font-size: var(--text-md); line-height: 1; }
.paste-tip { font-size: var(--text-xs); color: var(--color-text-subtle); min-width: 0; }
.tb-spacer { flex: 1; }
.attachment-stage { margin: 5px 14px 7px; }
.attachment-loading, .attachment-error, .attachment-preview-card { min-height: 58px; border: 1px solid var(--color-border); border-radius: 8px; background: var(--surface-soft); }
.attachment-loading { display: flex; align-items: center; padding: 10px 12px; color: var(--color-text-muted); font-size: var(--text-sm); }
.attachment-preview-card { display: grid; grid-template-columns: 46px minmax(0, 1fr) auto auto; gap: 10px; align-items: center; padding: 7px 9px; }
.attachment-thumb { width: 46px; height: 46px; object-fit: cover; border-radius: 6px; border: 1px solid var(--color-border); }
.attachment-file-icon { width: 46px; height: 46px; display: grid; place-items: center; border-radius: 6px; background: var(--surface-hover); color: var(--color-text-muted); font: 600 10px ui-monospace, Consolas, monospace; }
.attachment-meta { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.attachment-meta strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: var(--text-sm); color: var(--color-text); }
.attachment-meta span { font-size: var(--text-xs); color: var(--color-text-subtle); }
.attachment-remove, .attachment-send, .attachment-error button { border-radius: 6px; padding: 6px 10px; font-size: var(--text-sm); cursor: pointer; }
.attachment-remove { border: 1px solid var(--color-border); color: var(--color-text-muted); background: transparent; }
.attachment-send { border: 0; color: var(--color-text-inverse); background: var(--state-positive); min-width: 74px; }
.attachment-remove:disabled, .attachment-send:disabled { opacity: .55; cursor: not-allowed; }
.attachment-error { padding: 9px 11px; display: flex; align-items: center; justify-content: space-between; gap: 12px; color: var(--state-danger); font-size: var(--text-sm); }
.attachment-error button { border: 1px solid var(--color-border); background: var(--surface-panel); color: var(--color-text); }
.input-textarea-wrapper { flex: 1; padding: 0 14px; min-height: 48px; }
.wechat-textarea { width: 100%; height: 100%; border: none; outline: none; resize: none; font-size: var(--text-base); color: var(--color-text); font-family: inherit; line-height: 1.5; background: transparent; }
.input-footer-row { padding: 4px 14px 8px; display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.shortcut-tip { font-size: var(--text-xs); color: var(--color-text-subtle); }
.btn-send-message { background: var(--state-positive); color: var(--color-text-inverse); border: none; padding: 5px 16px; border-radius: 4px; font-size: var(--text-sm); font-weight: 500; cursor: pointer; transition: all .15s ease; }
.btn-send-message:disabled { background: var(--surface-hover); color: var(--color-text-subtle); cursor: not-allowed; }
button:focus-visible, .wechat-textarea:focus-visible { outline: 2px solid var(--color-primary, #0f8b8d); outline-offset: 2px; }
@media (max-width: 640px) { .paste-tip { display: none; } .attachment-preview-card { grid-template-columns: 46px minmax(0, 1fr); } .attachment-remove, .attachment-send { grid-row: 2; } .attachment-send { grid-column: 2; } .shortcut-tip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
@media (pointer: coarse) { .toolbar-btn, .attachment-remove, .attachment-send, .attachment-error button, .btn-send-message { min-height: 44px; } }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { transition-duration: .01ms !important; } }
</style>
