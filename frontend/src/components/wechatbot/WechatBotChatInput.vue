<script setup lang="ts">
// 微信机器人底部富交互输入区：工具按钮条（图片/文件/清屏）+ 多行输入框 + 发送栏。
// 自 WechatBotView.vue 随 DOM 逐字迁出的纯展示壳——发送/选图/选文件/清屏四个动作
// 全部上抛视图编排层（useWechatBot）执行；handleKeydown（Enter 发送、Shift+Enter 换行）
// 为本区专属交互逻辑，随消费组件就近安放。文本经受控 v-model 与父级 inputText 同一
// ref 双向代理，语义与拆分前模板直接 v-model 逐字等价。
import { computed } from 'vue'

const props = defineProps<{
  /** 输入框文本（与父级 inputText 同一 ref，受控回写）。 */
  modelValue: string
  /** 发送进行态（工具按钮禁用与发送按钮文案/禁用开关）。 */
  isSending: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'send-text': []
  'send-image': []
  'send-file': []
  'clear': []
}>()

// 受控输入：读写均代理父级 ref（等价于拆分前模板直接 v-model="inputText"）。
const textModel = computed({
  get: () => props.modelValue,
  set: (v: string) => emit('update:modelValue', v),
})

// 键盘快捷键处理 (Enter 发送，Shift+Enter 换行)
function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    emit('send-text')
  }
}
</script>

<template>
  <!-- 2.4 底部富交互输入区 (Chat Input Area) -->
  <footer class="chat-input-area">
    <!-- 顶部工具按钮条 -->
    <div class="input-toolbar-row">
      <button class="toolbar-btn" :disabled="isSending" @click="emit('send-image')" title="选择本地图片并通过微信加密通道发送">
        <span class="tb-icon">🖼️</span> 发送图片
      </button>
      <button class="toolbar-btn" :disabled="isSending" @click="emit('send-file')" title="选择任意本地文件并通过微信加密通道发送">
        <span class="tb-icon">📁</span> 发送文件
      </button>
      <div class="tb-spacer"></div>
      <button class="toolbar-btn text-danger" @click="emit('clear')" title="清空当前消息窗口">
        🧹 清屏
      </button>
    </div>

    <!-- 多行输入框 -->
    <div class="input-textarea-wrapper">
      <textarea
        v-model="textModel"
        class="wechat-textarea"
        placeholder="输入要下发给微信的消息，按 Enter 发送，Shift + Enter 换行…"
        rows="3"
        @keydown="handleKeydown"
      ></textarea>
    </div>

    <!-- 底部发送按钮栏 -->
    <div class="input-footer-row">
      <span class="shortcut-tip">按 Enter 发送 · Shift+Enter 换行</span>
      <button
        class="btn-send-message"
        :disabled="!modelValue.trim() || isSending"
        @click="emit('send-text')"
      >
        {{ isSending ? '发送中…' : '发送 (S)' }}
      </button>
    </div>
  </footer>
</template>

<style scoped>
/* 以下样式自 WechatBotView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
/* 3. 底部输入区 */
.chat-input-area {
  height: 155px;
  background: var(--surface-panel);
  border-top: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.input-toolbar-row {
  padding: 6px 14px 2px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.toolbar-btn {
  background: transparent;
  border: none;
  font-size: 12px;
  color: var(--color-text);
  padding: 4px 8px;
  border-radius: 4px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  transition: background 0.15s ease;
}

.toolbar-btn:hover {
  background: var(--surface-hover);
}

.toolbar-btn.text-danger:hover {
  color: var(--state-danger);
  background: var(--state-danger-soft);
}

.tb-icon {
  font-size: 13px;
}

.tb-spacer {
  flex: 1;
}

.input-textarea-wrapper {
  flex: 1;
  padding: 0 14px;
}

.wechat-textarea {
  width: 100%;
  height: 100%;
  border: none;
  outline: none;
  resize: none;
  font-size: 13px;
  color: var(--color-text);
  font-family: inherit;
  line-height: 1.5;
  background: transparent;
}

.input-footer-row {
  padding: 4px 14px 8px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.shortcut-tip {
  font-size: 11px;
  color: var(--color-text-subtle);
}

.btn-send-message {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 5px 16px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-send-message:hover {
  background: var(--state-positive);
}

.btn-send-message:disabled {
  background: var(--surface-hover);
  color: var(--color-text-subtle);
  cursor: not-allowed;
}
</style>
