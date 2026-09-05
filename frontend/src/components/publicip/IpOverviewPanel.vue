<script setup lang="ts">
import type { NetworkOverview } from '../../../bindings/hanxi/internal/modules/publicip/models'
import type { Adapter } from '../../../bindings/hanxi/internal/platform/models'
import AdapterCard from './AdapterCard.vue'

// 「IP 与网卡查看」面板：状态栏 + 公网出口 IP 双卡 + 网卡详情列表（纯展示）。
// 概览数据、加载态与错误文案由视图经 props 下传；强制刷新与快捷 Ping / 复制上抛。
// 根节点为多根 fragment（状态栏/错误框/两个小节），渲染后仍是 .network-page 的直接子节点，
// 故 flex gap 布局与拆分前逐字一致。
defineProps<{
  overview: NetworkOverview | null
  loading: boolean
  error: string
  adapters: Adapter[]
}>()

const emit = defineEmits<{
  refresh: []
  'quick-ping': [ip: string]
  'copy-text': [text: string, label: string]
}>()

// 网卡卡的快捷动作原样透传给视图编排层（跨 Tab 跳转与 toast 口径不在本层决定）。
function forwardQuickPing(ip: string) {
  emit('quick-ping', ip)
}

function forwardCopyText(text: string, label: string) {
  emit('copy-text', text, label)
}
</script>

<template>
  <!-- 顶部状态栏 -->
  <div class="control-panel">
    <div class="meta-info">
      <span v-if="overview?.fetchedAt">最后探测时间: {{ overview.fetchedAt }}</span>
      <span v-else>等待探测…</span>
    </div>
    <button class="btn btn-primary" :disabled="loading" @click="emit('refresh')">
      {{ loading ? '探测中…' : '强制刷新' }}
    </button>
  </div>

  <div v-if="error" class="error-box">{{ error }}</div>

  <!-- 1. 公网出口 IP 卡片区域 -->
  <div class="section-title">
    <h3>出口公网 IP</h3>
  </div>
  <div class="cards-grid">
    <!-- 公网 IPv4 卡片 -->
    <div class="ip-card">
      <div class="card-header">
        <div class="tag-row">
          <span class="card-tag v4">公网 IPv4</span>
          <span class="chip chip-neutral" v-if="overview?.publicIpv4">家宽 / 专线</span>
        </div>
        <span class="provider-label" v-if="overview?.sourceV4">来源: {{ overview.sourceV4 }}</span>
      </div>
      <div class="card-body">
        <div v-if="loading && !overview" class="ip-placeholder">正在探测公网 IPv4…</div>
        <div v-else-if="overview?.publicIpv4" class="ip-value">
          <code>{{ overview.publicIpv4 }}</code>
        </div>
        <div v-else class="ip-empty">未获取到公网 IPv4</div>
      </div>
      <div class="card-footer">
        <button
          class="btn-copy"
          :disabled="!overview?.publicIpv4"
          @click="emit('quick-ping', overview?.publicIpv4 ?? '')"
        >
          ⚡ Ping 测试
        </button>
        <button
          class="btn-copy"
          :disabled="!overview?.publicIpv4"
          @click="emit('copy-text', overview?.publicIpv4 ?? '', '公网 IPv4')"
        >
          复制 IPv4
        </button>
      </div>
    </div>

    <!-- 公网 IPv6 卡片 -->
    <div class="ip-card">
      <div class="card-header">
        <div class="tag-row">
          <span class="card-tag v6">公网 IPv6</span>
          <span class="chip chip-positive" v-if="overview?.publicIpv6">已开通 IPv6</span>
        </div>
        <span class="provider-label" v-if="overview?.sourceV6">来源: {{ overview.sourceV6 }}</span>
      </div>
      <div class="card-body">
        <div v-if="loading && !overview" class="ip-placeholder">正在探测公网 IPv6…</div>
        <div v-else-if="overview?.publicIpv6" class="ip-value">
          <code>{{ overview.publicIpv6 }}</code>
        </div>
        <div v-else class="ip-empty">未检测到公网 IPv6（当前网络可能未开通或被防火墙拦截）</div>
      </div>
      <div class="card-footer">
        <button
          class="btn-copy"
          :disabled="!overview?.publicIpv6"
          @click="emit('copy-text', overview?.publicIpv6 ?? '', '公网 IPv6')"
        >
          复制 IPv6
        </button>
      </div>
    </div>
  </div>

  <!-- 2. 本机网络适配器详情卡片列表 -->
  <div class="section-title">
    <h3>网卡与局域网配置</h3>
  </div>

  <div class="adapters-list">
    <AdapterCard
      v-for="adapter in adapters"
      :key="adapter.name"
      :adapter="adapter"
      @quick-ping="forwardQuickPing"
      @copy-text="forwardCopyText"
    />

    <div v-if="adapters.length === 0 && !loading" class="empty-hint">
      未检测到活跃网卡网络信息
    </div>
  </div>
</template>

<style scoped>
/* 页头/错误框/.btn 家族/.chip 徽标由 PageHeader 与 components.css 原子接管；
   以下状态栏、公网 IP 卡与网卡列表容器为本视图独有布局，拆分时随标记整体迁入。
   （协议标签 .card-tag 为方角小标签、非胶囊 chip 形，按手册§9-4 不硬塞全局） */
.control-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  padding: 12px 16px;
  border-radius: 8px;
}

.meta-info {
  font-size: 13px;
  color: var(--color-text-muted);
}

.section-title {
  margin-top: 6px;
}

.section-title h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin: 0;
}

.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(380px, 1fr));
  gap: 16px;
}

.ip-card {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.tag-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.card-tag {
  font-size: 12px;
  font-weight: 600;
  padding: 3px 8px;
  border-radius: 4px;
}
.card-tag.v4 {
  background: var(--state-information-soft);
  color: var(--state-information);
}
.card-tag.v6 {
  background: var(--state-warning-soft);
  color: var(--state-warning);
}

.provider-label {
  font-size: 11px;
  color: var(--color-text-subtle);
}

.card-body {
  padding: 8px 0;
  min-height: 48px;
  display: flex;
  align-items: center;
}

.ip-value code {
  font-family: var(--font-mono);
  font-size: 18px;
  font-weight: 700;
  color: var(--color-text);
  background: var(--surface-hover);
  padding: 4px 10px;
  border-radius: 6px;
  word-break: break-all;
}

.ip-placeholder, .ip-empty {
  font-size: 13px;
  color: var(--color-text-subtle);
}

.card-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  border-top: 1px solid var(--color-border);
  padding-top: 8px;
}

.btn-copy {
  padding: 4px 12px;
  border: 1px solid var(--color-border);
  border-radius: 5px;
  background: var(--surface-panel);
  cursor: pointer;
  font-size: 12px;
  transition: all var(--motion-base) ease;
}
.btn-copy:hover:not(:disabled) {
  border-color: var(--color-primary);
  color: var(--color-primary);
}
.btn-copy:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* 适配器卡片列表（卡片本体样式见 AdapterCard.vue） */
.adapters-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.empty-hint {
  text-align: center;
  padding: 32px;
  color: var(--color-text-subtle);
  font-size: 13px;
}
</style>
