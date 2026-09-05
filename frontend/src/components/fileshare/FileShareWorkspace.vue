<script setup lang="ts">
// 「访问与传输中心」工作区卡：页签导航（接入点/投递箱/传输审计）与面板切换容器。
// 激活页签属工作区自身 UI 状态（原视图 activeTab，不外显）；三个页签面板组件的
// 复制/删除/清空等业务动作经 emit 逐层回传视图编排层处理。
import { ref } from 'vue'
import type {
  NetworkEndpoint,
  DropItem,
  TransferEvent
} from '../../../bindings/hanxi/internal/modules/fileshare/models'
import FileShareEndpointsTab from './FileShareEndpointsTab.vue'
import FileShareInboxTab from './FileShareInboxTab.vue'
import FileShareLogsTab from './FileShareLogsTab.vue'

defineProps<{
  /** 原 status.isRunning——接入点页签停止态空态判定。 */
  isRunning: boolean
  endpoints: NetworkEndpoint[]
  /** 端点二维码 SVG 缓存 (url -> svg)。 */
  qrMap: Record<string, string>
  /** 投递箱条目（原 dropInbox）。 */
  inbox: DropItem[]
  /** 传输审计日志（原 transferLogs）。 */
  logs: TransferEvent[]
}>()

const emit = defineEmits<{
  copyUrl: [url: string]
  copyContent: [content: string]
  deleteDrop: [id: string]
  clearInbox: []
}>()

const activeTab = ref<'endpoints' | 'inbox' | 'logs'>('endpoints')
</script>

<template>
  <section class="workspace-card">
    <div class="workspace-header">
      <div>
        <div class="section-kicker">LIVE WORKSPACE</div>
        <h2 class="section-title">访问与传输中心</h2>
      </div>
      <nav class="tab-nav" aria-label="快传工作区">
        <button type="button" class="tab-item" :class="{ active: activeTab === 'endpoints' }" @click="activeTab = 'endpoints'">
          接入点 <span class="tab-count">{{ endpoints.length }}</span>
        </button>
        <button type="button" class="tab-item" :class="{ active: activeTab === 'inbox' }" @click="activeTab = 'inbox'">
          投递箱 <span class="tab-count">{{ inbox.length }}</span>
        </button>
        <button type="button" class="tab-item" :class="{ active: activeTab === 'logs' }" @click="activeTab = 'logs'">
          传输审计 <span class="tab-count">{{ logs.length }}</span>
        </button>
      </nav>
    </div>

    <div class="workspace-body">
      <!-- 接入点与二维码卡片 -->
      <FileShareEndpointsTab
        v-if="activeTab === 'endpoints'"
        :is-running="isRunning"
        :endpoints="endpoints"
        :qr-map="qrMap"
        @copy="emit('copyUrl', $event)"
      />

      <!-- 跨端投递箱 -->
      <FileShareInboxTab
        v-if="activeTab === 'inbox'"
        :items="inbox"
        @copy="emit('copyContent', $event)"
        @remove="emit('deleteDrop', $event)"
        @clear="emit('clearInbox')"
      />

      <!-- 实时传输审计日志 -->
      <FileShareLogsTab v-if="activeTab === 'logs'" :logs="logs" />
    </div>
  </section>
</template>

<style scoped>
/* 以下样式自 FileShareView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
.tab-nav {
  display: flex;
  gap: 8px;
  border-bottom: 1px solid var(--color-border);
  padding-bottom: 8px;
}

.tab-item {
  background: none;
  border: none;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 500;
  color: var(--color-text-muted);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.tab-item:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.tab-item.active {
  background: var(--state-information);
  color: var(--color-on-primary);
}

.section-kicker {
  margin-bottom: 7px;
  color: var(--color-primary);
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.16em;
}

.workspace-card {
  padding: 0;
  overflow: hidden;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 16px;
  box-shadow: 0 10px 28px var(--shadow-panel);
}

.workspace-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 20px 22px;
  border-bottom: 1px solid var(--color-border);
}

.section-title {
  margin: 0;
  color: var(--color-text);
  font-size: 18px;
  font-weight: 750;
}

.workspace-header {
  align-items: flex-end;
}

.tab-nav {
  gap: 4px;
  max-width: 100%;
  padding: 4px;
  overflow-x: auto;
  background: var(--surface-soft);
  border: 0;
  border-radius: 10px;
}

.tab-item {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 auto;
  padding: 7px 11px;
  white-space: nowrap;
  border-radius: 7px;
}

.tab-item.active {
  color: var(--color-primary-hover);
  background: var(--surface-panel);
  box-shadow: 0 2px 7px var(--shadow-small);
}

.tab-count {
  min-width: 18px;
  padding: 1px 5px;
  color: inherit;
  font-size: 10px;
  text-align: center;
  background: var(--surface-hover);
  border-radius: 999px;
}

.workspace-body {
  min-height: 230px;
  padding: 18px;
  background: var(--surface-soft);
}

.tab-item {
  transition: transform 0.15s ease, border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.tab-item:focus-visible {
  outline: 2px solid var(--color-primary-glow);
  outline-offset: 2px;
}

@media (max-width: 760px) {
  .workspace-header {
    align-items: stretch;
    flex-direction: column;
  }

  .workspace-header {
    gap: 14px;
  }

  .tab-nav {
    width: 100%;
  }
}
</style>
