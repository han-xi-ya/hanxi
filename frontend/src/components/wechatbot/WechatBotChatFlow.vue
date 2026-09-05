<script setup lang="ts">
// 微信机器人气泡消息流：系统胶囊 / 入站（含附件卡）/ 出站三类消息的纯展示壳。
// 自 WechatBotView.vue 随 DOM 逐字迁出——formatFileSize 为本区专属呈现函数（语义与
// utils/format.fmtSize 不同：空值文案"微信接收附件"、独立 B 档、KB/MB 一位小数、
// 阈值 >=1MB，强并会改界面文案，拆分时有意保留本地实现随消费组件就近安放）；
// getAvatarColor 为跨组件共用呈现函数（自 useWechatBot 引纯函数）。
// 附件打开/保存动作上抛视图编排层（useWechatBot）执行；滚动容器经 defineExpose
// 的 scrollToBottom 句柄供编排层触达（拆分前 ref 直挂同一 div，nextTick 时序逐字等价）。
import { ref } from 'vue'
import type { WechatAccountState } from '../../../bindings/hanxi/internal/modules/wechat/models'
import { getAvatarColor, type ChatMessage } from '../../composables/useWechatBot'

defineProps<{
  /** 当前选中账号（视图 v-else 分支保证非空，用于入站头像与发送者展示名）。 */
  account: WechatAccountState
  /** 当前会话消息列表（视图侧 currentMessages 过滤结果直传）。 */
  messages: ChatMessage[]
  /** 附件操作进行中状态表（按 attachmentId 索引，仅用于按钮禁用与文案）。 */
  attachmentAction: Record<string, 'opening' | 'saving' | undefined>
}>()

const emit = defineEmits<{
  'inbound-file-action': [msg: ChatMessage, action: 'open' | 'save']
}>()

const container = ref<HTMLDivElement | null>(null)

function scrollToBottom(smooth = false) {
  if (container.value) {
    container.value.scrollTo({
      top: container.value.scrollHeight,
      behavior: smooth ? 'smooth' : 'auto'
    })
  }
}

// 有意保留本地实现：语义与 utils/format.fmtSize 不同（空值文案"微信接收附件"、
// 独立 B 档、KB/MB 一位小数、阈值 >=1MB），强并会改界面文案。
function formatFileSize(size?: number): string {
  if (!size || size <= 0) return '微信接收附件'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

defineExpose({ scrollToBottom })
</script>

<template>
  <!-- 2.3 气泡消息流区域 (Message Stream) -->
  <div ref="container" class="chat-flow-scroll">
    <div v-if="messages.length === 0" class="flow-empty-box">
      <div class="empty-bubble-icon">💬</div>
      <div class="empty-title">当前会话暂无消息</div>
      <div class="empty-desc">可在下方输入文字、发送图片或文件，开始与微信端交互</div>
    </div>

    <template v-for="msg in messages" :key="msg.id">
      <!-- 系统消息胶囊 -->
      <div v-if="msg.direction === 'sys'" class="bubble-row system-row">
        <div class="system-pill">
          <span class="pill-text">{{ msg.content }}</span>
          <span class="pill-time">{{ msg.time }}</span>
        </div>
      </div>

      <!-- 手机微信端发来的消息 (左侧灰色气泡：展示备注名与对应头像) -->
      <div v-else-if="msg.direction === 'in'" class="bubble-row inbound-row">
        <div
          class="msg-avatar-icon peer-avatar"
          :style="{ backgroundColor: getAvatarColor(account.remarkName) }"
        >
          {{ (account.remarkName || '微').slice(0, 1) }}
        </div>
        <div class="bubble-column">
          <div class="bubble-info-header">
            <span class="sender-title">{{ account.remarkName || msg.senderName || '微信联系人' }}</span>
            <span class="msg-timestamp">{{ msg.time }}</span>
          </div>
          <div class="bubble-box inbound-bubble">
            <template v-if="msg.msgType === 'text'">
              <p class="bubble-text">{{ msg.content }}</p>
            </template>
            <template v-else-if="msg.msgType === 'file'">
              <div class="file-card-preview inbound-file-card">
                <span class="file-icon-box">📁</span>
                <div class="file-info-group">
                  <span class="file-main-name">{{ msg.fileName || '微信文件' }}</span>
                  <span class="file-sub-type" :title="msg.attachmentError">
                    {{ msg.downloadable ? formatFileSize(msg.fileSize) : (msg.attachmentError || '附件不可用') }}
                  </span>
                </div>
                <div class="file-actions">
                  <button
                    class="file-action-btn"
                    :disabled="!msg.downloadable || !msg.attachmentId || !!attachmentAction[msg.attachmentId || '']"
                    @click="emit('inbound-file-action', msg, 'open')"
                  >
                    {{ attachmentAction[msg.attachmentId || ''] === 'opening' ? '打开中…' : '打开' }}
                  </button>
                  <button
                    class="file-action-btn"
                    :disabled="!msg.downloadable || !msg.attachmentId || !!attachmentAction[msg.attachmentId || '']"
                    @click="emit('inbound-file-action', msg, 'save')"
                  >
                    {{ attachmentAction[msg.attachmentId || ''] === 'saving' ? '保存中…' : '保存' }}
                  </button>
                </div>
              </div>
            </template>
            <template v-else>
              <p class="media-tip">📷 {{ msg.content }}</p>
            </template>
          </div>
        </div>
      </div>

      <!-- 电脑端 Hanxi 机器人下发出去的消息 (右侧微信经典绿气泡：电脑是机器人一方) -->
      <div v-else-if="msg.direction === 'out'" class="bubble-row outbound-row">
        <div class="bubble-column align-right">
          <div class="bubble-info-header justify-end">
            <span class="msg-timestamp">{{ msg.time }}</span>
            <span class="sender-title robot-sender-title">🤖 Hanxi 机器人</span>
          </div>
          <div class="bubble-box outbound-bubble">
            <template v-if="msg.msgType === 'text'">
              <p class="bubble-text">{{ msg.content }}</p>
            </template>
            <template v-else-if="msg.msgType === 'image'">
              <div class="media-card-preview">
                <span class="media-icon-box">🖼️</span>
                <div class="media-info-group">
                  <span class="media-main-name">图片消息 (AES加密下发)</span>
                  <span class="media-sub-path" :title="msg.filePath">{{ msg.filePath }}</span>
                </div>
              </div>
            </template>
            <template v-else-if="msg.msgType === 'file'">
              <div class="file-card-preview out">
                <span class="file-icon-box">📁</span>
                <div class="file-info-group">
                  <span class="file-main-name">{{ msg.fileName || '文件附件' }}</span>
                  <span class="file-sub-path" :title="msg.filePath">{{ msg.filePath }}</span>
                </div>
              </div>
            </template>
          </div>
          <div v-if="msg.status === 'sending'" class="msg-send-status sending">发送中…</div>
          <div v-else-if="msg.status === 'failed'" class="msg-send-status failed" :title="msg.error">⚠️ 发送失败</div>
        </div>
        <div class="msg-avatar-icon my-avatar robot-avatar-box">
          🤖
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
/* 以下样式自 WechatBotView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
/* 消息流主体 */
.chat-flow-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.flow-empty-box {
  margin: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
  text-align: center;
}

.empty-bubble-icon {
  font-size: 36px;
  margin-bottom: 6px;
  opacity: 0.5;
}

.empty-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--color-text);
}

.empty-desc {
  font-size: 12px;
  color: var(--color-text-subtle);
  margin-top: 4px;
}

/* 消息行 */
.bubble-row {
  display: flex;
  gap: 10px;
  max-width: 80%;
}

.bubble-row.system-row {
  align-self: center;
  max-width: 90%;
  margin: 2px 0;
}

.system-pill {
  background: var(--surface-hover);
  padding: 4px 12px;
  border-radius: 12px;
  font-size: 12px;
  color: var(--color-text-muted);
  display: flex;
  align-items: center;
  gap: 6px;
}

.pill-time {
  font-size: 10px;
  color: var(--color-text-subtle);
}

.bubble-row.inbound-row {
  align-self: flex-start;
}

.bubble-row.outbound-row {
  align-self: flex-end;
  flex-direction: row;
}

.robot-sender-title {
  color: var(--color-primary);
  font-weight: 500;
}

.robot-avatar-box {
  background: var(--color-primary) !important;
  font-size: 16px;
}

.msg-avatar-icon {
  width: 34px;
  height: 34px;
  border-radius: 6px;
  color: #ffffff; /* 功能例外：数据值头像底上恒白不随主题 */
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 13px;
  flex-shrink: 0;
}

.peer-avatar {
  background: var(--color-text-muted);
}

.bubble-column {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.bubble-column.align-right {
  align-items: flex-end;
}

.bubble-info-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--color-text-subtle);
}

.bubble-info-header.justify-end {
  justify-content: flex-end;
}

.bubble-box {
  padding: 9px 13px;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.5;
  word-break: break-word;
  user-select: text;
  box-shadow: var(--shadow-small);
}

.inbound-bubble {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  border-top-left-radius: 2px;
}

.outbound-bubble {
  background: #95ec69; /* 功能例外：微信品牌绿气泡 */
  color: #1a1a1a; /* 功能例外：品牌绿上深文 */
  border-top-right-radius: 2px;
}

.bubble-text {
  margin: 0;
  white-space: pre-wrap;
}

.file-card-preview {
  display: flex;
  align-items: center;
  gap: 8px;
  background: rgba(0, 0, 0, 0.04); /* 功能例外：气泡上中性深度覆层 */
  padding: 6px 10px;
  border-radius: 6px;
}

.file-card-preview.out {
  background: rgba(0, 0, 0, 0.06); /* 功能例外：同上绿气泡档 */
}

.inbound-file-card {
  min-width: 310px;
}

.file-actions {
  display: flex;
  gap: 6px;
  margin-left: auto;
}

.file-action-btn {
  border: 1px solid var(--color-border);
  background: var(--surface-panel);
  color: var(--color-text);
  border-radius: 5px;
  padding: 4px 9px;
  font-size: 11px;
  cursor: pointer;
}

.file-action-btn:hover:not(:disabled) {
  border-color: #07c160; /* 功能例外：微信品牌绿 hover */
  color: #07883f; /* 功能例外：微信品牌绿深文 */
}

.file-action-btn:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.file-icon-box {
  font-size: 22px;
}

.file-info-group {
  display: flex;
  flex-direction: column;
}

.file-main-name {
  font-weight: 600;
  font-size: 12px;
}

.file-sub-type {
  font-size: 11px;
  color: var(--color-text-muted);
}

.file-sub-path {
  font-size: 10px;
  color: var(--color-text-muted);
  font-family: var(--font-mono);
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.media-card-preview {
  display: flex;
  align-items: center;
  gap: 8px;
}

.media-icon-box {
  font-size: 20px;
}

.media-info-group {
  display: flex;
  flex-direction: column;
}

.media-main-name {
  font-weight: 600;
  font-size: 12px;
}

.media-sub-path {
  font-size: 10px;
  font-family: var(--font-mono);
  opacity: 0.8;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.media-tip {
  margin: 0;
}

.msg-send-status {
  font-size: 11px;
}

.msg-send-status.sending {
  color: var(--color-text-subtle);
}

.msg-send-status.failed {
  color: var(--state-danger);
}
</style>
