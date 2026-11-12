<script setup lang="ts">
// 「访问与传输中心」工作区卡：页签导航（接入点/投递箱/传输审计）与面板切换容器。
// 激活页签是本组件局部 UI 状态，不外显到视图；三个页签面板的复制/删除/清空等业务动作
// 经 emit 逐层回传视图编排层处理。
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
  /** 服务是否运行——接入点页签停止态空态判定。 */
  isRunning: boolean
  endpoints: NetworkEndpoint[]
  /** 端点二维码 SVG 缓存 (url -> svg)。 */
  qrMap: Record<string, string>
  /** 二维码生成失败 Map (url -> 错误文本)；命中即卡片出失败·重试。 */
  qrErrors: Record<string, string>
  /** 投递箱条目（视图侧 dropInbox）。 */
  inbox: DropItem[]
  /** 传输审计事件（视图侧 transferLogs，最新 50 条）。 */
  logs: TransferEvent[]
}>()

const emit = defineEmits<{
  copyUrl: [url: string]
  copyContent: [content: string]
  deleteDrop: [id: string]
  clearInbox: []
  retryQr: []
}>()

const activeTab = ref<'endpoints' | 'inbox' | 'logs'>('endpoints')

// APG tab 模式：方向键循环切换并聚焦（roving tabindex 已在钮上），
// Home/End 落首尾；点击切换语义不变
const TAB_ORDER = ['endpoints', 'inbox', 'logs'] as const
function onTabKeydown(e: KeyboardEvent) {
  const i = TAB_ORDER.indexOf(activeTab.value)
  let next = -1
  if (e.key === 'ArrowRight') next = (i + 1) % TAB_ORDER.length
  else if (e.key === 'ArrowLeft') next = (i - 1 + TAB_ORDER.length) % TAB_ORDER.length
  else if (e.key === 'Home') next = 0
  else if (e.key === 'End') next = TAB_ORDER.length - 1
  if (next < 0) return
  e.preventDefault()
  activeTab.value = TAB_ORDER[next]
  document.getElementById(`fs-tab-${TAB_ORDER[next]}`)?.focus()
}
</script>

<template>
  <section class="workspace-card">
    <div class="workspace-header">
      <div>
        <div class="section-kicker">LIVE WORKSPACE</div>
        <h2 class="section-title">访问与传输中心</h2>
      </div>
      <div class="tab-nav" role="tablist" aria-label="快传工作区">
        <button type="button" role="tab" id="fs-tab-endpoints" aria-controls="fs-panel-endpoints" class="tab-item" :class="{ active: activeTab === 'endpoints' }" :aria-selected="activeTab === 'endpoints'" :tabindex="activeTab === 'endpoints' ? 0 : -1" @click="activeTab = 'endpoints'" @keydown="onTabKeydown($event)">
          接入点 <span class="tab-count">{{ endpoints.length }}</span>
        </button>
        <button type="button" role="tab" id="fs-tab-inbox" aria-controls="fs-panel-inbox" class="tab-item" :class="{ active: activeTab === 'inbox' }" :aria-selected="activeTab === 'inbox'" :tabindex="activeTab === 'inbox' ? 0 : -1" @click="activeTab = 'inbox'" @keydown="onTabKeydown($event)">
          投递箱 <span class="tab-count">{{ inbox.length }}</span>
        </button>
        <button type="button" role="tab" id="fs-tab-logs" aria-controls="fs-panel-logs" class="tab-item" :class="{ active: activeTab === 'logs' }" :aria-selected="activeTab === 'logs'" :tabindex="activeTab === 'logs' ? 0 : -1" @click="activeTab = 'logs'" @keydown="onTabKeydown($event)">
          传输审计 <span class="tab-count">{{ logs.length }}</span>
        </button>
      </div>
    </div>

    <div class="workspace-body">
      <!-- 接入点与二维码卡片（tabpanel 语义经单根 fallthrough 落在 .tab-content 上，
           保持 .workspace-body > .tab-content > .card 既有级联不变） -->
      <FileShareEndpointsTab
        v-if="activeTab === 'endpoints'"
        id="fs-panel-endpoints"
        role="tabpanel"
        aria-labelledby="fs-tab-endpoints"
        :is-running="isRunning"
        :endpoints="endpoints"
        :qr-map="qrMap"
        :qr-errors="qrErrors"
        @copy="emit('copyUrl', $event)"
        @retry-qr="emit('retryQr')"
      />

      <!-- 跨端投递箱 -->
      <FileShareInboxTab
        v-else-if="activeTab === 'inbox'"
        id="fs-panel-inbox"
        role="tabpanel"
        aria-labelledby="fs-tab-inbox"
        :items="inbox"
        @copy="emit('copyContent', $event)"
        @remove="emit('deleteDrop', $event)"
        @clear="emit('clearInbox')"
      />

      <!-- 实时传输审计日志 -->
      <FileShareLogsTab
        v-else
        id="fs-panel-logs"
        role="tabpanel"
        aria-labelledby="fs-tab-logs"
        :logs="logs"
      />
    </div>
  </section>
</template>

<style scoped>
/* 样式自视图拆分迁入；§9.6 在册的两代级联已按生效终值并为单源，
   被后代压死的 gen1 档（tab-nav 下边线/8px 内距、蓝色实底 .active）已除。 */
.tab-nav {
  display: flex;
  gap: 4px;
  max-width: 100%;
  padding: 4px;
  overflow-x: auto;
  background: var(--surface-soft);
  border: 0;
  border-radius: var(--radius-control);
}

.tab-item {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 auto;
  padding: 7px 11px;
  white-space: nowrap;
  border-radius: var(--radius-chip);
  background: none;
  border: none;
  font-size: var(--text-base);
  font-weight: 500;
  color: var(--color-text-muted);
  cursor: pointer;
  transition: transform var(--motion-base) ease, border-color var(--motion-base) ease, background var(--motion-base) ease, box-shadow var(--motion-base) ease;
}

.tab-item:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.tab-item.active {
  color: var(--color-primary-hover);
  background: var(--surface-panel);
  box-shadow: var(--shadow-small);
}

.tab-item:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 2px;
}

.section-kicker {
  margin-bottom: 7px;
  color: var(--color-primary);
  font-size: var(--text-micro);
  font-weight: 800;
  letter-spacing: 0.16em;
}

.workspace-card {
  padding: 0;
  overflow: hidden;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-panel);
  box-shadow: var(--shadow-panel);
}

.workspace-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  padding: 20px 22px;
  border-bottom: 1px solid var(--color-border);
}

/* 区块标题：18px 终值 token 化为 --text-xl */
.section-title {
  margin: 0;
  color: var(--color-text);
  font-size: var(--text-xl);
  font-weight: 750;
}

.tab-count {
  min-width: 18px;
  padding: 1px 5px;
  color: inherit;
  font-size: var(--text-micro);
  text-align: center;
  background: var(--surface-hover);
  border-radius: var(--radius-pill);
}

.workspace-body {
  min-height: 230px;
  padding: 18px;
  background: var(--surface-soft);
}

@media (max-width: 760px) {
  .workspace-header {
    align-items: stretch;
    flex-direction: column;
    gap: 14px;
  }

  .tab-nav {
    width: 100%;
  }
}
</style>
