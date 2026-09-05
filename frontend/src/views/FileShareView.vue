<script setup lang="ts">
// 局域网文件快传视图 —— Phase 6 巨型孤本结构拆分后的编排层。
// 业务状态/操作/事件订阅/轮询收敛于 composables/useFileShareServer（bindings 调用序列、
// 事件名与 toast 文案逐字未动）；hero/概览/设置/工作区（接入点·投递箱·审计）按职责
// 拆分至 components/fileshare/*，scoped 样式随标记迁移，本文件只留页面骨架与根级标记
// （alert-banner 错误横幅、section-gap），特征测试 FileShareView.spec.ts 锁定行为基线。
import FileShareHero from '../components/fileshare/FileShareHero.vue'
import FileShareOverview from '../components/fileshare/FileShareOverview.vue'
import FileShareSettings from '../components/fileshare/FileShareSettings.vue'
import FileShareWorkspace from '../components/fileshare/FileShareWorkspace.vue'
import { useFileShareServer } from '../composables/useFileShareServer'

const {
  status,
  configForm,
  endpoints,
  dropInbox,
  transferLogs,
  loading,
  errorMsg,
  qrMap,
  canOpenShareDirectory,
  unsavedPathHint,
  handleToggleServer,
  handleSaveConfig,
  handleChooseDirectory,
  handleOpenShareDirectory,
  copyToClipboard,
  handleDeleteDrop,
  handleClearInbox,
} = useFileShareServer()
</script>

<template>
  <div class="page fileshare-page">
    <FileShareHero
      :status="status"
      :fallback-port="configForm.port"
      :loading="loading"
      @toggle="handleToggleServer"
    />

    <div v-if="errorMsg" class="alert-banner error section-gap">
      <span>⚠️ {{ errorMsg }}</span>
      <button type="button" class="btn-text" aria-label="关闭错误提示" @click="errorMsg = ''">✕</button>
    </div>

    <FileShareOverview :status="status" :inbox-count="dropInbox.length" />

    <FileShareSettings
      :form="configForm"
      :is-running="status.isRunning"
      :can-open="canOpenShareDirectory"
      :unsaved="unsavedPathHint"
      @save="handleSaveConfig"
      @choose="handleChooseDirectory"
      @open="handleOpenShareDirectory"
    />

    <FileShareWorkspace
      :is-running="status.isRunning"
      :endpoints="endpoints"
      :qr-map="qrMap"
      :inbox="dropInbox"
      :logs="transferLogs"
      @copy-url="copyToClipboard($event)"
      @copy-content="copyToClipboard($event, '已复制投递内容')"
      @delete-drop="handleDeleteDrop"
      @clear-inbox="handleClearInbox"
    />
  </div>
</template>

<style scoped>
.fileshare-page {
  padding: 24px 32px;
  max-width: 1360px;
  margin: 0 auto;
}

/* Refined dashboard layout */
.fileshare-page {
  width: 100%;
  max-width: 1440px;
  padding: 28px 36px 40px;
  overflow-x: hidden;
}

/* 段间距：错误横幅（本层标记）与概览/设置子组件根元素共用——
   子组件单根元素同时携带父作用域 data-v 属性，此规则对其照常命中。 */
.section-gap {
  margin-bottom: 18px;
}

@media (max-width: 760px) {
  .fileshare-page {
    padding: 18px 16px 30px;
  }
}
</style>
