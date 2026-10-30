<script setup lang="ts">
// AI 聊天输入区：v-model 草稿 + send/stop/toggle-expand 上抛，内部零业务判断——
// 禁发矩阵（空文本/sending/asking/回合在途/无会话）全部由编排层（useAiChat）
// 算好经 canSend/disabledReason/phase 传入，与 WechatBotChatInput
// 「普通输入、判断上抛」的分工一致。
//
// rows 3 / 展开 8 档照 MsgBoardView draftExpanded（收展态由视图本地持有，纯 UI
// 不上收 composable）；resize: vertical 保留原生拉高兜底（.mb-text 同款）。
//
// IME 钉死：组合期回车是"确认候选词"不是发送——e.isComposing 前置守卫
// （先例 WechatBotChatInput 未防，属已知潜在误发面，本视图补齐）。
// 禁用态只禁发送钮，textarea 不 disable：禁发时用户要能继续改草稿、
// 上翻旧消息复制粘贴。
//
// 剪贴板约定裁量：聊天输入主语义是打字不是"接收粘贴的载荷"，原生 Ctrl+V 可用，
// 有意不加「从剪贴板粘贴」钮（component-patterns 该条自限"短结构输入不加=噪音"），
// 后来者勿"补一致性"。
// 草稿不持久化（无后端）：KeepAlive 停用再回，文本仍在组件实例内存里，可接受。
import { computed } from 'vue'
import UiButton from '../ui/UiButton.vue'

const props = defineProps<{
  modelValue: string
  /** 发送钮三态档位（编排层 sendPhase）：idle/sending/asking/working。 */
  phase: 'idle' | 'asking' | 'sending' | 'working'
  canSend: boolean
  /** 禁发原因（进钮 title，空态按钮只灰不改文案口径）。 */
  disabledReason: string
  /** 输入框占位语（封存会话提示自动复进等口径由编排层拼好传入）。 */
  placeholder: string
  /** 回合在途：显示「停止」钮（= CloseSession，结束本轮并回收进程）。 */
  showStop: boolean
  /** rows 8 档展开态（视图本地持有的纯 UI 位）。 */
  expanded: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  send: []
  stop: []
  'toggle-expand': []
}>()

const textModel = computed({
  get: () => props.modelValue,
  set: (v: string) => emit('update:modelValue', v),
})

const sendLabel = computed(() => {
  switch (props.phase) {
    case 'asking': return '等待确认…'
    case 'sending': return '发送中…'
    case 'working': return 'AI 回复中…'
    default: return '发送 (Enter)'
  }
})

function handleKeydown(e: KeyboardEvent) {
  if (e.isComposing) return // 输入法组合期回车=确认候选词，不是发送
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    if (props.canSend) emit('send')
  }
}
</script>

<template>
  <footer class="ac-input">
    <label class="sr-only" for="ac-draft">消息输入</label>
    <textarea
      id="ac-draft"
      v-model="textModel"
      class="text-input ac-text"
      :rows="expanded ? 8 : 3"
      :placeholder="placeholder"
      @keydown="handleKeydown"
    ></textarea>
    <div class="ac-foot">
      <button type="button" class="btn btn-ghost btn-small ac-expand" :aria-pressed="expanded" @click="emit('toggle-expand')">
        {{ expanded ? '收起' : '展开' }}
      </button>
      <span class="ac-tip">Enter 发送 · Shift+Enter 换行</span>
      <span class="ac-spacer"></span>
      <UiButton
        v-if="showStop"
        variant="danger"
        small
        class="ac-stop"
        title="结束本轮并回收进程（会话封存，下一条消息自动复进）"
        @click="emit('stop')"
      >
        停止
      </UiButton>
      <UiButton
        variant="primary"
        small
        class="ac-send"
        :disabled="!canSend"
        :title="disabledReason || undefined"
        @click="emit('send')"
      >
        {{ sendLabel }}
      </UiButton>
    </div>
  </footer>
</template>

<style scoped>
.ac-input {
  flex: none;
  border-top: 1px solid var(--color-border);
  background: var(--surface-panel);
  padding: 10px 14px;
  display: flex; flex-direction: column; gap: 8px;
}
/* 高度动画是抖动源：rows 切换不做过渡（design-system 动效条） */
.ac-text {
  width: 100%;
  min-width: 0;
  resize: vertical;
  line-height: 1.6;
  font-family: inherit;
}
.ac-foot { display: flex; align-items: center; gap: 8px; min-height: 32px; }
.ac-tip { font-size: var(--text-xs); color: var(--color-text-subtle); min-width: 0; }
.ac-spacer { flex: 1; }
/* busy 控件尺寸不动：发送钮按四字宽钉（checklist 口径），换档文案不顶版 */
.ac-send { min-width: 104px; }
.ac-send:disabled { cursor: not-allowed; }

/* 可视标签由 sr-only 兜底（与 MsgBoardView/ModuleCenterView 同形，全站无全局档） */
.sr-only {
  position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0;
  overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0;
}

@media (pointer: coarse) {
  .ac-send, .ac-stop, .ac-input .btn-ghost { min-height: 44px; }
}
</style>
