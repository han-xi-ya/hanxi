<script setup lang="ts">
// 微信机器人主控台——Phase 6 巨型孤本拆分后的编排层（原 2176 行）。
// 业务状态、操作、事件订阅与扫码轮询的单一来源在 composables/useWechatBot.ts；
// 界面标记与 scoped 样式随 DOM 逐字迁出至 components/wechatbot/ 七个纯展示壳
// （Sidebar 账号栏 / ChatHeader 聊天头部 / RulesStrip 规则条 / ChatFlow 消息流 /
// ChatInput 输入区 / BindModal 扫码模态 / RenameModal 重命名模态）。
// 本体只保留：composable 解构、props/事件接线与纯 UI 折叠、横幅开关。
import { ref } from 'vue'
import { useWechatBot } from '../composables/useWechatBot'
import WechatBotSidebar from '../components/wechatbot/WechatBotSidebar.vue'
import WechatBotChatHeader from '../components/wechatbot/WechatBotChatHeader.vue'
import WechatBotRulesStrip from '../components/wechatbot/WechatBotRulesStrip.vue'
import WechatBotChatFlow from '../components/wechatbot/WechatBotChatFlow.vue'
import WechatBotChatInput from '../components/wechatbot/WechatBotChatInput.vue'
import WechatBotBindModal from '../components/wechatbot/WechatBotBindModal.vue'
import WechatBotRenameModal from '../components/wechatbot/WechatBotRenameModal.vue'

const {
  accounts,
  currentAccount,
  handleSelectAccount,
  chatFlowRef,
  currentMessages,
  attachmentAction,
  handleInboundFileAction,
  inputText,
  isSending,
  handleSendText,
  handleSendImage,
  handleSendFile,
  clearCurrentChat,
  toUserIdInput,
  isEditingTargetUser,
  saveTargetUserId,
  refreshContextToken,
  isRefreshingToken,
  toggleAccountListener,
  openRenameModal,
  handleDeleteAccount,
  showBindModal,
  bindRemarkName,
  qrLoading,
  qrDataUrl,
  qrStatusText,
  qrStatusType,
  openBindModal,
  closeBindModal,
  fetchQRCode,
  showRenameModal,
  renameNewVal,
  saveAccountRename,
} = useWechatBot()

// 纯 UI 折叠/横幅开关（视图本地状态，不涉业务契约，故不上收 composable）
const isSidebarCollapsed = ref(false)
const showRulesBanner = ref(true)
</script>

<template>
  <div class="wechat-page-root">
    <!-- 经典微信桌面端双栏工作台 -->
    <div class="chat-workbench">
      <!-- 1. 左侧：账号与会话管理栏 -->
      <WechatBotSidebar
        :accounts="accounts"
        :current-account-id="currentAccount?.id || ''"
        :collapsed="isSidebarCollapsed"
        @select="handleSelectAccount"
        @toggle-collapse="isSidebarCollapsed = !isSidebarCollapsed"
        @bind="openBindModal"
        @toggle-listener="toggleAccountListener"
        @rename="openRenameModal"
        @delete="handleDeleteAccount"
      />

      <!-- 2. 右侧：主聊天视口 -->
      <main class="chat-main-pane">
        <!-- 未选中任何账号占位 -->
        <div v-if="!currentAccount" class="no-selection-placeholder">
          <div class="ph-icon">💬</div>
          <h3>选择或绑定一个微信机器人</h3>
          <p>在左侧选择账号即可发起消息下发或查看会话流</p>
          <button class="btn-bind-account large" @click="openBindModal">+ 扫码绑定新微信</button>
        </div>

        <template v-else>
          <!-- 2.1 聊天窗口顶部导航栏 (Chat Header) -->
          <WechatBotChatHeader
            :account="currentAccount"
            :collapsed="isSidebarCollapsed"
            :editing-target-user="isEditingTargetUser"
            :refreshing-token="isRefreshingToken"
            v-model:targetUserInput="toUserIdInput"
            @expand="isSidebarCollapsed = false"
            @edit-target="isEditingTargetUser = true"
            @cancel-edit="isEditingTargetUser = false"
            @save-target="saveTargetUserId"
            @refresh-token="refreshContextToken"
            @toggle-listener="toggleAccountListener"
          />

          <!-- 2.2 微信限制规则说明条 (可折叠) -->
          <WechatBotRulesStrip v-if="showRulesBanner" @close="showRulesBanner = false" />

          <!-- 2.3 气泡消息流区域 (Message Stream) -->
          <WechatBotChatFlow
            ref="chatFlowRef"
            :account="currentAccount"
            :messages="currentMessages"
            :attachment-action="attachmentAction"
            @inbound-file-action="handleInboundFileAction"
          />

          <!-- 2.4 底部富交互输入区 (Chat Input Area) -->
          <WechatBotChatInput
            v-model="inputText"
            :is-sending="isSending"
            @send-text="handleSendText"
            @send-image="handleSendImage"
            @send-file="handleSendFile"
            @clear="clearCurrentChat"
          />
        </template>
      </main>
    </div>

    <!-- 扫码绑定模态框 (QR Bind Modal) -->
    <WechatBotBindModal
      v-if="showBindModal"
      v-model:bindRemarkName="bindRemarkName"
      :qr-loading="qrLoading"
      :qr-data-url="qrDataUrl"
      :qr-status-text="qrStatusText"
      :qr-status-type="qrStatusType"
      @close="closeBindModal"
      @retry="fetchQRCode"
    />

    <!-- 账号重命名模态框 (Rename Modal) -->
    <WechatBotRenameModal
      v-if="showRenameModal"
      v-model:renameNewVal="renameNewVal"
      @close="showRenameModal = false"
      @save="saveAccountRename"
    />
  </div>
</template>

<style scoped>
.wechat-page-root {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 48px);
  position: relative;
  user-select: none;
}

/* 主双栏容器 */
.chat-workbench {
  display: flex;
  flex: 1;
  background: var(--surface-panel);
  border-radius: 8px;
  border: 1px solid var(--color-border);
  overflow: hidden;
  box-shadow: var(--shadow-small);
}

/* 右侧主聊天区域 */
.chat-main-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--surface-soft);
  position: relative;
  min-width: 0;
}

.no-selection-placeholder {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
  text-align: center;
  padding: 32px;
}

.ph-icon {
  font-size: 44px;
  margin-bottom: 12px;
  opacity: 0.7;
}

.no-selection-placeholder h3 {
  font-size: 16px;
  font-weight: 600;
  color: var(--color-text);
  margin: 0 0 6px;
}

.no-selection-placeholder p {
  font-size: 13px;
  color: var(--color-text-subtle);
  margin: 0;
}

/* 注：.btn-bind-account 家族与侧栏组件「绑定账号」按钮共用（跨组件边界），
   按「标记与样式同迁、不做语义改动」原则在两侧各留一份逐字副本（§9.6 登记）。 */
.btn-bind-account {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  transition: all 0.15s ease;
}

.btn-bind-account:hover {
  background: var(--state-positive);
}

.btn-bind-account.large {
  padding: 8px 18px;
  font-size: 13px;
  margin-top: 14px;
}
</style>
